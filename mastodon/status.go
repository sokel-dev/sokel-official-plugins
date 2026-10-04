package main

// 发布：发嘟、嘟串、删嘟、健康检查、媒体上传。

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type statusOut struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	URI        string `json:"uri"`
	Visibility string `json:"visibility"`
}

type publishOpts struct {
	Text      string
	ReplyTo   string
	Spoiler   string
	Visible   string
	Sensitive bool
	Language  string
	Images    []*plugin.File
	ImgAlts   []string
	Poll      []string
	PollHours int
}

func publish(ctx plugin.Ctx, o publishOpts) (statusOut, error) {
	text := strings.TrimSpace(o.Text)
	poll := nonEmpty(o.Poll)
	if text == "" && len(o.Images) == 0 {
		return statusOut{}, fmt.Errorf("正文与媒体至少要有一样")
	}
	if len(poll) > 0 && len(o.Images) > 0 {
		return statusOut{}, fmt.Errorf("投票和媒体不能同时带（Mastodon 的限制）")
	}
	// 字数按实例的上限算，不写死 500——问一次缓存住（见 client.go 的 maxChars）。
	if lim := maxChars(ctx); utf8.RuneCountInString(text) > lim {
		return statusOut{}, fmt.Errorf("正文 %d 个字，超过本实例的 %d 上限（发长内容请用「发嘟文串」）",
			utf8.RuneCountInString(text), lim)
	}

	form := url.Values{}
	if text != "" {
		form.Set("status", text)
	}
	if id := statusID(o.ReplyTo); id != "" {
		form.Set("in_reply_to_id", id)
	}
	if s := strings.TrimSpace(o.Spoiler); s != "" {
		form.Set("spoiler_text", s)
	}
	if v := strings.TrimSpace(o.Visible); v != "" {
		form.Set("visibility", v)
	}
	if o.Sensitive {
		form.Set("sensitive", "true")
	}
	if l := strings.TrimSpace(o.Language); l != "" {
		form.Set("language", l)
	}
	for _, f := range o.Images {
		id, err := uploadMedia(ctx, f, altFor(o.ImgAlts, o.Images, f))
		if err != nil {
			return statusOut{}, err
		}
		form.Add("media_ids[]", id)
	}
	if len(poll) > 0 {
		if len(poll) < 2 || len(poll) > 4 {
			return statusOut{}, fmt.Errorf("投票要 2-4 个选项，给了 %d 个", len(poll))
		}
		for _, p := range poll {
			form.Add("poll[options][]", p)
		}
		h := o.PollHours
		if h <= 0 {
			h = 24
		}
		form.Set("poll[expires_in]", strconv.Itoa(h*3600))
	}

	var out statusOut
	err := call(ctx, reqOpts{
		method: http.MethodPost, path: "/api/v1/statuses", form: form,
		// 幂等键让「工作流重试」不至于变成时间线上两条一样的嘟文。
		idemp: idempotencyKey(text, o.ReplyTo, o.Visible, o.Spoiler),
	}, &out)
	if err != nil {
		return statusOut{}, err
	}
	return out, nil
}

func opStatusCreate(ctx plugin.Ctx, in *MastoStatusCreateIn) (*MastoStatusCreateOut, error) {
	out, err := publish(ctx, publishOpts{
		Text: in.Text, ReplyTo: in.ReplyToID, Spoiler: in.SpoilerText, Visible: in.Visibility,
		Sensitive: in.Sensitive, Language: in.Language, Images: in.Images, ImgAlts: in.ImageAlts,
		Poll: in.PollOptions, PollHours: in.PollHours,
	})
	if err != nil {
		return nil, err
	}
	return &MastoStatusCreateOut{ID: out.ID, URL: linkOf(out), Visibility: out.Visibility}, nil
}

