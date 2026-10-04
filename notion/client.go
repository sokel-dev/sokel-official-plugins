package main

// 出站：一条请求要过的四道关——认证、代理、限流、错误翻译。
//
// 前两道靠凭证，后两道是 Notion 的实况：**每个连接平均 3 次/秒**（超了回 429 带 Retry-After），
// 以及一套只有 code 没有人话的错误。尤其 object_not_found——它十有八九不是「id 写错了」，
// 而是「这一页没 share 给这个集成」，而 Notion 的原文一个字都不提这件事。

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
	// notionVersion：API 版本。**钉死**——Notion 按这个头决定应答形状，不带的话它按最老的版本回，
	// 数据源（2025-09-03）与 markdown 接口（2026-03-11）全都不存在。
	notionVersion = "2026-03-11"
	// rateInterval：两次请求之间的最小间隔。Notion 是「每连接平均 3 次/秒」，
	// 取 340ms 略低于上限——撞 429 再退避是能跑，但一条自动翻页的查询会被拖成锯齿。
	rateInterval = 340 * time.Millisecond
	maxRetries   = 3
)

// —— 凭证 ——

// Cred 见 zz_credential.go（schema 声明生成）。

// authToken：内部集成密钥优先，其次是 OAuth 授权注入的 access_token。
//
// 两者是同一类东西（「这个集成能看到哪些页面」的凭据），所以不拆成两条凭证行；
// 都为空就是没配，错误里直接说清两条路。
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

// —— HTTP 客户端（按代理缓存）——

var (
	clientMu sync.Mutex
	clients  = map[string]*http.Client{}
)

// clientFor：按代理地址缓存客户端。
//
// 代理按凭证配而不是靠进程级 HTTP_PROXY：后者是全局的，为一个插件让所有出站绕道，
// 内网调用会跟着遭殃，也没法按工作空间区分（与搜索插件同一条判断）。
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

// —— 限流（按 token）——

type limiter struct {
	mu   sync.Mutex
	next time.Time
}

var (
	limMu sync.Mutex
	lims  = map[string]*limiter{}
)

// limiterFor：一个 token 一个闸。Notion 的限流是**按连接**算的，
// 同一进程服务多个凭证时各算各的，共用一个闸只会让所有人一起变慢。
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

// wait：排到自己那一格再走。
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

// —— 错误 ——

// apiError：Notion 的错误应答。
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	switch e.Code {
	// 这两个是同一件事的两种说法，而且**十有八九不是 id 写错了**：
	// Notion 的集成默认什么都看不到，要在页面右上角「⋯ → 连接 → 添加」把页面交给它。
	// 原文（"Could not find page with ID ..."）一个字都不提这件事，
	// 于是所有人第一次都以为是 id 抄错了。
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

// —— 请求 ——

type reqOpts struct {
	method string
	path   string // /pages/xxx，不含 /v1
	query  url.Values
	body   any
}

// callAPI：发一次请求并把应答解进 out。
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
			// 网络层失败：Notion 在境外，国内部署不配代理就是这一类，说清楚免得被当成 id 写错
			lastErr = fmt.Errorf("连接 Notion 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
			break
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()

		// 429 = 撞限流，529 = 上游过载。两者同样处理：按 Retry-After 等一等再来。
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

// retryAfter：Retry-After 秒数；没给就按次数退避。
func retryAfter(h string, attempt int) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

// —— 分页 ——

// listResponse：Notion 所有列表接口的共同外壳。
type listResponse struct {
	Results    []json.RawMessage `json:"results"`
	HasMore    bool              `json:"has_more"`
	NextCursor string            `json:"next_cursor"`
}

// paginate：翻到攒够 max 条为止。返回原始条目、是否还有更多、下一页游标。
//
// 每页 100 是 Notion 的上限；调用方给的 max 决定翻几页。**不自动无限翻**：
// 一个几万行的库会把工作流拖死，也会把限流吃光。
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

// —— ID ——

var hex32 = regexp.MustCompile(`[0-9a-fA-F]{32}`)

// notionID：从「id / 带横线的 uuid / 一条完整 Notion 链接」里取出 32 位 id。
//
// 平台没有「远程下拉选一个数据库」那种控件，所有 id 只能靠贴。而人手上有的
// 从来是浏览器地址栏里那条链接，不是 id——不认链接的话，每个字段都要人先学会
// 「id 是链接结尾那串没有横线的东西，但不要 ?v= 后面那串」。
//
// 只看 path 不看 query 是关键：数据库链接的 `?v=<32位>` 是**视图 id**，
// 拿它去查会得到一个 object_not_found，而错因完全看不出来。
func notionID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 已经是 id：32 位裸 hex，或带横线的 uuid
	if m := hex32.FindString(strings.ReplaceAll(s, "-", "")); m != "" && len(strings.ReplaceAll(s, "-", "")) == 32 {
		return m
	}
	path := s
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		path = u.Path // 丢掉 query：?v= 是视图 id，不是数据库 id
	} else if i := strings.IndexAny(s, "?#"); i >= 0 {
		path = s[:i]
	}
	all := hex32.FindAllString(strings.ReplaceAll(path, "-", ""), -1)
	if len(all) == 0 {
		return strings.TrimSpace(s) // 认不出来就原样交给 Notion，让它的错误去说话
	}
	return all[len(all)-1]
}

// requireID：必填 id 的取值（顺带抠链接）。
func requireID(field, v string) (string, error) {
	id := notionID(v)
	if id == "" {
		return "", fmt.Errorf("缺少 %s", field)
	}
	return id, nil
}
