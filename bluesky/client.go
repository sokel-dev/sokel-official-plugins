package main

// 出站与会话。
//
// AT Protocol 的会话是两段式：createSession 用「账号 + 应用密码」换 accessJwt(几分钟) +
// refreshJwt(长期)。**短期那个由插件缓存与续期**——存进凭证的话，用户得每隔几分钟手动换一次。
//
// 续期策略：先用 refreshJwt 换（refreshSession），换不动就拿密码重新 createSession。
// 两条都试是必要的：refreshJwt 也会过期，而那时不重登就是彻底失联。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const defaultPDS = "https://bsky.social"

// —— HTTP 客户端（按代理缓存）——

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

// —— 会话 ——

type session struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	DID        string `json:"did"`
	Handle     string `json:"handle"`
}

var (
	sessMu    sync.Mutex
	sessCache = map[string]*session{} // pds|identifier|password → 会话
)

func sessKey(c Cred) string {
	return pdsOf(c) + "|" + strings.TrimSpace(c.Identifier) + "|" + strings.TrimSpace(c.AppPassword)
}

func pdsOf(c Cred) string {
	if u := strings.TrimRight(strings.TrimSpace(c.PdsURL), "/"); u != "" {
		return u
	}
	return defaultPDS
}

// login：拿账号密码换会话。
func login(ctx context.Context, hc *http.Client, c Cred) (*session, error) {
	id, pw := strings.TrimSpace(c.Identifier), strings.TrimSpace(c.AppPassword)
	if id == "" || pw == "" {
		return nil, fmt.Errorf("凭证里缺账号或应用专用密码（Bluesky 设置 → Privacy and Security → App Passwords 生成）")
	}
	var s session
	err := rpc(ctx, hc, pdsOf(c), "", http.MethodPost, "com.atproto.server.createSession",
		nil, map[string]any{"identifier": id, "password": pw}, &s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// sessionFor：拿一个可用会话（缓存命中即返回）。
func sessionFor(ctx plugin.Ctx) (*session, *http.Client, Cred, error) {
	c := sokel.CredentialAs[Cred](ctx)
	hc := clientFor(c.Proxy)
	key := sessKey(c)

	sessMu.Lock()
	s, ok := sessCache[key]
	sessMu.Unlock()
	if ok {
		return s, hc, c, nil
	}
	s, err := login(ctx, hc, c)
	if err != nil {
		return nil, nil, c, err
	}
	sessMu.Lock()
	sessCache[key] = s
	sessMu.Unlock()
	return s, hc, c, nil
}

// renew：换一个新会话。先试 refreshJwt，不行再用密码重登。
func renew(ctx context.Context, hc *http.Client, c Cred, old *session) (*session, error) {
	key := sessKey(c)
	if old != nil && old.RefreshJwt != "" {
		var s session
		if err := rpc(ctx, hc, pdsOf(c), old.RefreshJwt, http.MethodPost,
			"com.atproto.server.refreshSession", nil, nil, &s); err == nil && s.AccessJwt != "" {
			sessMu.Lock()
			sessCache[key] = &s
			sessMu.Unlock()
			return &s, nil
		}
	}
	s, err := login(ctx, hc, c)
	if err != nil {
		return nil, err
	}
	sessMu.Lock()
	sessCache[key] = s
	sessMu.Unlock()
	return s, nil
}

// —— XRPC ——

// apiError：AT Protocol 的错误应答（error + message）。
type apiError struct {
	Status  int
	Kind    string
	Message string
}

func (e *apiError) Error() string {
	switch e.Kind {
	case "AuthenticationRequired", "InvalidToken", "ExpiredToken":
		return fmt.Sprintf("Bluesky 拒绝了这个身份（%s）：应用专用密码可能已被撤销，去设置里重新生成一个", e.Message)
	case "AccountTakedown":
		return "这个账号被 Bluesky 停用了"
	case "RateLimitExceeded":
		return "Bluesky 限流（每账号每小时 5000 点、发一条 3 点）——降低发布频率"
	case "BlobTooLarge":
		return "图片太大：Bluesky 单张上限 2MB"
	}
	if e.Message != "" {
		return fmt.Sprintf("Bluesky 报错 %s: %s", e.Kind, e.Message)
	}
	return fmt.Sprintf("Bluesky 返回 HTTP %d", e.Status)
}

// rpc：一次 XRPC 调用。query 非空走 GET，body 非空走 POST。
func rpc(ctx context.Context, hc *http.Client, pds, token, method, nsid string,
	query url.Values, body any, out any) error {
	uri := pds + "/xrpc/" + nsid
	if len(query) > 0 {
		uri += "?" + query.Encode()
	}
	var payload io.Reader
	ctype := ""
	if body != nil {
		switch b := body.(type) {
		case []byte: // 上传 blob：原始字节
			payload = bytes.NewReader(b)
		default:
			raw, err := json.Marshal(b)
			if err != nil {
				return fmt.Errorf("请求体序列化失败: %w", err)
			}
			payload, ctype = bytes.NewReader(raw), "application/json"
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, uri, payload)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if mime, ok := ctx.Value(blobMimeKey{}).(string); ok && mime != "" && ctype == "" {
		req.Header.Set("Content-Type", mime)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Bluesky 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 400 {
		e := &apiError{Status: resp.StatusCode}
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &body)
		e.Kind, e.Message = body.Error, body.Message
		return e
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Bluesky 应答无法解析: %w", err)
	}
	return nil
}

type blobMimeKey struct{}

// call：带鉴权与**一次自动续期**的调用。
//
// accessJwt 只活几分钟，而工作流可能停在人工节点上等半天——不自动续期的话，
// 恢复运行的第一条请求必然 401，且看起来像密码错了。
func call(ctx plugin.Ctx, method, nsid string, query url.Values, body, out any) error {
	s, hc, cred, err := sessionFor(ctx)
	if err != nil {
		return err
	}
	err = rpc(ctx, hc, pdsOf(cred), s.AccessJwt, method, nsid, query, body, out)
	var ae *apiError
	if !asAPIError(err, &ae) || ae.Status != http.StatusUnauthorized {
		return err
	}
	ns, rerr := renew(ctx, hc, cred, s)
	if rerr != nil {
		return rerr
	}
	return rpc(ctx, hc, pdsOf(cred), ns.AccessJwt, method, nsid, query, body, out)
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

// —— 地址互转 ——

// atURI：把 bsky.app 链接换成 at:// 地址（用户手上多半是前者）。
//
//	https://bsky.app/profile/<handle 或 did>/post/<rkey>  →  at://<did>/app.bsky.feed.post/<rkey>
//
// 需要把 handle 解析成 did——链接里给的通常是 handle，而记录地址只认 did。
func atURI(ctx plugin.Ctx, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("帖子地址是空的")
	}
	if strings.HasPrefix(s, "at://") {
		return s, nil
	}
	i := strings.Index(s, "/profile/")
	j := strings.Index(s, "/post/")
	if i < 0 || j < 0 || j < i {
		return "", fmt.Errorf("认不出这个帖子地址：%s（要 at:// 地址或 bsky.app/profile/…/post/… 链接）", raw)
	}
	who := s[i+len("/profile/") : j]
	rkey := strings.Trim(s[j+len("/post/"):], "/")
	did := who
	if !strings.HasPrefix(who, "did:") {
		var out struct {
			DID string `json:"did"`
		}
		if err := call(ctx, http.MethodGet, "com.atproto.identity.resolveHandle",
			url.Values{"handle": {who}}, nil, &out); err != nil {
			return "", fmt.Errorf("解析账号 %s 失败: %w", who, err)
		}
		did = out.DID
	}
	return "at://" + did + "/app.bsky.feed.post/" + rkey, nil
}

// webURL：at:// → 网页链接。发完给链接，下游要发通知时不必自己拼。
func webURL(handle, atURI string) string {
	parts := strings.Split(strings.TrimPrefix(atURI, "at://"), "/")
	if len(parts) != 3 {
		return ""
	}
	who := handle
	if who == "" {
		who = parts[0]
	}
	return "https://bsky.app/profile/" + who + "/post/" + parts[2]
}

func itoa(n int) string { return strconv.Itoa(n) }
