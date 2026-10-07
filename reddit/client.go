package main

import (
	"context"
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

// base is a variable so tests can point it at a recording server.
var base = "https://www.reddit.com"

const userAgent = "sokel-reddit-plugin/0.2 (+https://github.com/sokel-dev/sokel-official-plugins)"

var (
	clientsMu sync.Mutex
	clients   = map[string]*http.Client{}
)

func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 30 * time.Second}
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

// pacer serialises requests and honours Reddit's one-per-minute window for unauthenticated feeds (measured
// 2026-10-07: x-ratelimit-remaining is 0 after a single request and x-ratelimit-reset counts down from ~60 s).
// A 429 sets the next slot from the reset header and the request is retried once. Process-wide: the window is per
// IP, so operations and the watch loop share it.
type pacer struct {
	mu   sync.Mutex
	next time.Time
	// window is what a request costs when the server does not say; tests shrink it.
	window time.Duration
}

var pace = &pacer{window: 61 * time.Second}

// maxWait caps how long one request may sit in the queue; beyond it the window is just not coming back in time.
const maxWait = 130 * time.Second

func (p *pacer) wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		d := time.Until(p.next)
		if d > maxWait {
			p.mu.Unlock()
			return fmt.Errorf("Reddit 的额度要等 %s 才恢复（不登录一分钟只让取一次）", d.Round(time.Second))
		}
		if d <= 0 {
			// Claim the slot before releasing the lock: a concurrent caller then queues behind this request's window.
			p.next = time.Now().Add(p.window)
			p.mu.Unlock()
			return nil
		}
		p.mu.Unlock()
		t := time.NewTimer(d)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}

func (p *pacer) observe(resp *http.Response) {
	reset, err := strconv.ParseFloat(strings.TrimSpace(resp.Header.Get("x-ratelimit-reset")), 64)
	if err != nil {
		return
	}
	p.mu.Lock()
	// Trust the server's clock over the fixed window: a slot that opens earlier is taken earlier, a later one waited for.
	p.next = time.Now().Add(time.Duration(reset+1) * time.Second)
	p.mu.Unlock()
}

// fetchFeed GETs one feed through the pacer and parses it. Reddit answers 429 to the second request in a window,
// so one 429 is waited out and retried; a second one is an error (someone else shares this IP's window).
func fetchFeed(ctx context.Context, c Cred, path string, query url.Values) ([]item, error) {
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	for attempt := 0; ; attempt++ {
		if err := pace.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/atom+xml, application/xml;q=0.9, */*;q=0.5")
		resp, err := clientFor(c.Proxy).Do(req)
		if err != nil {
			return nil, fmt.Errorf("连不上 Reddit（%v）。部署环境在国内时，到凭证里填「出站代理」", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		pace.observe(resp)
		switch {
		case resp.StatusCode == http.StatusTooManyRequests && attempt == 0:
			continue
		case resp.StatusCode == http.StatusTooManyRequests:
			return nil, fmt.Errorf("Reddit 限流（429）：不登录一分钟只让取一次，等了一轮仍被拒——这个出口 IP 上还有别的请求在用额度")
		case resp.StatusCode == http.StatusNotFound:
			return nil, fmt.Errorf("Reddit 上没有这个地址（404）：%s", path)
		case resp.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("Reddit 拒绝了这个请求（403）：%s。私密版块、被封禁的用户，或这个出口 IP 被 Reddit 拦了", path)
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("Reddit 回 HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 200)])))
		}
		return parseFeed(raw)
	}
}

var (
	postIDRe   = regexp.MustCompile(`^(?:t3_)?([a-z0-9]{4,10})$`)
	postLinkRe = regexp.MustCompile(`reddit\.com/(?:r/[^/]+/)?comments/([a-z0-9]+)`)
	subsRe     = regexp.MustCompile(`^[A-Za-z0-9_]+(\+[A-Za-z0-9_]+)*$`)
	userRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{3,20}$`)
)

// postIDOf accepts a post id (with or without t3_) or a post address.
func postIDOf(s string) (string, error) {
	s = strings.TrimSpace(s)
	if m := postLinkRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1], nil
	}
	if m := postIDRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1], nil
	}
	return "", fmt.Errorf("认不出「%s」：填帖子 id（如 1abc2de）或 reddit.com/r/…/comments/… 地址", s)
}

func postIDList(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\n' }) {
		if id, err := postIDOf(part); err == nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func subsOf(s string) (string, error) {
	s = strings.Trim(strings.TrimSpace(s), "/")
	s = strings.TrimPrefix(s, "r/")
	s = strings.ReplaceAll(s, ",", "+")
	if s == "" {
		return "", nil
	}
	if !subsRe.MatchString(s) {
		return "", fmt.Errorf("版块写成 golang 或 golang+selfhosted（不带 r/）：「%s」认不出", s)
	}
	return s, nil
}

func userOf(s string) (string, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "/"), "u/")
	if !userRe.MatchString(s) {
		return "", fmt.Errorf("Reddit 用户名是 3–20 位字母数字下划线（不带 u/）：「%s」认不出", s)
	}
	return s, nil
}
