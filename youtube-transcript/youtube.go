package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/youtube-transcript/schema"
)

// 取字幕走的是 YouTube **网页客户端自己在用**的那套未公开接口，与 Data API v3 无关：
//
//	① GET  watch?v=<id>                      → 抠出 INNERTUBE_API_KEY（顺带识别风控页）
//	② POST youtubei/v1/player?key=<key>      → 拿字幕轨清单（captionTracks）
//	③ GET  <track.baseUrl>                   → timedtext XML，就是字幕内容
//
// 为什么不走 Data API v3：那条路要 key、有配额，而且 captions.download **只能下自己频道的**，
// 别人的视频一律 403 —— 也就是说它根本做不了这件事。
//
// 客户端伪装成 ANDROID（与参考项目一致）：网页客户端近年会要 PO Token，
// 而 Android 客户端这条路目前还不要。这是**会被 YouTube 改掉**的东西，
// 所以常量单独摆在这里，改起来只有一处。

const (
	watchURL      = "https://www.youtube.com/watch?v=%s"
	innertubeURL  = "https://www.youtube.com/youtubei/v1/player?key=%s"
	defaultUA     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	healthVideoID = "dQw4w9WgXcQ" // 体检用的公开视频：十几年没下架过，字幕齐全
)

// innertubeContext：伪装的客户端身份。版本号会过期，过期的症状是 player 接口开始要 PO Token。
var innertubeContext = map[string]any{
	"client": map[string]any{"clientName": "ANDROID", "clientVersion": "20.10.38"},
}

var (
	apiKeyRe  = regexp.MustCompile(`"INNERTUBE_API_KEY":\s*"([A-Za-z0-9_-]+)"`)
	consentRe = regexp.MustCompile(`name="v" value="(.*?)"`)
)

// client：一次调用的 HTTP 门面（代理与 UA 来自凭证）。
type client struct {
	http *http.Client
	ua   string
}

// newClient：代理有两个来源，**都得管用**。
//
//	① 凭证里的「出站代理」——线上正解（住宅代理对付 YouTube 封机房 IP）；
//	② 进程的 HTTP(S)_PROXY 环境变量——本地开发的正解（国内直连到不了 youtube.com）。
//
// 回落 ProxyFromEnvironment 这一句是必需的：自造的 http.Transport 其 Proxy 字段默认 nil，
// 那不是「用默认」，是**显式关掉**代理（带 ProxyFromEnvironment 的是 http.DefaultTransport）。
// 少了它，本地 export 了 HTTPS_PROXY 也连不上，且症状看起来像普通网络故障。
func newClient(proxy, ua string) (*client, error) {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if proxy = strings.TrimSpace(proxy); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("代理地址不合法 %q：应形如 http://user:pass@host:port 或 socks5://host:1080", proxy)
		}
		tr.Proxy = http.ProxyURL(u) // 凭证优先于环境变量：线上不该被宿主环境悄悄改道
	}
	if ua = strings.TrimSpace(ua); ua == "" {
		ua = defaultUA
	}
	return &client{http: &http.Client{Transport: tr, Timeout: 30 * time.Second}, ua: ua}, nil
}

func (c *client) do(ctx context.Context, req *http.Request) (string, int, error) {
	req.Header.Set("User-Agent", c.ua)
	// Accept-Language 决定 YouTube 返回的**轨道显示名**语言（不影响有哪些轨）。
	// 钉成 en-US 是为了让 name.runs[0].text 稳定，否则同一个视频在不同出口 IP 下
	// 显示名会变，下游按名字判断就会飘。
	req.Header.Set("Accept-Language", "en-US")
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return "", 0, fmt.Errorf("请求 YouTube 失败（检查出站网络/代理）: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("读取响应失败: %w", err)
	}
	return string(b), resp.StatusCode, nil
}

// errBlocked：风控。单独成一类是因为它的**处置方式和别的错完全不同**——
// 不是参数写错了，是这台机器的出口 IP 被 YouTube 拒了，唯一的解法是配代理。
func errBlocked(detail string) error {
	return fmt.Errorf("被 YouTube 风控挡下（%s）。云主机/机房 IP 基本都会撞到这个；"+
		"解法是在本插件的凭证里配一个**住宅代理**出站。本插件不需要 YouTube 账号，配了代理即可", detail)
}

// fetchAPIKey 取 watch 页并抠出 InnerTube key，顺带识别同意页与风控页。
func (c *client) fetchAPIKey(ctx context.Context, videoID string) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf(watchURL, videoID), nil)
	body, status, err := c.do(ctx, req)
	if err != nil {
		return "", err
	}
	if status == http.StatusTooManyRequests {
		return "", errBlocked("HTTP 429")
	}
	if strings.Contains(body, `class="g-recaptcha"`) {
		return "", errBlocked("返回了人机验证页")
	}
	// 欧盟同意页：需要带一个 CONSENT cookie 再来一次。
	if strings.Contains(body, `action="https://consent.youtube.com/s"`) {
		m := consentRe.FindStringSubmatch(body)
		if m == nil {
			return "", fmt.Errorf("撞上 YouTube 同意页且取不到同意参数——换个出口地区（或配代理）再试")
		}
		req2, _ := http.NewRequest(http.MethodGet, fmt.Sprintf(watchURL, videoID), nil)
		req2.AddCookie(&http.Cookie{Name: "CONSENT", Value: "YES+" + m[1], Domain: ".youtube.com"})
		if body, _, err = c.do(ctx, req2); err != nil {
			return "", err
		}
	}
	m := apiKeyRe.FindStringSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("在视频页里找不到 InnerTube key —— 多半是 YouTube 改了页面结构（本插件需要更新），也可能是被风控挡了")
	}
	return m[1], nil
}

