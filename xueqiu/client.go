package main

// 出站。三个接口都是网页端自己在调的私有接口（抓包得来，无文档）：
//
//	POST /statuses/update.json      发帖（表单；要 session_token）
//	POST /photo/upload.json         传图（multipart，字段名 file）
//	GET  /etc/private_fund/state.json  登录态探测（无副作用，用作 health_check）
//
// **无文档意味着应答形状是猜的**，所以解析一律走「宽松提取」：在 JSON 里递归找想要的东西，
// 而不是钉死某个键名。钉死的话，雪球改一次字段名就全线报错，而错误信息只会说「解析失败」。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// base / mpBase：**是 var 不是 const**——测试要把它们指到假上游上。
//
// 两个子站是两套接口：xueqiu.com 发**短帖**（要 session_token），
// mp.xueqiu.com 发**长文**（不要 session_token，也没见风控参数）。同一份 cookie。
var (
	base   = "https://xueqiu.com"
	mpBase = "https://mp.xueqiu.com"
)

const defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

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

func credOf(ctx plugin.Ctx) Cred { return sokel.CredentialAs[Cred](ctx) }

func cookieOf(c Cred) (string, error) {
	ck := strings.TrimSpace(c.Cookie)
	if ck == "" {
		return "", fmt.Errorf("凭证里没有 Cookie：登录雪球后从浏览器复制整条 Cookie 粘进来")
	}
	if !strings.Contains(ck, "xq_a_token") {
		return "", fmt.Errorf("这条 Cookie 里没有 xq_a_token，多半复制得不全（要整条，不是某一个键）")
	}
	return ck, nil
}

// —— 请求 ——

type reqOpts struct {
	method string
	path   string
	// host：留空用 base（xueqiu.com）。长文那套在 mp.xueqiu.com 上——
	// 同一份 cookie，不同的子站与接口（见 article.go）。
	host    string
	form    url.Values     // 表单体
	multi   *multipartBody // 文件体
	referer string
}

type multipartBody struct {
	body  []byte
	ctype string
}

// do：发一次请求。**四个头是雪球认人的关键**：Cookie、UA、X-Requested-With、Referer/Origin。
// 少任何一个都可能被 WAF 当成爬虫拦掉，而它回的是一个和登录态无关的错误页。
func do(ctx plugin.Ctx, o reqOpts, out any) error {
	cred := credOf(ctx)
	ck, err := cookieOf(cred)
	if err != nil {
		return err
	}
	host := o.host
	if host == "" {
		host = base
	}
	uri := host + o.path
	// 风控参数：网页请求 URL 上挂着它。留空先不带——多数情况下能过；
	// 真被拦了，错误信息会让用户去粘一条（见 translate）。
	if p := strings.TrimSpace(cred.RiskParam); p != "" {
		uri += "?md5__1038=" + url.QueryEscape(p)
	}
	var body io.Reader
	ctype := ""
	switch {
	case o.multi != nil:
		body, ctype = bytes.NewReader(o.multi.body), o.multi.ctype
	case o.form != nil:
		body, ctype = strings.NewReader(o.form.Encode()), "application/x-www-form-urlencoded; charset=UTF-8"
	}
	req, err := http.NewRequestWithContext(ctx, o.method, uri, body)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", ck)
	req.Header.Set("User-Agent", uaOf(cred))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "*/*")
	ref := o.referer
	if ref == "" {
		ref = host + "/"
	}
	req.Header.Set("Referer", ref)
	req.Header.Set("Origin", host)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("连接雪球失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err := translate(resp.StatusCode, raw); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("雪球应答无法解析（前 200 字：%s）: %w", clip(raw, 200), err)
	}
	return nil
}

func uaOf(c Cred) string {
	if ua := strings.TrimSpace(c.UserAgent); ua != "" {
		return ua
	}
	return defaultUA
}

