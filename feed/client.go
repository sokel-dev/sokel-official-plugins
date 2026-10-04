package main

// 出站与雪球的匿名令牌。
//
// 雪球的**读**接口（api.xueqiu.com）与发帖那套完全不同：它只要一个匿名令牌，
// 访问一次首页就会在 Set-Cookie 里给 xq_a_token——**不需要用户登录**。
// RSSHub 现在用 Playwright 拿它（为了抗风控），我们先用普通 HTTP 试；
// 拿不到就让用户在凭证里粘一条真 Cookie，不为这一步引一个浏览器。

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

// 站点地址：**是 var 不是 const**——测试要把它们指到假上游上。
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

// get：一次 GET，返回正文。
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

// —— 雪球匿名令牌 ——

var (
	tokMu    sync.Mutex
	tokCache = map[string]tokenEntry{}
)

type tokenEntry struct {
	cookie string
	exp    time.Time
}

// xueqiuCookie：凭证里粘了就用粘的；否则访问首页取匿名令牌（缓存 30 分钟）。
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

	// **打 /hq 而不是首页**：实测首页只回阿里云 WAF 的 acw_tc，不发 xq_a_token；
	// /hq（行情页）才发。RSSHub 为这一步改用了 Playwright，其实换个入口就够——
	// 换回首页的话表现是「一直取不到令牌」，而错误信息会把人引向「粘 Cookie」那条路。
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

// readAll：读应答体，封个顶。
func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 32<<20))
}
