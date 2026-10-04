package main

// Outbound requests pass through four gates: auth, proxy, rate limiting, error translation.
//
// The first two come from the credential; the other two are Notion facts of life: **roughly 3
// requests/sec per connection** (go over and you get a 429 with Retry-After), plus a set of errors
// that give you a code but no human-readable explanation. object_not_found in particular is almost
// never "the id is wrong" — it's "this page hasn't been shared with the integration", and Notion's
// own message never says so.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	notionAPI = "https://api.notion.com/v1"
	// notionVersion: the API version. **Pinned** — Notion shapes its response based on this header;
	// without it, it falls back to the oldest version, where neither data sources (2025-09-03) nor
	// the markdown endpoint (2026-03-11) exist.
	notionVersion = "2026-03-11"
	// rateInterval: the minimum gap between two requests. Notion allows roughly 3 requests/sec per
	// connection; 340ms sits slightly under that ceiling — hitting 429 and backing off would also
	// work, but it turns an auto-paginating query into a jagged, stop-start mess.
	rateInterval = 340 * time.Millisecond
	maxRetries   = 3
)

// —— Credentials ——

// Cred is in zz_credential.go (generated from the schema declaration).

// authToken prefers the internal integration secret, falling back to the access_token injected by
// OAuth authorization.
//
// Both are the same kind of thing (the credential that determines "which pages can this integration
// see"), so they aren't split into two separate credential fields; if both are empty nothing is
// configured, and the error spells out both paths.
func authToken(cred Cred) (string, error) {
	if t := strings.TrimSpace(cred.Token); t != "" {
		return t, nil
	}
	if t := strings.TrimSpace(cred.AccessToken); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("凭证里既没有内部集成密钥，也没有授权令牌：" +
		"要么填 Notion 集成设置页的 Internal Integration Secret（ntn_ 开头），要么点凭证行的「授权」")
}

// —— HTTP client (cached per proxy) ——

var (
	clientMu sync.Mutex
	clients  = map[string]*http.Client{}
)

// clientFor caches an HTTP client per proxy address.
//
// The proxy is configured per credential rather than via a process-wide HTTP_PROXY: the latter is
// global, so routing all outbound traffic for one plugin through it would also drag internal-network
// calls through the proxy, and it can't be scoped per workspace (same reasoning as the search plugin).
func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 60 * time.Second}
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

// —— Rate limiting (per token) ——

type limiter struct {
	mu   sync.Mutex
	next time.Time
}

var (
	limMu sync.Mutex
	lims  = map[string]*limiter{}
)

// limiterFor: one gate per token. Notion's rate limit is counted **per connection**; when one
// process serves multiple credentials each counts separately, and sharing a single gate would just
// slow everyone down together.
func limiterFor(token string) *limiter {
	limMu.Lock()
	defer limMu.Unlock()
	if l, ok := lims[token]; ok {
		return l
	}
	l := &limiter{}
	lims[token] = l
	return l
}

// wait blocks until it's this call's turn.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	l.next = l.next.Add(rateInterval)
	l.mu.Unlock()

	d := time.Until(at)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// —— Errors ——

// apiError is Notion's error response.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	switch e.Code {
	// These two are two names for the same thing, and **it's almost never a wrong id**:
	// a Notion integration can't see anything by default — the page has to be shared with it via
	// the top-right "... -> Connections -> Add". The raw message ("Could not find page with ID
	// ...") never mentions this, so everyone's first instinct is to suspect a typo in the id.
	case "object_not_found", "restricted_resource":
		return fmt.Sprintf("Notion 找不到或没权限访问该对象：%s\n"+
			"多半是这一页/这个库还没交给集成——在 Notion 里打开它 → 右上角「⋯」→「连接」→ 添加你的集成（子页面会自动继承）", e.Message)
	case "unauthorized":
		return fmt.Sprintf("Notion 拒绝了这个令牌（%s）：密钥可能被撤销或粘贴不全，重新复制一次 Internal Integration Secret，或重新走一次授权", e.Message)
	case "validation_error":
		return fmt.Sprintf("Notion 说请求不合法：%s", e.Message)
	}
	if e.Code == "" {
		return fmt.Sprintf("Notion 返回 HTTP %d", e.Status)
	}
	return fmt.Sprintf("Notion 报错 %s: %s", e.Code, e.Message)
}

// —— Requests ——

type reqOpts struct {
	method string
	path   string // /pages/xxx, without the /v1 prefix
	query  url.Values
	body   any
}

// callAPI sends one request and decodes the response into out.
func callAPI(ctx plugin.Ctx, o reqOpts, out any) error {
	cred := sokel.CredentialAs[Cred](ctx)
	tok, err := authToken(cred)
	if err != nil {
		return err
	}
	return callAPIWith(ctx, clientFor(cred.Proxy), tok, o, out)
}

