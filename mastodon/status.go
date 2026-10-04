package main

// Publishing: post, thread, delete, health check, media upload.

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
	// The character count is checked against the instance's limit, not hardcoded to 500 — asked
	// once and cached (see maxChars in client.go).
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
		// The idempotency key keeps a workflow retry from turning into two identical posts on the timeline.
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
			// CW and visibility are **inherited across the whole thread**: mixing public and
			// unlisted in one thread leaves readers seeing only a disjointed half.
			Spoiler: in.SpoilerText, Visible: in.Visibility, Language: in.Language,
		}
		if i == 0 {
			o.Images = in.Images
		}
		out, err := publish(ctx, o)
		if err != nil {
			// A mid-thread failure is **not rolled back**: the earlier posts are already on the
			// timeline, and deleting them would be a second act of damage.
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
		// Unavailable is a conclusion, not a fault: the platform uses it to write the credential's
		// status.
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

// —— Media ——

// uploadMedia uploads a single file and returns the media id.
//
// **Large files are processed asynchronously**: the v2 endpoint returns 200 synchronously for
// images, but 202 with an empty url for video/audio — posting immediately at that point gets
// rejected with a 422 ("media not finished processing"), so it has to be polled until done.
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
		return out.ID, nil // finished synchronously (this is the path images take)
	}
	// Asynchronous: poll until url has a value. Posting before it's done gets a 422, and the error
	// just says "media unavailable".
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

// —— Small helpers ——

// linkOf prefers url (a clickable address on this instance), falling back to uri (the federated identifier).
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
