package main

// Outbound requests and Xueqiu's anonymous token.
//
// Xueqiu's **read** API (api.xueqiu.com) is completely separate from the posting API: it only
// needs an anonymous token, and one hit on the homepage returns xq_a_token in Set-Cookie —
// **no user login required**. RSSHub now fetches it with Playwright (to dodge anti-bot
// measures); we try a plain HTTP request first. If that fails, we let the user paste a real
// cookie into the credential instead of pulling in a browser for this one step.

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

// Site addresses: **var, not const** — tests need to point these at a fake upstream.
var (
	xueqiuSite = "https://xueqiu.com"
	xueqiuAPI  = "https://api.xueqiu.com"
)

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
	c := &http.Client{Timeout: 45 * time.Second}
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

func uaOf(c Cred) string {
	if ua := strings.TrimSpace(c.UserAgent); ua != "" {
		return ua
	}
	return defaultUA
}

// get performs a single GET and returns the body.
func get(ctx plugin.Ctx, uri, cookie, referer string) ([]byte, error) {
	cred := credOf(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", uaOf(cred))
	req.Header.Set("Accept", "*/*")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", hostOf(uri), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s 返回 HTTP %d：%s", hostOf(uri), resp.StatusCode, clip(raw, 160))
	}
	return raw, nil
}

// —— Xueqiu anonymous token ——

var (
	tokMu    sync.Mutex
	tokCache = map[string]tokenEntry{}
)

type tokenEntry struct {
	cookie string
	exp    time.Time
}

// xueqiuCookie uses whatever cookie the user pasted into the credential, if any; otherwise it
// hits the homepage to obtain an anonymous token (cached for 30 minutes).
func xueqiuCookie(ctx plugin.Ctx) (string, error) {
	cred := credOf(ctx)
	if c := strings.TrimSpace(cred.XueqiuCookie); c != "" {
		return c, nil
	}
	key := cred.Proxy + "|" + uaOf(cred)
	tokMu.Lock()
	e, ok := tokCache[key]
	tokMu.Unlock()
	if ok && time.Now().Before(e.exp) {
		return e.cookie, nil
	}

	// **Hit /hq, not the homepage**: in practice the homepage only returns Alibaba Cloud
	// WAF's acw_tc cookie, not xq_a_token; /hq (the quotes page) does issue it. RSSHub
	// switched to Playwright for this step, but changing the entry point is actually
	// enough — reverting to the homepage shows up as "the token never arrives," and the
	// error message would then steer people toward pasting a cookie instead.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xueqiuSite+"/hq", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", uaOf(cred))
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return "", fmt.Errorf("取雪球匿名令牌失败（连不上）: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	var parts []string
	for _, c := range resp.Cookies() {
		if c.Name == "xq_a_token" || c.Name == "xqat" || c.Name == "u" || c.Name == "device_id" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("雪球没给匿名令牌——多半被风控挡了：" +
			"把浏览器里的整条 Cookie 粘进凭证的「雪球 Cookie」再试")
	}
	ck := strings.Join(parts, "; ")
	tokMu.Lock()
	tokCache[key] = tokenEntry{cookie: ck, exp: time.Now().Add(30 * time.Minute)}
	tokMu.Unlock()
	return ck, nil
}

func hostOf(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Host != "" {
		return u.Host
	}
	return uri
}

func clip(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// readAll reads the response body with a size cap.
func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 32<<20))
}