func callAPIWith(ctx context.Context, hc *http.Client, tok string, o reqOpts, out any) error {
	uri := notionAPI + o.path
	if len(o.query) > 0 {
		uri += "?" + o.query.Encode()
	}
	var payload []byte
	if o.body != nil {
		b, err := json.Marshal(o.body)
		if err != nil {
			return fmt.Errorf("请求体序列化失败: %w", err)
		}
		payload = b
	}
	return doWithRetry(ctx, hc, tok, o.method, uri, payload, out)
}

func doWithRetry(ctx context.Context, hc *http.Client, tok, method, uri string, payload []byte, out any) error {
	lim := limiterFor(tok)
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := lim.wait(ctx); err != nil {
			return err
		}
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, uri, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Notion-Version", notionVersion)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := hc.Do(req)
		if err != nil {
			// Network-level failure: Notion is hosted abroad, so a deployment with no proxy
			// configured hits exactly this; spell it out so it isn't mistaken for a wrong id.
			lastErr = fmt.Errorf("连接 Notion 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
			break
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()

		// 429 = rate limited, 529 = upstream overloaded. Both are handled the same way: wait
		// for Retry-After, then try again.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
			lastErr = &apiError{Status: resp.StatusCode, Code: "rate_limited",
				Message: fmt.Sprintf("触发限流，已重试 %d 次", attempt+1)}
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			}
			t.Stop()
			continue
		}
		if resp.StatusCode >= 400 {
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			return &apiError{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("Notion 应答无法解析: %w", err)
		}
		return nil
	}
	return lastErr
}

// retryAfter reads the Retry-After seconds; falls back to exponential backoff by attempt if absent.
func retryAfter(h string, attempt int) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

// —— Pagination ——

// listResponse is the common envelope for all of Notion's list endpoints.
type listResponse struct {
	Results    []json.RawMessage `json:"results"`
	HasMore    bool              `json:"has_more"`
	NextCursor string            `json:"next_cursor"`
}

// paginate pages through results until max items are collected. Returns the raw items, whether more
// remain, and the next cursor.
//
// 100 per page is Notion's ceiling; the caller's max decides how many pages to fetch. **It never
// paginates without bound**: a database with tens of thousands of rows would stall the workflow and
// burn through the rate limit.
func paginate(ctx plugin.Ctx, o reqOpts, max int, cursor string) ([]json.RawMessage, bool, string, error) {
	if max <= 0 {
		max = 100
	}
	var all []json.RawMessage
	for {
		size := max - len(all)
		if size > 100 {
			size = 100
		}
		page := o
		switch o.method {
		case http.MethodPost:
			b := map[string]any{}
			if o.body != nil {
				bs, _ := json.Marshal(o.body)
				_ = json.Unmarshal(bs, &b)
			}
			b["page_size"] = size
			if cursor != "" {
				b["start_cursor"] = cursor
			}
			page.body = b
		default:
			q := url.Values{}
			for k, v := range o.query {
				q[k] = v
			}
			q.Set("page_size", strconv.Itoa(size))
			if cursor != "" {
				q.Set("start_cursor", cursor)
			}
			page.query = q
		}
		var lr listResponse
		if err := callAPI(ctx, page, &lr); err != nil {
			return nil, false, "", err
		}
		all = append(all, lr.Results...)
		cursor = lr.NextCursor
		if !lr.HasMore || cursor == "" || len(all) >= max {
			return all, lr.HasMore, cursor, nil
		}
	}
}

// —— IDs ——

var hex32 = regexp.MustCompile(`[0-9a-fA-F]{32}`)

// notionID extracts a 32-char id from an "id / hyphenated uuid / full Notion URL" input.
//
// The platform has no "remote dropdown, pick a database" control, so every id has to be pasted in.
// And what a person has on hand is always the browser address-bar link, never the bare id — if the
// field doesn't accept links, everyone has to first learn that "the id is the string at the end of
// the link with the hyphens stripped, but not the part after ?v=".
//
// Looking only at the path and ignoring the query is the key bit: a database link's `?v=<32 chars>`
// is the **view id**, and querying with it gets you an object_not_found with no clue why.
func notionID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Already an id: a bare 32-char hex string, or a hyphenated uuid
	if m := hex32.FindString(strings.ReplaceAll(s, "-", "")); m != "" && len(strings.ReplaceAll(s, "-", "")) == 32 {
		return m
	}
	path := s
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		path = u.Path // Drop the query: ?v= is the view id, not the database id
	} else if i := strings.IndexAny(s, "?#"); i >= 0 {
		path = s[:i]
	}
	all := hex32.FindAllString(strings.ReplaceAll(path, "-", ""), -1)
	if len(all) == 0 {
		return strings.TrimSpace(s) // Unrecognized — hand it to Notion as-is and let its error speak
	}
	return all[len(all)-1]
}

// requireID fetches a required id value (also unwraps links).
func requireID(field, v string) (string, error) {
	id := notionID(v)
	if id == "" {
		return "", fmt.Errorf("缺少 %s", field)
	}
	return id, nil
}
