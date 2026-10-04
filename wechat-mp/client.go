package main

// Outbound calls and access_token.
//
// **access_token is globally unique**: refreshing it again for the same appid immediately
// invalidates the previous one. So:
//   - It's cached in-process by appid (valid for 7200 seconds, refreshed 5 minutes early);
//   - Running multiple replicas means they keep invalidating each other's token (symptom: random
//     40001s) — run a single replica, or have one central service issue the token. This is spelled
//     out explicitly in the usage doc.
//
// Error handling has two special cases:
//   - WeChat packs business errors inside an **HTTP 200** (errcode != 0). Checking only the status
//     code would treat "IP not in the allowlist" as success, and it would then fail in some
//     mysterious way at the next step.
//   - 40001/42001 mean "token invalid": clear the cache and retry once, without letting the caller
//     see it.

import (
	"bytes"
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

// apiBase is the WeChat API root address. **It's a var, not a const** — tests need to point it at
// a fake upstream.
var apiBase = "https://api.weixin.qq.com"

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

// —— token ——

type cachedToken struct {
	token string
	exp   time.Time
}

var (
	tokMu    sync.Mutex
	tokCache = map[string]cachedToken{} // appid → token
)

func accessToken(ctx plugin.Ctx, force bool) (string, error) {
	c := credOf(ctx)
	appID, secret := strings.TrimSpace(c.AppID), strings.TrimSpace(c.AppSecret)
	if appID == "" || secret == "" {
		return "", fmt.Errorf("凭证里缺 AppID 或 AppSecret（公众号后台「设置与开发 → 基本配置」）")
	}
	tokMu.Lock()
	if t, ok := tokCache[appID]; ok && !force && time.Now().Before(t.exp) {
		tokMu.Unlock()
		return t.token, nil
	}
	tokMu.Unlock()

	q := url.Values{"grant_type": {"client_credential"}, "appid": {appID}, "secret": {secret}}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := raw(ctx, http.MethodGet, "/cgi-bin/token?"+q.Encode(), nil, "", &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("微信没返回 access_token")
	}
	ttl := out.ExpiresIn
	if ttl <= 0 {
		ttl = 7200
	}
	tokMu.Lock()
	// Expires 5 minutes early: using it right up to the edge risks hitting "it just invalidated this
	// instant".
	tokCache[appID] = cachedToken{token: out.AccessToken, exp: time.Now().Add(time.Duration(ttl-300) * time.Second)}
	tokMu.Unlock()
	return out.AccessToken, nil
}

// —— Errors ——

type apiError struct {
	Code int
	Msg  string
}

func (e *apiError) Error() string {
	switch e.Code {
	case 40164, 40501:
		return fmt.Sprintf("调用服务器的 IP 不在白名单里（%d）：到公众号后台「设置与开发 → 基本配置 → IP 白名单」"+
			"把**本插件容器的公网出口 IP** 加进去。注意那多半不是你以为的那个 IP——"+
			"用 curl ifconfig.me 在容器里查一次最稳", e.Code)
	case 48001:
		return "该账号没有这个接口的权限（48001）：**2025-07 起个人主体与未认证企业号已被收回发布权限**，" +
			"需要认证的服务号/订阅号"
	case 40001, 40125:
		return "AppSecret 不对（40001）：确认没多空格；被重置过的话旧的会立刻失效"
	case 45009:
		return "接口调用超出频率限制（45009）：公众号的日调用量按账号算，降低频率或明天再试"
	case 41005, 40007:
		return fmt.Sprintf("素材有问题（%d %s）：封面图要先经「上传图片」传成永久素材，media_id 不能手填", e.Code, e.Msg)
	case 53404:
		return "账号已被限制发布能力（53404）"
	case 53405:
		return "内容不合规被拒（53405）：金融类内容尤其注意荐股、收益承诺一类的表述"
	}
	if e.Msg != "" {
		return fmt.Sprintf("微信报错 %d: %s", e.Code, e.Msg)
	}
	return fmt.Sprintf("微信返回错误码 %d", e.Code)
}

// —— Requests ——

// raw fires a single request. **WeChat packs business errors inside an HTTP 200**, so errcode has
// to be checked every time.
func raw(ctx plugin.Ctx, method, path string, body io.Reader, ctype string, out any) error {
	c := credOf(ctx)
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := clientFor(c.Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("连接微信失败: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("微信返回 HTTP %d", resp.StatusCode)
	}
	var e struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(data, &e)
	if e.ErrCode != 0 {
		return &apiError{Code: e.ErrCode, Msg: e.ErrMsg}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("微信应答无法解析: %w", err)
	}
	return nil
}

// callJSON makes a token-authenticated JSON call, auto-refreshing the token once if it's invalid.
//
// **The body must use unicode-unescaped JSON**: Go escapes Chinese characters to \uXXXX by default,
// and some WeChat endpoints (especially draft content) don't handle this consistently — sending
// the raw text is the safest option.
func callJSON(ctx plugin.Ctx, path string, body any, out any) error {
	return withToken(ctx, func(tok string) error {
		buf, err := jsonBody(body)
		if err != nil {
			return err
		}
		return raw(ctx, http.MethodPost, path+"?access_token="+url.QueryEscape(tok),
			bytes.NewReader(buf), "application/json; charset=utf-8", out)
	})
}

func callGet(ctx plugin.Ctx, path string, out any) error {
	return withToken(ctx, func(tok string) error {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return raw(ctx, http.MethodGet, path+sep+"access_token="+url.QueryEscape(tok), nil, "", out)
	})
}

// callUpload does a multipart upload (material/images).
func callUpload(ctx plugin.Ctx, path, name, mime string, data []byte, out any) error {
	return withToken(ctx, func(tok string) error {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="media"; filename="%s"`, name)}
		h["Content-Type"] = []string{mime}
		part, err := w.CreatePart(h)
		if err != nil {
			return err
		}
		if _, err := part.Write(data); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return raw(ctx, http.MethodPost, path+sep+"access_token="+url.QueryEscape(tok),
			bytes.NewReader(buf.Bytes()), w.FormDataContentType(), out)
	})
}

// withToken fetches a token and runs once; on 40001/42001 (token invalid), it clears the cache and
// retries once. This is exactly the error code multiple replicas produce when they invalidate each
// other's token, and one retry self-heals it.
func withToken(ctx plugin.Ctx, fn func(tok string) error) error {
	tok, err := accessToken(ctx, false)
	if err != nil {
		return err
	}
	err = fn(tok)
	var ae *apiError
	if !asAPIError(err, &ae) || (ae.Code != 40001 && ae.Code != 42001) {
		return err
	}
	tok, err2 := accessToken(ctx, true)
	if err2 != nil {
		return err2
	}
	return fn(tok)
}

func asAPIError(err error, target **apiError) bool {
	if err == nil {
		return false
	}
	e, ok := err.(*apiError)
	if ok {
		*target = e
	}
	return ok
}

// jsonBody produces JSON that doesn't escape Chinese characters (WeChat endpoints handle \uXXXX
// inconsistently; sending the raw text is the safest option).
func jsonBody(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("请求体序列化失败: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
