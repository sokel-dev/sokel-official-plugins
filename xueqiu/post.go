package main

// Two operations: post, and check credential.
//
// Posting is three steps: upload images to Xueqiu's image host first → convert the text
// to Xueqiu's HTML (images embedded in a fixed shape) → submit the form.

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opPostCreate(ctx plugin.Ctx, in *XqPostCreateIn) (*XqPostCreateOut, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" && len(in.Images) == 0 {
		return nil, fmt.Errorf("正文与图片至少要有一样")
	}
	imgs := make([]string, 0, len(in.Images))
	for i, f := range in.Images {
		if f == nil || f.ID == "" {
			continue
		}
		src, err := uploadPhoto(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("传第 %d 张图失败: %w", i+1, err)
		}
		imgs = append(imgs, src)
	}
	token, err := sessionToken(ctx)
	if err != nil {
		return nil, err
	}

	form := url.Values{
		"status":        {toXueqiuHTML(text, imgs)},
		"allow_reward":  {boolStr(in.AllowReward)},
		"ai_disclose":   {intStr(in.AiDisclose)}, // compliance field: should be 1 whenever an LLM was involved
		"post_position": {"pc_home_post"},
		"post_source":   {""},
		"session_token": {token},
	}
	var raw any
	if err := do(ctx, reqOpts{method: http.MethodPost, path: "/statuses/update.json", form: form}, &raw); err != nil {
		return nil, err
	}
	id := findID(raw, "id", "status_id", "statusId")
	if id == "" {
		return nil, fmt.Errorf("发出去了但没认出帖子 id——雪球可能改了应答形状，请把应答贴给开发者")
	}
	return &XqPostCreateOut{ID: id, URL: postURL(credOf(ctx).Cookie, id)}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	// Determines login state by reading the **writer page**: it embeds
	// window.UOM_CURRENTUSER, which carries both login state and uid — much more direct
	// than guessing "is this logged in" from some other endpoint, and equally
	// side-effect-free. Posting a status to check a credential would leave junk on the timeline.
	uid, name, err := meFromWritePage(ctx)
	if err != nil {
		// Unavailable is a conclusion, not a failure: the platform uses ok=false to write the credential's status and trigger the "credential invalid" alert.
		return &HealthCheckOut{OK: false, UID: uidFromCookie(credOf(ctx).Cookie), Message: err.Error()}, nil
	}
	msg := "Cookie 有效（uid " + uid
	if name != "" {
		msg += " / " + name
	}
	return &HealthCheckOut{OK: true, UID: uid, Name: name, Message: msg + "）"}, nil
}

// uploadPhoto: uploads one image, returning an image-host address that can be embedded in the text (//xqimg.imedao.com/xxx.png).
func uploadPhoto(ctx plugin.Ctx, f *plugin.File) (string, error) {
	data, err := ctx.Fetch(f)
	if err != nil {
		return "", fmt.Errorf("取文件失败: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("文件是空的")
	}
	name := f.Name
	if name == "" {
		name = "image.png"
	}
	mime := f.Mime
	if mime == "" {
		mime = "image/png"
	}
	mp, err := newMultipart("file", name, mime, data)
	if err != nil {
		return "", err
	}
	var raw any
	if err := do(ctx, reqOpts{method: http.MethodPost, path: "/photo/upload.json", multi: mp}, &raw); err != nil {
		return "", err
	}
	// The response shape is undocumented: **search by content** (the string containing the image-host domain) rather than pin down a key name.
	src := findString(raw, func(s string) bool { return strings.Contains(s, "xqimg.imedao.com") })
	if src == "" {
		return "", fmt.Errorf("传上去了但没认出图片地址——雪球可能改了应答形状，请把应答贴给开发者")
	}
	return src, nil
}

// —— text → Xueqiu HTML ——

var (
	boldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	// Deciding "this is already HTML" must require a **complete tag** shaped like <p …>.
	// Looking only for `<p` would let plain text containing "A<B" pass as HTML too →
	// the whole block skips escaping, and a stray < in the user's text can break the
	// structure (this is exactly what TestTextIsEscaped pins down).
	htmlRe = regexp.MustCompile(`(?is)<(p|div|br|img|b|strong)(\s[^<>]*)?/?>`)
	linkRe = regexp.MustCompile(`https?://[^\s<>"'）)】\]}，。；！？]+`)
)

// toXueqiuHTML: converts plain text / simple markdown into the HTML Xueqiu's posting endpoint expects.
//
// Three rules:
//   - **Already-HTML text is passed through as-is** (a user's own hand-built markup
//     shouldn't be reprocessed by us);
//   - Plain text must be **escaped before being assembled** — a single < in the text can
//     break the whole structure;
//   - Images have a fixed shape: <div class="img-single-upload"><img src="…!custom.jpg"
//     class="ke_img">; without those two classes, Xueqiu's editor doesn't recognize it as an image.
func toXueqiuHTML(text string, imgs []string) string {
	var b strings.Builder
	if htmlRe.MatchString(text) {
		b.WriteString(text)
	} else {
		for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			para = strings.TrimSpace(para)
			if para == "" {
				continue
			}
			esc := html.EscapeString(para)
			esc = boldRe.ReplaceAllString(esc, "<b>$1</b>")
			// After escaping, & in a link becomes &amp;; this only wraps the whole link in <a>, without touching its inner characters
			esc = linkRe.ReplaceAllStringFunc(esc, func(u string) string {
				return `<a href="` + u + `" target="_blank">` + u + `</a>`
			})
			b.WriteString("<p>" + esc + "</p>")
		}
	}
	for _, src := range imgs {
		if !strings.Contains(src, "!") {
			src += "!custom.jpg" // Xueqiu picks the scaled variant via this suffix; without it the editor won't display the image
		}
		b.WriteString(`<div class="img-single-upload"><img src="` + src + `" class="ke_img"></div>`)
	}
	return b.String()
}

func postURL(cookie, id string) string {
	uid := uidFromCookie(cookie)
	if uid == "" || id == "" {
		return ""
	}
	return "https://xueqiu.com/" + uid + "/" + id
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func intStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