// playerResp：player 接口里我们真正要读的那几块。
type playerResp struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	Captions struct {
		Renderer struct {
			CaptionTracks []struct {
				BaseURL string `json:"baseUrl"`
				Name    struct {
					Runs []struct {
						Text string `json:"text"`
					} `json:"runs"`
					SimpleText string `json:"simpleText"`
				} `json:"name"`
				LanguageCode   string `json:"languageCode"`
				Kind           string `json:"kind"` // "asr" = 自动生成
				IsTranslatable bool   `json:"isTranslatable"`
			} `json:"captionTracks"`
			TranslationLanguages []struct {
				LanguageCode string `json:"languageCode"`
			} `json:"translationLanguages"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
}

// checkPlayability 把 YouTube 的状态码翻译成人话。
//
// 这一步是本插件用户体验的大头：这几种情况**都不是插件坏了**，但如果只回一句
// 「取字幕失败」，用户没有任何办法判断该改什么。
func checkPlayability(status, reason string) error {
	switch status {
	case "", "OK":
		return nil
	case "LOGIN_REQUIRED":
		if strings.Contains(reason, "not a bot") || strings.Contains(reason, "Sign in to confirm") {
			return errBlocked("YouTube 要求「确认你不是机器人」")
		}
		return fmt.Errorf("这个视频有年龄限制，需要登录才能看，本插件取不到它的字幕（YouTube 已废弃 cookie 认证那条路）")
	case "ERROR":
		return fmt.Errorf("视频不可用：%s（检查 id 是否正确、视频是否已删除或设为私享）", strings.TrimSpace(reason))
	case "UNPLAYABLE":
		return fmt.Errorf("视频无法播放：%s", strings.TrimSpace(reason))
	}
	if reason != "" {
		return fmt.Errorf("视频不可用（%s）：%s", status, strings.TrimSpace(reason))
	}
	return nil
}

// listTracks 走完 ①② 两步，拿到这个视频的全部字幕轨。
func (c *client) listTracks(ctx context.Context, videoID string) ([]track, error) {
	key, err := c.fetchAPIKey(ctx, videoID)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"context": innertubeContext, "videoId": videoID})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf(innertubeURL, key), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	body, status, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	if status == http.StatusTooManyRequests {
		return nil, errBlocked("player 接口返回 429")
	}
	var pr playerResp
	if err := json.Unmarshal([]byte(body), &pr); err != nil {
		return nil, fmt.Errorf("player 响应解析失败（YouTube 可能改了接口）: %w", err)
	}
	if err := checkPlayability(pr.PlayabilityStatus.Status, pr.PlayabilityStatus.Reason); err != nil {
		return nil, err
	}
	raw := pr.Captions.Renderer.CaptionTracks
	if len(raw) == 0 {
		return nil, fmt.Errorf("这个视频没有开放字幕（作者关掉了字幕，或还没生成）")
	}
	transLangs := make([]string, 0, len(pr.Captions.Renderer.TranslationLanguages))
	for _, l := range pr.Captions.Renderer.TranslationLanguages {
		transLangs = append(transLangs, l.LanguageCode)
	}

	out := make([]track, 0, len(raw))
	for _, ct := range raw {
		name := ct.Name.SimpleText
		if len(ct.Name.Runs) > 0 {
			name = ct.Name.Runs[0].Text
		}
		info := schema.TrackInfo{
			Language:       name,
			LanguageCode:   ct.LanguageCode,
			IsGenerated:    ct.Kind == "asr",
			IsTranslatable: ct.IsTranslatable,
		}
		if ct.IsTranslatable {
			info.TranslationLanguages = transLangs
		}
		// 去掉 &fmt=srv3：那个格式是给播放器用的富文本，我们要的是朴素 XML。
		out = append(out, track{Info: info, URL: strings.ReplaceAll(ct.BaseURL, "&fmt=srv3", "")})
	}
	return out, nil
}

// fetchTrack 取一条轨的内容（第 ③ 步）。tlang 非空则要 YouTube 顺便机翻。
func (c *client) fetchTrack(ctx context.Context, t track, tlang string, preserveFormatting bool) ([]schema.Snippet, error) {
	u := t.URL
	if tlang != "" {
		if !t.Info.IsTranslatable {
			return nil, fmt.Errorf("这条字幕（%s）不支持翻译，去掉「翻译成」或换一条轨", t.Info.LanguageCode)
		}
		if len(t.Info.TranslationLanguages) > 0 && !containsFold(t.Info.TranslationLanguages, tlang) {
			return nil, fmt.Errorf("不能翻译成 %q。可选：%s", tlang, strings.Join(t.Info.TranslationLanguages, "、"))
		}
		u += "&tlang=" + url.QueryEscape(tlang)
	}
	// PO Token：网页客户端路线的新拦路虎。识别出来明说，别让它退化成一句 XML 解析失败。
	if strings.Contains(u, "&exp=xpe") {
		return nil, fmt.Errorf("这个视频的字幕要求 PO Token，本插件（与所参考的实现一样）暂时取不到")
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	body, status, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	if status == http.StatusTooManyRequests {
		return nil, errBlocked("timedtext 接口返回 429")
	}
	return ParseTimedText(body, preserveFormatting)
}

func containsFold(xs []string, want string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}