// translate：把雪球的失败翻译成人话。
//
// 它的失败有三种，长得都不一样：HTTP 层的 403/401、JSON 信封里的 error_code/error_description、
// 以及**被 WAF 拦下时回的一整页 HTML**。第三种最坑——按 JSON 解会得到「解析失败」，
// 而真正的原因是风控。
func translate(status int, raw []byte) error {
	body := strings.TrimSpace(string(raw))
	if strings.HasPrefix(body, "<") {
		return fmt.Errorf("雪球没回 JSON 而是一页 HTML（HTTP %d）——多半是被风控拦了："+
			"把浏览器请求 URL 上的 md5__1038 值粘进凭证的「风控参数」再试；也可能是 Cookie 已失效", status)
	}
	var env struct {
		ErrorCode  any    `json:"error_code"`
		ErrorDesc  string `json:"error_description"`
		ErrorMsg   string `json:"error_message"`
		Success    any    `json:"success"`
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
	}
	_ = json.Unmarshal(raw, &env)
	msg := firstNonEmpty(env.ErrorDesc, env.ErrorMsg, env.Message)
	code := fmt.Sprint(env.ErrorCode)

	if status == http.StatusUnauthorized || status == http.StatusForbidden ||
		code == "400016" || strings.Contains(msg, "登录") || strings.Contains(msg, "身份") {
		return fmt.Errorf("雪球说没登录（HTTP %d %s）：Cookie 过期了——"+
			"重新登录一次并更新凭证（凭证页「检查凭证」能确认）", status, msg)
	}
	if msg != "" && (code != "" && code != "0" && code != "<nil>") {
		return fmt.Errorf("雪球拒绝了这次请求（%s）：%s", code, msg)
	}
	if status >= 400 {
		return fmt.Errorf("雪球返回 HTTP %d：%s", status, clip(raw, 200))
	}
	if msg != "" {
		return fmt.Errorf("雪球拒绝了这次请求：%s", msg)
	}
	return nil
}

// —— session_token ——
//
// 发帖接口要它，而它不在 cookie 里（传图与登录态探测都不需要）。
// 优先用凭证里手工粘的；没有就去首页抓一次——抓不到时**明确告诉用户去哪儿复制**，
// 而不是发一个必然被拒的请求。

var tokenRe = regexp.MustCompile(`session_token["'\s:=]{1,6}["']([A-Za-z0-9_\-]{8,64})["']`)

var (
	tokMu    sync.Mutex
	tokCache = map[string]string{} // cookie → session_token
)

func sessionToken(ctx plugin.Ctx) (string, error) {
	cred := credOf(ctx)
	if t := strings.TrimSpace(cred.SessionToken); t != "" {
		return t, nil
	}
	ck, err := cookieOf(cred)
	if err != nil {
		return "", err
	}
	tokMu.Lock()
	t, ok := tokCache[ck]
	tokMu.Unlock()
	if ok {
		return t, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", ck)
	req.Header.Set("User-Agent", uaOf(cred))
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return "", fmt.Errorf("取 session_token 时连不上雪球: %w", err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if m := tokenRe.FindSubmatch(page); len(m) == 2 {
		t := string(m[1])
		tokMu.Lock()
		tokCache[ck] = t
		tokMu.Unlock()
		return t, nil
	}
	return "", fmt.Errorf("没能从页面上取到 session_token：请在浏览器里手动发一条帖子，" +
		"F12 → Network → update.json → 请求体里复制 session_token 的值，粘进凭证的同名字段")
}

// —— 宽松提取 ——
//
// 应答形状没有文档，钉死键名等于把「雪球改一次字段名」变成「全线解析失败」。
// 这两个函数在任意深度的 JSON 里找想要的东西。

// findString：递归找第一个满足 want 的字符串值。
func findString(v any, want func(string) bool) string {
	switch t := v.(type) {
	case string:
		if want(t) {
			return t
		}
	case []any:
		for _, e := range t {
			if s := findString(e, want); s != "" {
				return s
			}
		}
	case map[string]any:
		for _, e := range t {
			if s := findString(e, want); s != "" {
				return s
			}
		}
	}
	return ""
}

// findID：按键名找 id（数字或字符串都认——雪球的 id 是大整数，JSON 解出来是 float64）。
func findID(v any, keys ...string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range keys {
		switch t := m[k].(type) {
		case string:
			if t != "" {
				return t
			}
		case float64:
			return fmt.Sprintf("%.0f", t)
		}
	}
	for _, e := range m {
		if s := findID(e, keys...); s != "" {
			return s
		}
	}
	return ""
}

// —— 小工具 ——

// uidFromCookie：从 xq_id_token（JWT）的载荷里读 uid。
// 只为拼帖子链接用——读不到就不拼，**不因此让发布失败**。
func uidFromCookie(cookie string) string {
	i := strings.Index(cookie, "xq_id_token=")
	if i < 0 {
		return ""
	}
	tok := cookie[i+len("xq_id_token="):]
	if j := strings.IndexAny(tok, "; "); j > 0 {
		tok = tok[:j]
	}
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		UID any `json:"uid"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	switch t := claims.UID.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	}
	return ""
}

func newMultipart(field, filename, mime string, data []byte) (*multipartBody, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename)}
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

func clip(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// readAllLimited：读页面用，封个顶免得被一个巨大的响应撑爆。
func readAllLimited(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 4<<20))
}
