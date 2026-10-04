package main

// 出站：一条请求要过的四道关——认证、代理、限流、错误翻译。
//
// 认证只有 OAuth 注入的 access_token（平台代刷，插件不碰 refresh_token）。
// 后两道是 X 的实况：**限流按端点分桶**（搜索 15 分钟 300 次、发推 15 分钟 100 次…），
// 撞上回 429 并带 x-rate-limit-reset（一个绝对时间戳，不是秒数——按 Retry-After 那套读会等错时长）；
// 以及一套 detail 里才有人话的错误。

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

// xAPI：X 的 API 根地址。**是 var 不是 const**——测试要把它指到假上游上。
var xAPI = "https://api.x.com/2"

const (
	// maxRetries：429 与 5xx 的重试次数。X 的限流窗口是 15 分钟，等满一个窗口没有意义
	// （工作流早超时了），所以只等 waitCap 那么久，超过就把「什么时候恢复」写进错误里让人看见。
	maxRetries = 3
	waitCap    = 60 * time.Second
)

// —— 凭证 ——

// Cred 见 zz_credential.go（schema 声明生成）。

func accessToken(cred Cred) (string, error) {
	if t := strings.TrimSpace(cred.AccessToken); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("这条凭证还没授权：到凭证页点「授权」走一次 X 的同意页。" +
		"（X 的写操作必须是用户身份，没有「填个密钥就能用」的路）")
}

// —— HTTP 客户端（按代理缓存）——

var (
	clientMu sync.Mutex
	clients  = map[string]*http.Client{}
)

// clientFor：按代理地址缓存客户端。代理按凭证配而不是靠进程级 HTTP_PROXY——
// 后者是全局的，为一个插件让所有出站绕道，内网调用会跟着遭殃。
func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 120 * time.Second} // 视频分片上传要久一点
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

// —— 错误 ——

// apiError：X 的错误应答。X 有两套错误形状（RFC7807 的 title/detail，和老的 errors[].message），
// 两套都要认——只认一套的话，另一套过来时错误信息是空的。
type apiError struct {
	Status int
	Title  string
	Detail string
	Reset  time.Time // 限流恢复时刻（x-rate-limit-reset）
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("X 拒绝了这个令牌（401 %s）：授权可能已被撤销，到凭证页重新授权一次", e.Detail)
	case http.StatusForbidden:
		// 403 在 X 上几乎总是这三件事之一，而它的原文从不提哪一件。
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

// —— 请求 ——

type reqOpts struct {
	method string
	path   string // /tweets，不含 /2
	query  url.Values
	body   any
	// raw：非 JSON 的请求体（媒体分片上传用）。给了 raw 就不看 body。
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
			// X 在境外：国内部署不配代理就是这一类。说清楚，否则会被当成 id 写错。
			return fmt.Errorf("连接 X 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			e := parseError(resp, raw)
			lastErr = e
			wait := backoff(e, attempt)
			// 等不起就别硬等：把「几点恢复」写进错误里，比让工作流卡到超时有用得多。
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
			return nil // 204：删除类接口不回 body
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("X 应答无法解析: %w", err)
		}
		return nil
	}
	return lastErr
}

// parseError：把两套错误形状归一。
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
	// x-rate-limit-reset 是**绝对 epoch 秒**，不是「等几秒」。当成秒数用会等到明年。
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

// —— 读接口的公共参数 ——
//
// 字段参数**用 tweet.fields 还是 post.fields**：X 把文档里的名字改成了 post.fields，
// 但 tweet.fields 是 v2 从第一天起的名字、至今仍被接受。这里先发 tweet.fields，
// 万一某天 X 真把它下掉（应答是 400 且抱怨这个参数名），自动改用 post.fields 并记住——
// 否则那一天的表现是「所有读操作同时 400」，而没人会想到是参数改名。
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

// readQuery：读接口的公共查询参数（展开作者与媒体，否则拿回来的只有 author_id 和 media_key）。
func readQuery() url.Values {
	q := url.Values{}
	q.Set(fieldsParamName(), postFieldList)
	q.Set("expansions", "author_id,attachments.media_keys,referenced_tweets.id")
	q.Set("user.fields", userFieldList)
	q.Set("media.fields", mediaFieldSet)
	return q
}

// callRead：带字段参数名回退的读请求。
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