func opStatusThread(ctx plugin.Ctx, in *MastoStatusThreadIn) (*MastoStatusThreadOut, error) {
	texts := nonEmpty(in.Texts)
	if len(texts) == 0 {
		return nil, fmt.Errorf("一条正文都没有")
	}
	gap := time.Duration(in.IntervalMs) * time.Millisecond
	if in.IntervalMs <= 0 {
		gap = 500 * time.Millisecond
	}
	ids := make([]string, 0, len(texts))
	var rootURL string
	prev := statusID(in.ReplyToID)
	for i, t := range texts {
		o := publishOpts{
			Text: t, ReplyTo: prev,
			// CW 与可见性**整串继承**：一串里混进公开与不列出，读者只能看到断断续续的半串。
			Spoiler: in.SpoilerText, Visible: in.Visibility, Language: in.Language,
		}
		if i == 0 {
			o.Images = in.Images
		}
		out, err := publish(ctx, o)
		if err != nil {
			// 中途失败**不回滚**：前面几条已经在时间线上了，删掉是二次破坏。
			return nil, fmt.Errorf("嘟文串发到第 %d 条失败（前 %d 条已发出：%s）: %w",
				i+1, len(ids), strings.Join(ids, ","), err)
		}
		ids = append(ids, out.ID)
		if i == 0 {
			rootURL = linkOf(out)
		}
		prev = out.ID
		if i < len(texts)-1 && gap > 0 {
			t := time.NewTimer(gap)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("嘟文串被中断（已发出 %d 条：%s）", len(ids), strings.Join(ids, ","))
			}
		}
	}
	return &MastoStatusThreadOut{IDs: ids, RootID: ids[0], RootURL: rootURL, Count: len(ids)}, nil
}

func opStatusDelete(ctx plugin.Ctx, in *MastoStatusDeleteIn) (*MastoStatusDeleteOut, error) {
	id := statusID(in.StatusID)
	if id == "" {
		return nil, fmt.Errorf("嘟文 id 是空的")
	}
	if err := call(ctx, reqOpts{method: http.MethodDelete, path: "/api/v1/statuses/" + id}, nil); err != nil {
		return nil, err
	}
	return &MastoStatusDeleteOut{Deleted: true}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var acct struct {
		Acct     string `json:"acct"`
		Username string `json:"username"`
	}
	if err := call(ctx, reqOpts{method: http.MethodGet,
		path: "/api/v1/accounts/verify_credentials"}, &acct); err != nil {
		// 不可用是结论不是故障：平台拿它写凭证状态。
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	who := acct.Acct
	if who == "" {
		who = acct.Username
	}
	if who == "" {
		return &HealthCheckOut{OK: false, Message: "实例没返回账号信息，令牌可能无效"}, nil
	}
	return &HealthCheckOut{OK: true, Account: who, Message: "@" + who}, nil
}

// —— 媒体 ——

// uploadMedia：传一个文件拿 media id。
//
// **大文件是异步处理的**：v2 接口对图片同步返回 200，对视频/音频返回 202 且 url 为空——
// 这时立刻拿去发嘟会被 422 拒（「媒体还没处理完」），所以要轮询到处理完。
func uploadMedia(ctx plugin.Ctx, f *plugin.File, alt string) (string, error) {
	if f == nil || f.ID == "" {
		return "", fmt.Errorf("媒体文件是空的")
	}
	data, err := ctx.Fetch(f)
	if err != nil {
		return "", fmt.Errorf("取文件失败: %w", err)
	}
	extra := map[string]string{}
	if alt != "" {
		extra["description"] = alt
	}
	name := f.Name
	if name == "" {
		name = "media"
	}
	mime := f.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	mp, err := newMultipart("file", name, mime, data, extra)
	if err != nil {
		return "", err
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := call(ctx, reqOpts{method: http.MethodPost, path: "/api/v2/media", multi: mp}, &out); err != nil {
		return "", fmt.Errorf("传媒体失败: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("实例没返回 media id")
	}
	if out.URL != "" {
		return out.ID, nil // 同步处理完了（图片走这条）
	}
	// 异步：轮询到 url 有值为止。没处理完就发嘟会被 422 拒，而错误里只说「媒体不可用」。
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		t := time.NewTimer(2 * time.Second)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return "", ctx.Err()
		}
		var st struct {
			URL string `json:"url"`
		}
		if err := call(ctx, reqOpts{method: http.MethodGet, path: "/api/v1/media/" + out.ID}, &st); err == nil &&
			st.URL != "" {
			return out.ID, nil
		}
	}
	return "", fmt.Errorf("媒体处理超时（id=%s）：大视频可稍后重试", out.ID)
}

// —— 小工具 ——

// linkOf：优先用 url（本站可点的地址），退回 uri（联邦标识）。
func linkOf(s statusOut) string {
	if s.URL != "" {
		return s.URL
	}
	return s.URI
}

func altFor(alts []string, files []*plugin.File, cur *plugin.File) string {
	for i, f := range files {
		if f == cur && i < len(alts) {
			return alts[i]
		}
	}
	return ""
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
