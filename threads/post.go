package main

// 出站与三个操作。
//
// Threads 的发布是**两步 + 异步**：
//   建容器（/threads）→ 拿 creation_id → 发布（/threads_publish）。
// 容器建好之后 Meta 还要在后台把媒体拉下来处理，**立刻发布会被拒**（媒体没就绪）。
// 所以带媒体时要等容器状态变成 FINISHED 再发——不等的话表现是随机失败，重试又能成，
// 最难查的那种。

import (
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

const (
	maxTextLen  = 500
	maxMedia    = 20
	containerTO = 3 * time.Minute
)

// apiBase：**是 var 不是 const**——测试要把它指到假上游上。
var apiBase = "https://graph.threads.net/v1.0"

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
		"（Threads 的令牌 60 天到期，过期后再点一次即可）")
}

// —— 错误 ——

type apiError struct {
	Status  int
	Type    string
	Code    int
	Message string
}

func (e *apiError) Error() string {
	switch {
	case e.Status == http.StatusUnauthorized || e.Code == 190:
		return fmt.Sprintf("Threads 拒绝了这个令牌（%s）：**多半是 60 天到期了**——"+
			"到凭证页重新点一次「授权」。（Threads 没有刷新令牌，这是常态）", e.Message)
	case e.Code == 4 || e.Status == http.StatusTooManyRequests:
		return fmt.Sprintf("Threads 限流或超出配额（%s）：每 24 小时 250 条，"+
			"用「检查凭证」能看到剩余额度", e.Message)
	case e.Code == 10 || e.Status == http.StatusForbidden:
		return fmt.Sprintf("Threads 拒绝了这个操作（%s）：授权时缺 threads_content_publish 作用域，"+
			"或这个账号不是 Threads 专业/创作者账号", e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("Threads 报错 %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("Threads 返回 HTTP %d", e.Status)
}

// call：一次 Graph 调用。参数走 query（Meta 的写接口也收 query，比 JSON 省事且与文档一致）。
func call(ctx plugin.Ctx, method, path string, params url.Values, out any) error {
	tok, err := tokenOf(ctx)
	if err != nil {
		return err
	}
	if params == nil {
		params = url.Values{}
	}
	params.Set("access_token", tok)
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := clientFor(credOf(ctx).Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("连接 Threads 失败（在境外，部署环境可能要在凭证里配出站代理）: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	var wrap struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &wrap)
	if resp.StatusCode >= 400 || wrap.Error.Message != "" {
		return &apiError{Status: resp.StatusCode, Type: wrap.Error.Type,
			Code: wrap.Error.Code, Message: wrap.Error.Message}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Threads 应答无法解析: %w", err)
	}
	return nil
}

// —— 账号 ——

var (
	meMu    sync.Mutex
	meCache = map[string]meInfo{}
)

type meInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// me：授权账号。所有发布路径都以它的 id 开头，而它不会变——按 token 缓存。
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
	if err := call(ctx, http.MethodGet, "/me", url.Values{"fields": {"id,username"}}, &out); err != nil {
		return meInfo{}, err
	}
	if out.ID == "" {
		return meInfo{}, fmt.Errorf("Threads 没返回账号 id（授权时是不是漏了 threads_basic？）")
	}
	meMu.Lock()
	meCache[tok] = out
	meMu.Unlock()
	return out, nil
}

// —— 发布 ——

type publishOpts struct {
	Text         string
	Media        []*plugin.File
	MediaURLs    []string
	ReplyTo      string
	ReplyControl string
}

// publish：建容器 →（带媒体时）等就绪 → 发布。返回帖子 id。
func publish(ctx plugin.Ctx, o publishOpts) (string, error) {
	text := strings.TrimSpace(o.Text)
	urls := mediaURLs(o.Media, o.MediaURLs)
	if text == "" && len(urls) == 0 {
		return "", fmt.Errorf("正文与媒体至少要有一样")
	}
	if n := len([]rune(text)); n > maxTextLen {
		return "", fmt.Errorf("正文 %d 个字符，超过 Threads 的 %d 上限（长内容请用「发帖串」）", n, maxTextLen)
	}
	if len(urls) > maxMedia {
		return "", fmt.Errorf("一条最多 %d 个媒体，给了 %d 个", maxMedia, len(urls))
	}
	who, err := me(ctx)
	if err != nil {
		return "", err
	}

	var creationID string
	switch {
	case len(urls) == 0:
		creationID, err = createContainer(ctx, who.ID, url.Values{
			"media_type": {"TEXT"}, "text": {text},
		}, o)
	case len(urls) == 1:
		p := url.Values{"media_type": {mediaType(urls[0])}, "text": {text}}
		p.Set(mediaParam(urls[0]), urls[0])
		creationID, err = createContainer(ctx, who.ID, p, o)
	default:
		// 轮播：每个媒体先建一个 is_carousel_item 容器，再建一个 CAROUSEL 容器把它们串起来。
		children := make([]string, 0, len(urls))
		for i, u := range urls {
			p := url.Values{"media_type": {mediaType(u)}, "is_carousel_item": {"true"}}
			p.Set(mediaParam(u), u)
			id, cerr := createContainer(ctx, who.ID, p, publishOpts{})
			if cerr != nil {
				return "", fmt.Errorf("第 %d 个媒体建容器失败: %w", i+1, cerr)
			}
			children = append(children, id)
		}
		creationID, err = createContainer(ctx, who.ID, url.Values{
			"media_type": {"CAROUSEL"}, "text": {text}, "children": {strings.Join(children, ",")},
		}, o)
	}
	if err != nil {
		return "", err
	}

	// 带媒体的容器要等 Meta 把文件拉下来处理完。不等的话发布会被拒，
	// 而重试又能成——最难查的那种随机失败。
	if len(urls) > 0 {
		if err := waitReady(ctx, creationID); err != nil {
			return "", err
		}
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := call(ctx, http.MethodPost, "/"+who.ID+"/threads_publish",
		url.Values{"creation_id": {creationID}}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("Threads 没返回帖子 id")
	}
	return out.ID, nil
}

func createContainer(ctx plugin.Ctx, userID string, params url.Values, o publishOpts) (string, error) {
	if id := strings.TrimSpace(o.ReplyTo); id != "" {
		params.Set("reply_to_id", id)
	}
	if rc := strings.TrimSpace(o.ReplyControl); rc != "" && rc != "everyone" {
		params.Set("reply_control", rc)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := call(ctx, http.MethodPost, "/"+userID+"/threads", params, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("Threads 没返回容器 id")
	}
	return out.ID, nil
}

// waitReady：等容器状态变成 FINISHED。
func waitReady(ctx plugin.Ctx, creationID string) error {
	deadline := time.Now().Add(containerTO)
	for time.Now().Before(deadline) {
		var st struct {
			Status      string `json:"status"`
			ErrorMsg    string `json:"error_message"`
			StatusField string `json:"status_field"`
		}
		if err := call(ctx, http.MethodGet, "/"+creationID,
			url.Values{"fields": {"status,error_message"}}, &st); err != nil {
			return err
		}
		switch strings.ToUpper(st.Status) {
		case "FINISHED", "PUBLISHED", "":
			return nil
		case "ERROR", "EXPIRED":
			return fmt.Errorf("Threads 处理媒体失败（%s）：%s——多半是那个地址它下载不到，"+
				"或格式不支持（图 JPEG/PNG、视频 MP4）", st.Status, st.ErrorMsg)
		}
		t := time.NewTimer(3 * time.Second)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
	return fmt.Errorf("等媒体就绪超时（容器 %s）：大视频可稍后重试", creationID)
}

// —— 操作 ——

func opPostCreate(ctx plugin.Ctx, in *ThPostCreateIn) (*ThPostCreateOut, error) {
	id, err := publish(ctx, publishOpts{
		Text: in.Text, Media: in.Media, MediaURLs: in.MediaUrls,
		ReplyTo: in.ReplyToID, ReplyControl: in.ReplyControl,
	})
	if err != nil {
		return nil, err
	}
	who, _ := me(ctx)
	return &ThPostCreateOut{ID: id, URL: postURL(who.Username, id)}, nil
}

func opPostThread(ctx plugin.Ctx, in *ThPostThreadIn) (*ThPostThreadOut, error) {
	texts := nonEmpty(in.Texts)
	if len(texts) == 0 {
		return nil, fmt.Errorf("一条正文都没有")
	}
	gap := time.Duration(in.IntervalMs) * time.Millisecond
	if in.IntervalMs <= 0 {
		gap = time.Second
	}
	ids := make([]string, 0, len(texts))
	prev := strings.TrimSpace(in.ReplyToID)
	for i, t := range texts {
		o := publishOpts{Text: t, ReplyTo: prev}
		if i == 0 {
			o.Media = in.Media
		}
		id, err := publish(ctx, o)
		if err != nil {
			// 中途失败**不回滚**：前面几条已经公开了，删掉是二次破坏。
			return nil, fmt.Errorf("帖串发到第 %d 条失败（前 %d 条已发出：%s）: %w",
				i+1, len(ids), strings.Join(ids, ","), err)
		}
		ids = append(ids, id)
		prev = id
		if i < len(texts)-1 && gap > 0 {
			t := time.NewTimer(gap)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("帖串被中断（已发出 %d 条：%s）", len(ids), strings.Join(ids, ","))
			}
		}
	}
	who, _ := me(ctx)
	return &ThPostThreadOut{IDs: ids, RootID: ids[0], RootURL: postURL(who.Username, ids[0]), Count: len(ids)}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	who, err := me(ctx)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	out := &HealthCheckOut{OK: true, Username: who.Username, Message: "@" + who.Username}
	// 剩余额度顺带带出来：每 24 小时 250 条是账号级配额，工作流可以据此决定还发不发。
	// 查不到不影响结论——凭证本身是好的。
	var q struct {
		Data []struct {
			Usage int `json:"quota_usage"`
			Total int `json:"config"`
			Cfg   struct {
				QuotaTotal int `json:"quota_total"`
			} `json:"quota_config"`
		} `json:"data"`
	}
	if err := call(ctx, http.MethodGet, "/"+who.ID+"/threads_publishing_limit",
		url.Values{"fields": {"quota_usage,config"}}, &q); err == nil && len(q.Data) > 0 {
		out.QuotaUsed = q.Data[0].Usage
		out.QuotaTotal = q.Data[0].Cfg.QuotaTotal
		if out.QuotaTotal == 0 {
			out.QuotaTotal = 250
		}
		out.Message = fmt.Sprintf("@%s（24 小时内已发 %d/%d）", who.Username, out.QuotaUsed, out.QuotaTotal)
	}
	return out, nil
}

// —— 小工具 ——

// mediaURLs：文件与外部地址合成一份。
//
// **平台会把文件引用改写成带签名的下载地址**（正是为这类「上游自己来拉」的接口准备的），
// 所以这里直接用 File.URL；拿不到绝对地址就说清楚，别让 Threads 去下载一个相对路径。
func mediaURLs(files []*plugin.File, urls []string) []string {
	out := make([]string, 0, len(files)+len(urls))
	for _, f := range files {
		if f == nil {
			continue
		}
		if u := strings.TrimSpace(f.URL); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			out = append(out, u)
		}
	}
	out = append(out, nonEmpty(urls)...)
	return out
}

// mediaType / mediaParam：按扩展名判图还是视频。Threads 的两个参数名不同（image_url / video_url），
// 给错了它会说「缺少必需参数」，而不会告诉你给错了哪个。
func mediaType(u string) string {
	if isVideo(u) {
		return "VIDEO"
	}
	return "IMAGE"
}

func mediaParam(u string) string {
	if isVideo(u) {
		return "video_url"
	}
	return "image_url"
}

func isVideo(u string) bool {
	low := strings.ToLower(u)
	if i := strings.IndexAny(low, "?#"); i > 0 {
		low = low[:i]
	}
	for _, ext := range []string{".mp4", ".mov", ".m4v", ".webm"} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return false
}

func postURL(username, id string) string {
	if id == "" {
		return ""
	}
	if username == "" {
		return "https://www.threads.net/t/" + id
	}
	return "https://www.threads.net/@" + username + "/post/" + id
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var _ = strconv.Itoa
