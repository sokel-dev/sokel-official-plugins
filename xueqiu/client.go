package main

// Egress. All three endpoints are private ones the web frontend calls itself (obtained by
// packet capture, undocumented):
//
//	POST /statuses/update.json      post (form; needs session_token)
//	POST /photo/upload.json         upload image (multipart, field name "file")
//	GET  /etc/private_fund/state.json  login-state probe (no side effects, used as health_check)
//
// **Undocumented means the response shape is guesswork**, so parsing always goes through
// "loose extraction": recursively searching the JSON for what's wanted instead of pinning
// down a specific key name. Pinning it down would turn one Xueqiu field rename into a
// blanket failure, with the error only saying "parse failed".

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

// base / mpBase: **vars, not consts** — tests need to point them at a fake upstream.
//
// The two subdomains are two separate APIs: xueqiu.com posts **short posts** (needs
// session_token), mp.xueqiu.com posts **long articles** (no session_token needed, and no
// risk-control param observed either). Same cookie for both.
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

// —— request ——

type reqOpts struct {
	method string
	path   string
	// host: empty means base (xueqiu.com). The long-article API lives on mp.xueqiu.com —
	// same cookie, different subdomain and endpoints (see article.go).
	host    string
	form    url.Values     // form body
	multi   *multipartBody // file body
	referer string
}

type multipartBody struct {
	body  []byte
	ctype string
}

// do: sends one request. **Four headers are key to Xueqiu recognizing a real client**:
// Cookie, UA, X-Requested-With, Referer/Origin. Missing any one of them risks being
// blocked by the WAF as a crawler, which responds with an error page unrelated to login state.
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
	// Risk-control param: attached to the URL on web requests. Leave it off by default —
	// most of the time it goes through fine; if it actually gets blocked, the error
	// message tells the user to paste one in (see translate).
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

// translate: turns a Xueqiu failure into a human-readable message.
//
// Its failures come in three different-looking shapes: an HTTP-level 403/401, an
// error_code/error_description inside a JSON envelope, and **a full HTML page returned
// when the WAF blocks the request**. The third is the nastiest — parsing it as JSON would
// just say "parse failed", while the real cause is risk control.
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
// The posting endpoint needs it, and it isn't in the cookie (uploading images and the
// login-state probe don't need it). Prefers the one manually pasted into the credential;
// otherwise it's scraped from the homepage once — and if that fails, **tell the user
// exactly where to copy it from** rather than sending a request that's bound to be rejected.

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

// —— loose extraction ——
//
// The response shape is undocumented, so pinning down a key name would turn one Xueqiu
// field rename into a blanket parse failure. These two functions search for what's wanted
// at any depth in the JSON.

// findString: recursively finds the first string value that satisfies want.
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

// findID: looks up an id by key name (accepts both number and string — Xueqiu's ids are large integers that JSON decodes as float64).
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

// —— small helpers ——

// uidFromCookie: reads uid from the payload of xq_id_token (a JWT).
// Only used to build the post link — if it can't be read, the link is simply omitted;
// **this must not fail the publish**.
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

// readAllLimited: for reading a page, with a cap so a huge response can't blow things up.
func readAllLimited(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 4<<20))
}
