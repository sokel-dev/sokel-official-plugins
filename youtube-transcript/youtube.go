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

// Fetching transcripts uses the same undocumented API **the YouTube web client itself relies on**,
// unrelated to Data API v3:
//
//	① GET  watch?v=<id>                      → extract INNERTUBE_API_KEY (and detect an anti-bot page)
//	② POST youtubei/v1/player?key=<key>      → get the list of caption tracks (captionTracks)
//	③ GET  <track.baseUrl>                   → timedtext XML, the actual transcript content
//
// Why not Data API v3: that path needs a key, has a quota, and captions.download **only works for your
// own channel's videos** — any other video gets a flat 403, meaning it simply can't do this job at all.
//
// The client identifies itself as ANDROID (matching the reference project): the web client has started
// requiring a PO Token in recent times, while the Android client path still doesn't. This is something
// **YouTube can change at any time**, which is why the constants are kept isolated here — there's only
// one place to update.

const (
	watchURL      = "https://www.youtube.com/watch?v=%s"
	innertubeURL  = "https://www.youtube.com/youtubei/v1/player?key=%s"
	defaultUA     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	healthVideoID = "dQw4w9WgXcQ" // public video used for health checks: up for over a decade, full transcripts
)

// innertubeContext is the spoofed client identity. The version number does go stale; the symptom is the
// player endpoint starting to demand a PO Token.
var innertubeContext = map[string]any{
	"client": map[string]any{"clientName": "ANDROID", "clientVersion": "20.10.38"},
}

var (
	apiKeyRe  = regexp.MustCompile(`"INNERTUBE_API_KEY":\s*"([A-Za-z0-9_-]+)"`)
	consentRe = regexp.MustCompile(`name="v" value="(.*?)"`)
)

// client is the HTTP facade for one call (proxy and UA come from the credential).
type client struct {
	http *http.Client
	ua   string
}

// newClient: the proxy can come from two sources, and **both must actually work**.
//
//	① the credential's "outbound proxy" — the production answer (a residential proxy to get around
//	   YouTube blocking datacenter IPs);
//	② the process's HTTP(S)_PROXY environment variables — the local-dev answer (direct connections from
//	   mainland China can't reach youtube.com).
//
// Falling back to ProxyFromEnvironment is necessary: a hand-built http.Transport's Proxy field defaults
// to nil, which isn't "use the default", it's **explicitly disabling** the proxy (http.DefaultTransport
// is the one that carries ProxyFromEnvironment). Without this, exporting HTTPS_PROXY locally still
// wouldn't connect, and the symptom would look like an ordinary network failure.
func newClient(proxy, ua string) (*client, error) {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if proxy = strings.TrimSpace(proxy); proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("代理地址不合法 %q：应形如 http://user:pass@host:port 或 socks5://host:1080", proxy)
		}
		// credential wins over env vars: production shouldn't be silently rerouted by the host environment
		tr.Proxy = http.ProxyURL(u)
	}
	if ua = strings.TrimSpace(ua); ua == "" {
		ua = defaultUA
	}
	return &client{http: &http.Client{Transport: tr, Timeout: 30 * time.Second}, ua: ua}, nil
}

func (c *client) do(ctx context.Context, req *http.Request) (string, int, error) {
	req.Header.Set("User-Agent", c.ua)
	// Accept-Language determines the language of the **track display names** YouTube returns (it doesn't
	// affect which tracks exist). Pinning it to en-US keeps name.runs[0].text stable; otherwise the same
	// video would show different display names depending on the egress IP, and anything downstream that
	// matches on the name would drift.
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

// errBlocked represents an anti-bot block. It's kept as its own category because **the way you handle it
// is completely different from any other error** — it's not a wrong parameter, it's this machine's egress
// IP being rejected by YouTube, and the only fix is configuring a proxy.
func errBlocked(detail string) error {
	return fmt.Errorf("被 YouTube 风控挡下（%s）。云主机/机房 IP 基本都会撞到这个；"+
		"解法是在本插件的凭证里配一个**住宅代理**出站。本插件不需要 YouTube 账号，配了代理即可", detail)
}

// fetchAPIKey fetches the watch page and extracts the InnerTube key, also detecting a consent page or an
// anti-bot page along the way.
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
	// EU consent page: needs a CONSENT cookie and a retry.
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

// playerResp is the subset of the player endpoint's response that we actually need.
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
				Kind           string `json:"kind"` // "asr" = auto-generated
				IsTranslatable bool   `json:"isTranslatable"`
			} `json:"captionTracks"`
			TranslationLanguages []struct {
				LanguageCode string `json:"languageCode"`
			} `json:"translationLanguages"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
}

// checkPlayability translates YouTube's status codes into plain language.
//
// This step accounts for most of this plugin's usability: in all of these cases **the plugin itself
// isn't broken**, but a bare "failed to fetch transcript" would leave the user with no way to figure out
// what to change.
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

// listTracks walks through steps ① and ② to get this video's complete list of caption tracks.
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
		// Strip &fmt=srv3: that format is rich text meant for the player, we want plain XML.
		out = append(out, track{Info: info, URL: strings.ReplaceAll(ct.BaseURL, "&fmt=srv3", "")})
	}
	return out, nil
}

// fetchTrack fetches one track's content (step ③). A non-empty tlang also asks YouTube to machine
// translate it.
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
	// PO Token: the new roadblock on the web-client path. Detect it and say so explicitly, rather than
	// letting it degrade into a bare "XML parse failed".
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
