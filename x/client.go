package main

// Outbound requests pass through four gates: auth, proxy, rate limiting, error translation.
//
// Auth is only the OAuth-injected access_token (the platform handles refreshing it; the plugin
// never touches refresh_token). The other two are X facts of life: **rate limits are bucketed per
// endpoint** (search: 300 per 15 minutes, post: 100 per 15 minutes, ...); hitting one returns a 429
// with x-rate-limit-reset (an absolute timestamp, not a number of seconds — reading it like a
// Retry-After header waits the wrong amount of time); plus a set of errors whose only human-readable
// part is the detail field.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// xAPI is X's API root. **It's a var, not a const** — tests need to point it at a fake upstream.
var xAPI = "https://api.x.com/2"

const (
	// maxRetries: how many times to retry a 429 or 5xx. X's rate-limit window is 15 minutes, and
	// waiting out a full window is pointless (the workflow would have timed out long before), so
	// this only waits up to waitCap, and beyond that surfaces "when it recovers" in the error for
	// a human to see.
	maxRetries = 3
	waitCap    = 60 * time.Second
)

// —— Credentials ——

// Cred is in zz_credential.go (generated from the schema declaration).

func accessToken(cred Cred) (string, error) {
	if t := strings.TrimSpace(cred.AccessToken); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("这条凭证还没授权：到凭证页点「授权」走一次 X 的同意页。" +
		"（X 的写操作必须是用户身份，没有「填个密钥就能用」的路）")
}

// —— HTTP client (cached per proxy) ——

var (
	clientMu sync.Mutex
	clients  = map[string]*http.Client{}
)

// clientFor caches an HTTP client per proxy address. The proxy is configured per credential rather
// than via a process-wide HTTP_PROXY — the latter is global, so routing all outbound traffic for
// one plugin through it would also drag internal-network calls through the proxy.
func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 120 * time.Second} // Chunked video upload needs a bit longer
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.Proxy = http.ProxyURL(u)
			c.Transport = tr
		}
	}
	clients[proxy] = c
	return c
}

// —— Errors ——

// apiError is X's error response. X has two error shapes (RFC7807's title/detail, and the older
// errors[].message) — both must be handled, or the message comes back empty whenever the other
// shape is used.
type apiError struct {
	Status int
	Title  string
	Detail string
	Reset  time.Time // When the rate limit recovers (x-rate-limit-reset)
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("X 拒绝了这个令牌（401 %s）：授权可能已被撤销，到凭证页重新授权一次", e.Detail)
	case http.StatusForbidden:
		// A 403 on X is almost always one of these three things, and its own message never says which.
		return fmt.Sprintf("X 拒绝了这个操作（403 %s）：常见原因是"+
			"① 开发者后台的 App permissions 不是 Read and write（改完必须**重新授权**，光改设置不生效）；"+
			"② 授权时少勾了作用域（如发私信要 dm.write）；"+
			"③ 这个操作要更高的 API 档位（引用转推、全量存档搜索是 Enterprise）", e.Detail)
	case http.StatusTooManyRequests:
		if !e.Reset.IsZero() {
			return fmt.Sprintf("X 限流（429）：该端点要到 %s 才恢复（还有 %s）",
				e.Reset.Format("15:04:05"), time.Until(e.Reset).Round(time.Second))
		}
		return "X 限流（429），请降低频率或拉长轮询间隔"
	case http.StatusPaymentRequired:
		return fmt.Sprintf("X 说这次调用没有额度（402 %s）：X API 从 2026-02 起按次计费，"+
			"到开发者后台充值或调低拉取量", e.Detail)
	}
	if e.Detail != "" {
		return fmt.Sprintf("X 报错 %d %s: %s", e.Status, e.Title, e.Detail)
	}
	return fmt.Sprintf("X 返回 HTTP %d", e.Status)
}

// —— Requests ——

type reqOpts struct {
	method string
	path   string // /tweets, without the /2 prefix
	query  url.Values
	body   any
	// raw: a non-JSON request body (used for chunked media upload). When raw is set, body is ignored.
	raw     []byte
	rawType string
}

func callAPI(ctx plugin.Ctx, o reqOpts, out any) error {
	cred := sokel.CredentialAs[Cred](ctx)
	tok, err := accessToken(cred)
	if err != nil {
		return err
	}
	return callAPIWith(ctx, clientFor(cred.Proxy), tok, o, out)
}

