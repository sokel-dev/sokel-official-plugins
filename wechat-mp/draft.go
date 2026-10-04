package main

// Four operations: create draft, publish draft, upload image, health check.

import (
	"fmt"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const (
	maxTitle  = 64
	maxDigest = 120
)

func opDraftAdd(ctx plugin.Ctx, in *MpDraftAddIn) (*MpDraftAddOut, error) {
	title := strings.TrimSpace(in.Title)
	content := strings.TrimSpace(in.Content)
	thumb := strings.TrimSpace(in.ThumbMediaID)
	switch {
	case title == "":
		return nil, fmt.Errorf("标题是空的")
	case content == "":
		return nil, fmt.Errorf("正文是空的")
	case thumb == "":
		// WeChat would otherwise return a cryptic 41005 here — better to say it plainly ourselves.
		return nil, fmt.Errorf("封面图是必填的：先用「上传图片」（用途选「封面」）拿一个 thumb_media_id")
	}
	if n := len([]rune(title)); n > maxTitle {
		return nil, fmt.Errorf("标题 %d 字，超过微信的 %d 上限", n, maxTitle)
	}
	digest := strings.TrimSpace(in.Digest)
	if n := len([]rune(digest)); n > maxDigest {
		digest = string([]rune(digest)[:maxDigest])
	}
	// Hotlinked images never display in the body (anti-hotlinking) — warn about this up front,
	// rather than discovering after publishing that the article has no images.
	if warn := foreignImages(content); warn != "" {
		return nil, fmt.Errorf("正文里有非微信域名的图片（%s）：微信会屏蔽它们，"+
			"先用「上传图片」（用途选「正文配图」）换成 mp.weixin.qq.com 的地址再拼进 HTML", warn)
	}

	article := map[string]any{
		"title":                 title,
		"content":               content,
		"thumb_media_id":        thumb,
		"need_open_comment":     boolInt(in.OpenComment),
		"only_fans_can_comment": boolInt(in.OnlyFansComment),
	}
	if a := strings.TrimSpace(in.Author); a != "" {
		article["author"] = a
	}
	if digest != "" {
		article["digest"] = digest
	}
	if u := strings.TrimSpace(in.ContentSourceURL); u != "" {
		article["content_source_url"] = u
	}

	var out struct {
		MediaID string `json:"media_id"`
	}
	if err := callJSON(ctx, "/cgi-bin/draft/add",
		map[string]any{"articles": []any{article}}, &out); err != nil {
		return nil, err
	}
	if out.MediaID == "" {
		return nil, fmt.Errorf("微信没返回草稿 media_id")
	}
	return &MpDraftAddOut{MediaID: out.MediaID}, nil
}

func opPublish(ctx plugin.Ctx, in *MpPublishIn) (*MpPublishOut, error) {
	mediaID := strings.TrimSpace(in.MediaID)
	if mediaID == "" {
		return nil, fmt.Errorf("草稿 media_id 是空的（先用「建草稿」拿一个）")
	}
	var sub struct {
		PublishID any `json:"publish_id"`
	}
	if err := callJSON(ctx, "/cgi-bin/freepublish/submit",
		map[string]any{"media_id": mediaID}, &sub); err != nil {
		return nil, err
	}
	pubID := asString(sub.PublishID)
	if pubID == "" {
		return nil, fmt.Errorf("微信没返回 publish_id")
	}
	if !in.Wait {
		return &MpPublishOut{PublishID: pubID, Status: "publishing"}, nil
	}
	return waitPublished(ctx, pubID)
}

// waitPublished polls for the publish result.
//
// **submit succeeding only means the job was submitted**: it can still get stuck on the originality
// declaration or platform review and ultimately fail. Without waiting, the workflow would treat an
// "actually never published" case as a success, and the article would simply never show up.
func waitPublished(ctx plugin.Ctx, pubID string) (*MpPublishOut, error) {
	deadline := time.Now().Add(4 * time.Minute)
	out := &MpPublishOut{PublishID: pubID, Status: "publishing"}
	for time.Now().Before(deadline) {
		var st struct {
			PublishStatus int    `json:"publish_status"`
			ArticleID     string `json:"article_id"`
			ArticleDetail struct {
				Item []struct {
					ArticleURL string `json:"article_url"`
				} `json:"item"`
			} `json:"article_detail"`
			FailIdx []int `json:"fail_idx"`
		}
		err := callJSON(ctx, "/cgi-bin/freepublish/get", map[string]any{"publish_id": pubID}, &st)
		if err != nil {
			return out, err
		}
		out.Status = publishStatus(st.PublishStatus)
		out.MsgDataID = st.ArticleID
		if len(st.ArticleDetail.Item) > 0 {
			out.ArticleURL = st.ArticleDetail.Item[0].ArticleURL
		}
		switch st.PublishStatus {
		case 0: // success
			return out, nil
		case 1: // publishing
		default:
			// The various failure cases: pass the status through as-is, and let a branch on the
			// canvas decide what to do with it.
			return out, fmt.Errorf("发布未成功（%s）：到公众号后台看这篇的详情", out.Status)
		}
		t := time.NewTimer(3 * time.Second)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return out, ctx.Err()
		}
	}
	return out, fmt.Errorf("等发布结果超时（publish_id=%s，当前 %s）：可稍后用这个 id 去后台查", pubID, out.Status)
}

