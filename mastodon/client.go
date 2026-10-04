package main

// 出站：REST + Bearer token。比 bluesky 简单——token 是长期的，不用管会话。
//
// 两件事值得单列：
//   - **幂等键**：发布带 Idempotency-Key（一小时内同键只落一条）。工作流会重试，
//     不带的话一次超时重试就是时间线上两条一样的嘟文。
//   - **字数上限问实例**：Mastodon 是联邦网络，500 只是默认值。问一次缓存住。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const defaultMaxChars = 500

// —— 客户端 ——

var (
	clientMu sync.Mutex
	clients  = map[string]*http.Client{}
)

func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientMu.Lock()
	defer clientMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 120 * time.Second} // 传视频要久一点
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

func credOf(ctx plugin.Ctx) Cred { return sokel.CredentialAs[Cred](ctx) }

func baseOf(c Cred) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(c.InstanceURL), "/")
	if u == "" {
		return "", fmt.Errorf("凭证里没填实例地址（如 https://mastodon.social）")
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return "", fmt.Errorf("凭证里没填访问令牌（实例的「偏好设置 → 开发 → 新建应用」里生成）")
	}
	return u, nil
}

// —— 错误 ——

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("实例拒绝了这个令牌（401 %s）：可能已被撤销，去「偏好设置 → 开发」重新生成", e.Message)
	case http.StatusForbidden:
		return fmt.Sprintf("实例拒绝了这个操作（403 %s）：多半是应用的作用域不够——"+
			"发嘟要 write:statuses，传图要 write:media（改作用域后要**重新生成令牌**）", e.Message)
	case http.StatusUnprocessableEntity:
		return fmt.Sprintf("实例说这条不合法（422 %s）：常见是超出字数上限、投票选项不合规、或媒体与投票同时给了", e.Message)
	case http.StatusTooManyRequests:
		return "实例限流（429）：Mastodon 各实例自己配额度，降低发布频率"
	}
	if e.Message != "" {
		return fmt.Sprintf("实例报错 %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("实例返回 HTTP %d", e.Status)
}

// —— 请求 ——

type reqOpts struct {
	method string
	path   string // /api/v1/statuses
	query  url.Values
	form   url.Values // 表单体（Mastodon 的写接口收表单，数组用 key[] 重复）
	multi  *multipartBody
	idemp  string // Idempotency-Key
}

type multipartBody struct {
	body  []byte
	ctype string
}

func call(ctx plugin.Ctx, o reqOpts, out any) error {
	cred := credOf(ctx)
	base, err := baseOf(cred)
	if err != nil {
		return err
	}
	uri := base + o.path
	if len(o.query) > 0 {
		uri += "?" + o.query.Encode()
	}
	var body io.Reader
	ctype := ""
	switch {
	case o.multi != nil:
		body, ctype = bytes.NewReader(o.multi.body), o.multi.ctype
	case o.form != nil:
		body, ctype = strings.NewReader(o.form.Encode()), "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, o.method, uri, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cred.AccessToken))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if o.idemp != "" {
		req.Header.Set("Idempotency-Key", o.idemp)
	}
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("连接实例失败（%s；部署环境可能要在凭证里配出站代理）: %w", base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error
		if e.Description != "" {
			msg += "（" + e.Description + "）"
		}
		return &apiError{Status: resp.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("实例应答无法解析: %w", err)
	}
	return nil
}

// —— 实例信息（字数上限）——

var (
	instMu    sync.Mutex
	instChars = map[string]int{} // 实例地址 → 字数上限
)

// maxChars：问实例要字数上限（缓存住）。
//
// **不能写死 500**：mastodon.social 是 500，中文圈不少实例是 5000，有的到 11000。
// 写死的话，一条本可以发的长文会被插件自己拦下来，而用户在网页上明明发得出去。
// 问不到就退回 500——那是官方默认值，宁可保守。
func maxChars(ctx plugin.Ctx) int {
	cred := credOf(ctx)
	base, err := baseOf(cred)
	if err != nil {
		return defaultMaxChars
	}
	instMu.Lock()
	n, ok := instChars[base]
	instMu.Unlock()
	if ok {
		return n
	}
	var v2 struct {
		Configuration struct {
			Statuses struct {
				MaxCharacters int `json:"max_characters"`
			} `json:"statuses"`
		} `json:"configuration"`
	}
	n = defaultMaxChars
	if err := call(ctx, reqOpts{method: http.MethodGet, path: "/api/v2/instance"}, &v2); err == nil &&
		v2.Configuration.Statuses.MaxCharacters > 0 {
		n = v2.Configuration.Statuses.MaxCharacters
	}
	instMu.Lock()
	instChars[base] = n
	instMu.Unlock()
	return n
}

// —— 幂等键 ——

// idempotencyKey：同一条内容重发时要是**同一个键**，否则幂等等于没做。
// 取「正文 + 回复目标 + 可见性」的摘要：工作流重跑同一个节点，这三样不会变。
func idempotencyKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// —— 小工具 ——

// statusID：用户可能粘的是嘟文链接（https://实例/@某人/1234567890）。
func statusID(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.LastIndex(s, "/"); i >= 0 && strings.Contains(s, "://") {
		return s[i+1:]
	}
	return s
}

func newMultipart(field, name, mime string, data []byte, extra map[string]string) (*multipartBody, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range extra {
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, name)}
	h["Content-Type"] = []string{mime}
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return &multipartBody{body: buf.Bytes(), ctype: w.FormDataContentType()}, nil
}

var _ = context.Background
