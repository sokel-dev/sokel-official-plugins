package main

// Outbound calls and three operations.
//
// Three things that don't show up elsewhere:
//   - **Two mandatory headers**: LinkedIn-Version (YYYYMM) and X-Restli-Protocol-Version: 2.0.0.
//     Miss either one and the endpoint returns 426 or a mismatched shape, and the error message
//     won't tell you a header is missing.
//   - **author must be the account's own URN** (urn:li:person:{sub}), fetched from /v2/userinfo;
//     it never changes, so it's cached by token.
//   - **Images are a three-step process**: initializeUpload gets an upload URL and a URN → PUT the
//     binary → splice the URN into the post. Not the same shape as other platforms' "upload, get
//     an id back".

import (
	"bytes"
	"encoding/json"
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

const (
	apiBaseDefault = "https://api.linkedin.com"
	// linkedInVersion: the version header, YYYYMM. LinkedIn releases one every month and
	// deprecates old ones roughly a year later — pin a known-good one; upgrading is a deliberate
	// action, not something that silently changes shape in production one day.
	linkedInVersion = "202601"
	maxTextLen      = 3000
	maxImages       = 9
	maxImageBytes   = 10 << 20
)

// apiBase is **a var, not a const** — tests need to point it at a fake upstream.
var apiBase = apiBaseDefault

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
	c := &http.Client{Timeout: 90 * time.Second}
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

func tokenOf(ctx plugin.Ctx) (string, error) {
	if t := strings.TrimSpace(credOf(ctx).AccessToken); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("这条凭证还没授权：到凭证页点「授权」。" +
		"（LinkedIn 的令牌 60 天到期，过期后再点一次即可）")
}

// —— Errors ——

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("LinkedIn 拒绝了这个令牌（401 %s）：**多半是 60 天到期了**——"+
			"到凭证页重新点一次「授权」。（自助接入的应用没有刷新令牌，这是常态）", e.Message)
	case http.StatusForbidden:
		return fmt.Sprintf("LinkedIn 拒绝了这个操作（403 %s）：授权时缺 w_member_social 作用域，"+
			"或应用没加「Share on LinkedIn」产品", e.Message)
	case http.StatusTooManyRequests:
		return "LinkedIn 限流（429）：个人号是 150 次/人/天、10 万次/应用/天"
	case 426:
		return "LinkedIn 说版本头不对（426）：插件钉的 LinkedIn-Version 可能已被停用，需要升一版"
	}
	if e.Message != "" {
		return fmt.Sprintf("LinkedIn 报错 %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("LinkedIn 返回 HTTP %d", e.Status)
}

// call makes a single REST call. **Both headers must be sent**; missing either one means a 426 or
// a mismatched shape.
func call(ctx plugin.Ctx, method, path string, body any, out any) error {
	tok, err := tokenOf(ctx)
	if err != nil {
		return err
	}
	var payload io.Reader
	ctype := ""
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("请求体序列化失败: %w", err)
		}
		payload, ctype = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("LinkedIn-Version", linkedInVersion)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := clientFor(credOf(ctx).Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("连接 LinkedIn 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 400 {
		e := &apiError{Status: resp.StatusCode}
		var b struct {
			Message   string `json:"message"`
			Code      string `json:"code"`
			ServiceEC int    `json:"serviceErrorCode"`
		}
		_ = json.Unmarshal(raw, &b)
		e.Code, e.Message = b.Code, b.Message
		return e
	}
	// On a successful post, the body can be empty — the id is in the header (x-restli-id). This is
	// an old LinkedIn convention.
	if id := resp.Header.Get("x-restli-id"); id != "" {
		if p, ok := out.(*createdPost); ok {
			p.ID = id
			return nil
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("LinkedIn 应答无法解析: %w", err)
	}
	return nil
}

type createdPost struct {
	ID string `json:"id"`
}

// —— Account URN ——

var (
	meMu    sync.Mutex
	meCache = map[string]meInfo{} // access_token → account
)

type meInfo struct {
	Sub  string `json:"sub"`
	Name string `json:"name"`
}

// me is the authorized account. A post's author must be its URN, and that URN never changes —
// cached by token.
func me(ctx plugin.Ctx) (meInfo, error) {
	tok, err := tokenOf(ctx)
	if err != nil {
		return meInfo{}, err
	}
	meMu.Lock()
	m, ok := meCache[tok]
	meMu.Unlock()
	if ok {
		return m, nil
	}
	var out meInfo
	if err := call(ctx, http.MethodGet, "/v2/userinfo", nil, &out); err != nil {
		return meInfo{}, err
	}
	if out.Sub == "" {
		return meInfo{}, fmt.Errorf("LinkedIn 没返回账号标识（授权时是不是漏了 openid/profile 作用域？）")
	}
	meMu.Lock()
	meCache[tok] = out
	meMu.Unlock()
	return out, nil
}

// —— Operations ——

func opPostCreate(ctx plugin.Ctx, in *LiPostCreateIn) (*LiPostCreateOut, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" && len(in.Images) == 0 {
		return nil, fmt.Errorf("正文与图片至少要有一样")
	}
	if n := len([]rune(text)); n > maxTextLen {
		return nil, fmt.Errorf("正文 %d 个字符，超过 LinkedIn 的 %d 上限", n, maxTextLen)
	}
	if len(in.Images) > maxImages {
		return nil, fmt.Errorf("一条动态最多 %d 张图，给了 %d 张", maxImages, len(in.Images))
	}
	who, err := me(ctx)
	if err != nil {
		return nil, err
	}
	author := "urn:li:person:" + who.Sub

	vis := strings.TrimSpace(in.Visibility)
	if vis == "" {
		vis = "PUBLIC"
	}
	body := map[string]any{
		"author":     author,
		"commentary": text,
		"visibility": vis,
		// Reshare/reply visibility settings: omitting this gets some accounts flagged as missing a field.
		"distribution": map[string]any{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []any{},
			"thirdPartyDistributionChannels": []any{},
		},
		"lifecycleState":            "PUBLISHED",
		"isReshareDisabledByAuthor": false,
	}
	if len(in.Images) > 0 {
		content, err := imagesContent(ctx, author, in.Images, in.ImageAlts)
		if err != nil {
			return nil, err
		}
		body["content"] = content
	}

	var out createdPost
	if err := call(ctx, http.MethodPost, "/rest/posts", body, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("LinkedIn 没返回动态 id")
	}
	return &LiPostCreateOut{ID: out.ID, URL: postURL(out.ID)}, nil
}

func opPostDelete(ctx plugin.Ctx, in *LiPostDeleteIn) (*LiPostDeleteOut, error) {
	id := postURN(in.PostID)
	if id == "" {
		return nil, fmt.Errorf("动态 URN 是空的")
	}
	if err := call(ctx, http.MethodDelete, "/rest/posts/"+url.PathEscape(id), nil, nil); err != nil {
		return nil, err
	}
	return &LiPostDeleteOut{Deleted: true}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	who, err := me(ctx)
	if err != nil {
		// Unavailable is a conclusion, not a fault: the platform uses it to write the credential's
		// status.
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Name: who.Name, Message: who.Name}, nil
}

// —— Images ——
//
// Three steps: initializeUpload gets an upload URL and a URN → PUT the binary → splice the URN
// into the post. Not the same shape as other platforms' "upload, get an id back directly" — the
// second step uses a temporary URL LinkedIn hands back (not the API domain).

func imagesContent(ctx plugin.Ctx, author string, files []*plugin.File, alts []string) (map[string]any, error) {
	urns := make([]map[string]any, 0, len(files))
	for i, f := range files {
		if f == nil || f.ID == "" {
			continue
		}
		data, err := ctx.Fetch(f)
		if err != nil {
			return nil, fmt.Errorf("取第 %d 张图失败: %w", i+1, err)
		}
		if len(data) > maxImageBytes {
			return nil, fmt.Errorf("第 %d 张图 %.1fMB，超过 LinkedIn 的 10MB 上限", i+1, float64(len(data))/(1<<20))
		}
		urn, err := uploadImage(ctx, author, data)
		if err != nil {
			return nil, fmt.Errorf("传第 %d 张图失败: %w", i+1, err)
		}
		item := map[string]any{"id": urn}
		if i < len(alts) && strings.TrimSpace(alts[i]) != "" {
			item["altText"] = alts[i]
		}
		urns = append(urns, item)
	}
	if len(urns) == 0 {
		return nil, fmt.Errorf("一张有效的图都没有")
	}
	if len(urns) == 1 {
		return map[string]any{"media": urns[0]}, nil
	}
	return map[string]any{"multiImage": map[string]any{"images": urns}}, nil
}

func uploadImage(ctx plugin.Ctx, author string, data []byte) (string, error) {
	var init struct {
		Value struct {
			UploadURL string `json:"uploadUrl"`
			Image     string `json:"image"`
		} `json:"value"`
	}
	if err := call(ctx, http.MethodPost, "/rest/images?action=initializeUpload",
		map[string]any{"initializeUploadRequest": map[string]any{"owner": author}}, &init); err != nil {
		return "", err
	}
	if init.Value.UploadURL == "" || init.Value.Image == "" {
		return "", fmt.Errorf("LinkedIn 没返回上传地址")
	}
	// The second step PUTs to the temporary URL LinkedIn hands back (not the API domain, and no
	// version header either).
	tok, err := tokenOf(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, init.Value.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := clientFor(credOf(ctx).Proxy).Do(req)
	if err != nil {
		return "", fmt.Errorf("上传图片字节失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("上传图片字节被拒（HTTP %d）", resp.StatusCode)
	}
	return init.Value.Image, nil
}

// —— URLs ——

// postURN handles that a user might paste in a post link instead of a URN.
//
//	https://www.linkedin.com/feed/update/urn:li:share:7123/ → urn:li:share:7123
func postURN(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "urn:li:") {
		return s
	}
	if i := strings.Index(s, "urn:li:"); i >= 0 {
		s = s[i:]
		if j := strings.IndexAny(s, "/?#"); j > 0 {
			s = s[:j]
		}
		return s
	}
	return ""
}

func postURL(urn string) string {
	if urn == "" {
		return ""
	}
	return "https://www.linkedin.com/feed/update/" + urn + "/"
}