// publishStatus turns WeChat's numeric status into a human-readable one.
func publishStatus(code int) string {
	switch code {
	case 0:
		return "success"
	case 1:
		return "publishing"
	case 2:
		return "original_failed" // failed originality review
	case 3:
		return "audit_failed" // failed general review
	case 4:
		return "all_failed"
	case 5:
		return "partial_failed"
	case 6:
		return "deleted" // deleted
	case 9:
		return "banned" // no publishing capability
	}
	return fmt.Sprintf("unknown_%d", code)
}

func opImageUpload(ctx plugin.Ctx, in *MpImageUploadIn) (*MpImageUploadOut, error) {
	if in.File == nil || in.File.ID == "" {
		return nil, fmt.Errorf("没有图片")
	}
	data, err := ctx.Fetch(in.File)
	if err != nil {
		return nil, fmt.Errorf("取文件失败: %w", err)
	}
	name := in.File.Name
	if name == "" {
		name = "image.jpg"
	}
	mime := in.File.Mime
	if mime == "" {
		mime = "image/jpeg"
	}
	if in.Purpose == "cover" {
		// Permanent material: a cover image needs a media_id.
		if len(data) > 10<<20 {
			return nil, fmt.Errorf("封面图 %.1fMB，超过微信的 10MB 上限", float64(len(data))/(1<<20))
		}
		var out struct {
			MediaID string `json:"media_id"`
			URL     string `json:"url"`
		}
		if err := callUpload(ctx, "/cgi-bin/material/add_material?type=image", name, mime, data, &out); err != nil {
			return nil, err
		}
		return &MpImageUploadOut{MediaID: out.MediaID, URL: out.URL}, nil
	}
	// Inline body images: only a URL comes back, doesn't count against the material library quota.
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("正文配图 %.1fMB，超过微信的 1MB 上限（封面图才是 10MB）", float64(len(data))/(1<<20))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := callUpload(ctx, "/cgi-bin/media/uploadimg", name, mime, data, &out); err != nil {
		return nil, err
	}
	if out.URL == "" {
		return nil, fmt.Errorf("微信没返回图片地址")
	}
	return &MpImageUploadOut{URL: out.URL}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	// Checking the draft count validates three things in one call: whether AppSecret is correct,
	// whether the IP is in the allowlist, and whether this account has publishing permission. Just
	// refreshing the token alone wouldn't surface the allowlist or permission issue until the actual
	// publish attempt.
	var out struct {
		TotalCount int `json:"total_count"`
	}
	if err := callGet(ctx, "/cgi-bin/draft/count", &out); err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, DraftCount: out.TotalCount,
		Message: fmt.Sprintf("正常，草稿箱 %d 篇", out.TotalCount)}, nil
}

// —— Small helpers ——

// foreignImages checks whether the body has any image from a non-WeChat domain. Returns the first
// one found; an empty string means none.
func foreignImages(html string) string {
	rest := html
	for {
		i := strings.Index(rest, "<img")
		if i < 0 {
			return ""
		}
		rest = rest[i+4:]
		j := strings.Index(rest, ">")
		if j < 0 {
			return ""
		}
		tag := rest[:j]
		src := attr(tag, "src")
		if src != "" && !strings.Contains(src, "qpic.cn") && !strings.Contains(src, "weixin.qq.com") &&
			!strings.HasPrefix(src, "data:") {
			return src
		}
		rest = rest[j:]
	}
}

func attr(tag, name string) string {
	i := strings.Index(tag, name+"=")
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(tag[i+len(name)+1:])
	if rest == "" {
		return ""
	}
	quote := rest[0]
	if quote != '"' && quote != '\'' {
		if j := strings.IndexAny(rest, " \t"); j > 0 {
			return rest[:j]
		}
		return rest
	}
	if j := strings.IndexByte(rest[1:], quote); j >= 0 {
		return rest[1 : 1+j]
	}
	return ""
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// asString handles that WeChat sometimes returns publish_id as a number and sometimes as a string.
func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}