func callAPIWith(ctx context.Context, hc *http.Client, tok string, o reqOpts, out any) error {
	uri := xAPI + o.path
	if len(o.query) > 0 {
		uri += "?" + o.query.Encode()
	}
	payload, ctype := o.raw, o.rawType
	if payload == nil && o.body != nil {
		b, err := json.Marshal(o.body)
		if err != nil {
			return fmt.Errorf("请求体序列化失败: %w", err)
		}
		payload, ctype = b, "application/json"
	}
	return doWithRetry(ctx, hc, tok, o.method, uri, payload, ctype, out)
}

func doWithRetry(ctx context.Context, hc *http.Client, tok, method, uri string, payload []byte, ctype string, out any) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, uri, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := hc.Do(req)
		if err != nil {
			// X is hosted abroad: a deployment with no proxy configured hits exactly this. Spell
			// it out, otherwise it gets mistaken for a wrong id.
			return fmt.Errorf("连接 X 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			e := parseError(resp, raw)
			lastErr = e
			wait := backoff(e, attempt)
			// Don't force a wait that's too long: putting "recovers at what time" in the error is
			// far more useful than letting the workflow hang until it times out.
			if wait > waitCap {
				return e
			}
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return parseError(resp, raw)
		}
		if out == nil {
			return nil
		}
		if len(raw) == 0 {
			return nil // 204: delete-style endpoints return no body
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("X 应答无法解析: %w", err)
		}
		return nil
	}
	return lastErr
}

// parseError normalizes X's two error shapes into one.
func parseError(resp *http.Response, raw []byte) *apiError {
	e := &apiError{Status: resp.StatusCode}
	var body struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Errors []struct {
			Message string `json:"message"`
			Detail  string `json:"detail"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &body) == nil {
		e.Title, e.Detail = body.Title, body.Detail
		if e.Detail == "" && len(body.Errors) > 0 {
			e.Detail = body.Errors[0].Message
			if e.Detail == "" {
				e.Detail = body.Errors[0].Detail
			}
		}
	}
	// x-rate-limit-reset is an **absolute epoch-seconds timestamp**, not "wait this many seconds".
	// Treating it as a duration would wait until next year.
	if v := resp.Header.Get("x-rate-limit-reset"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil && ts > 0 {
			e.Reset = time.Unix(ts, 0)
		}
	}
	return e
}

func backoff(e *apiError, attempt int) time.Duration {
	if !e.Reset.IsZero() {
		if d := time.Until(e.Reset) + time.Second; d > 0 {
			return d
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

// —— Common parameters for read endpoints ——
//
// **tweet.fields vs. post.fields** for the fields parameter: X renamed it to post.fields in its
// docs, but tweet.fields is the name v2 has used since day one and is still accepted. This sends
// tweet.fields first, and if X ever actually removes it (the response is a 400 complaining about
// this parameter name), it automatically switches to post.fields and remembers the choice —
// otherwise the symptom that day would be "every read operation 400s at once", and nobody would
// think to suspect a renamed parameter.
var postFieldsParam atomic.Value // string

func fieldsParamName() string {
	if v, ok := postFieldsParam.Load().(string); ok && v != "" {
		return v
	}
	return "tweet.fields"
}

const (
	postFieldList = "id,text,created_at,author_id,conversation_id,public_metrics,referenced_tweets,entities,lang,attachments"
	userFieldList = "id,name,username,description,url,verified,protected,created_at,public_metrics"
	mediaFieldSet = "media_key,type,url,preview_image_url,alt_text"
)

// readQuery builds the common query parameters for read endpoints (expands author and media;
// otherwise all that comes back is author_id and media_key).
func readQuery() url.Values {
	q := url.Values{}
	q.Set(fieldsParamName(), postFieldList)
	q.Set("expansions", "author_id,attachments.media_keys,referenced_tweets.id")
	q.Set("user.fields", userFieldList)
	q.Set("media.fields", mediaFieldSet)
	return q
}

// callRead is a read request with fallback for the fields parameter name.
func callRead(ctx plugin.Ctx, o reqOpts, out any) error {
	err := callAPI(ctx, o, out)
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		return err
	}
	cur := fieldsParamName()
	if !strings.Contains(ae.Detail+ae.Title, cur) {
		return err
	}
	alt := "post.fields"
	if cur == "post.fields" {
		alt = "tweet.fields"
	}
	if o.query.Get(cur) == "" {
		return err
	}
	o.query.Set(alt, o.query.Get(cur))
	o.query.Del(cur)
	if err2 := callAPI(ctx, o, out); err2 == nil {
		log.Printf("x: %s 被 X 拒了，已改用 %s（X 改了参数名）", cur, alt)
		postFieldsParam.Store(alt)
		return nil
	}
	return err
}
