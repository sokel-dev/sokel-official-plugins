package main

// 长文（专栏）：`mp.xueqiu.com` 那套接口。与短帖是两回事：
//
//	POST /xq/statuses/draft/save.json   存草稿（title + text(HTML) + is_private）
//	POST /xq/photo/upload.json          传图，回 {url, filename}
//	GET  /write/                        写作页，里面有 window.UOM_CURRENTUSER → 登录态 + uid
//
// **这套不要 session_token，也没见风控参数**——比短帖那条路干净得多，
// 而长文正是投研内容该去的地方。
//
// 接口形状参照了 wechatsync/Wechatsync 的 xueqiu driver（一个仍在维护的开源实现，
// 做法与我们一致：用已有 cookie 调网页端自己在用的接口）。**只借鉴接口知识，没有抄代码。**
//
// **产出是草稿不是已发布**：save.json 落的是草稿箱，最后一步「发布」留给人在网页上点。
// 这既是接口本身的语义，也正好是合规上更稳的形态——自动写、人工发。

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opArticleDraft(ctx plugin.Ctx, in *XqArticleDraftIn) (*XqArticleDraftOut, error) {
	title := strings.TrimSpace(in.Title)
	body := strings.TrimSpace(in.Content)
	if title == "" {
		return nil, fmt.Errorf("长文必须有标题")
	}
	if body == "" {
		return nil, fmt.Errorf("正文是空的")
	}
	imgs := make([]string, 0, len(in.Images))
	for i, f := range in.Images {
		if f == nil || f.ID == "" {
			continue
		}
		src, err := uploadArticleImage(ctx, f)
		if err != nil {
			return nil, fmt.Errorf("传第 %d 张图失败: %w", i+1, err)
		}
		imgs = append(imgs, src)
	}

	form := url.Values{
		"title":      {title},
		"text":       {toArticleHTML(body, imgs)},
		"cover_pic":  {strings.TrimSpace(in.CoverPic)},
		"flags":      {"false"},
		"is_private": {boolStr(in.Private)},
	}
	var raw any
	if err := do(ctx, reqOpts{method: http.MethodPost, host: mpBase,
		path: "/xq/statuses/draft/save.json", form: form, referer: mpBase + "/write/"}, &raw); err != nil {
		return nil, err
	}
	id := findID(raw, "id", "draft_id", "draftId")
	out := &XqArticleDraftOut{ID: id, EditURL: mpBase + "/write/"}
	if id != "" {
		out.EditURL = mpBase + "/write/?draft_id=" + id
	}
	return out, nil
}

// uploadArticleImage：长文的图床接口，回 {url, filename} 两段，要自己拼。
func uploadArticleImage(ctx plugin.Ctx, f *plugin.File) (string, error) {
	data, err := ctx.Fetch(f)
	if err != nil {
		return "", fmt.Errorf("取文件失败: %w", err)
	}
	name, mime := f.Name, f.Mime
	if name == "" {
		name = "image.png"
	}
	if mime == "" {
		mime = "image/png"
	}
	mp, err := newMultipart("file", name, mime, data)
	if err != nil {
		return "", err
	}
	var resp struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Data     struct {
			URL      string `json:"url"`
			Filename string `json:"filename"`
		} `json:"data"`
	}
	if err := do(ctx, reqOpts{method: http.MethodPost, host: mpBase,
		path: "/xq/photo/upload.json", multi: mp, referer: mpBase + "/write/"}, &resp); err != nil {
		return "", err
	}
	u, fn := firstNonEmpty(resp.URL, resp.Data.URL), firstNonEmpty(resp.Filename, resp.Data.Filename)
	if u == "" {
		return "", fmt.Errorf("传上去了但没认出图片地址——雪球可能改了应答形状，请把应答贴给开发者")
	}
	// 它回的是两段：url 是目录（//xqimg.imedao.com/xxx），filename 是文件名。
	src := strings.TrimRight(u, "/")
	if fn != "" {
		src += "/" + fn
	}
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	return src, nil
}

// —— 登录态 ——

// currentUserRe：写作页里塞了 window.UOM_CURRENTUSER = {...}，登录态与 uid 都在里面。
// 比拿别的接口猜「有没有登录」直接得多。
var currentUserRe = regexp.MustCompile(`(?s)UOM_CURRENTUSER\s*=\s*(\{.*?\})\s*[;<\n]`)

type currentUser struct {
	ID         any    `json:"id"`
	ScreenName string `json:"screen_name"`
}

// meFromWritePage：读写作页判断登录态。返回 uid 与昵称。
func meFromWritePage(ctx plugin.Ctx) (string, string, error) {
	cred := credOf(ctx)
	ck, err := cookieOf(cred)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mpBase+"/write/", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Cookie", ck)
	req.Header.Set("User-Agent", uaOf(cred))
	req.Header.Set("Referer", mpBase+"/")
	resp, err := clientFor(cred.Proxy).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("连接雪球写作页失败: %w", err)
	}
	defer resp.Body.Close()
	page, _ := readAllLimited(resp.Body)
	m := currentUserRe.FindSubmatch(page)
	if len(m) != 2 {
		// 没登录时雪球会把写作页 302 到登录页，那上面没有这段脚本。
		return "", "", fmt.Errorf("写作页上没有登录信息：Cookie 多半过期了——" +
			"重新登录一次并更新凭证")
	}
	var cu struct {
		CurrentUser currentUser `json:"currentUser"`
	}
	var flat currentUser
	if json.Unmarshal(m[1], &cu) == nil && idStr(cu.CurrentUser.ID) != "" {
		return idStr(cu.CurrentUser.ID), cu.CurrentUser.ScreenName, nil
	}
	if json.Unmarshal(m[1], &flat) == nil && idStr(flat.ID) != "" {
		return idStr(flat.ID), flat.ScreenName, nil
	}
	return "", "", fmt.Errorf("写作页上的登录信息解不开：Cookie 可能已失效")
}

func idStr(v any) string {
	switch t := v.(type) {
	case string:
		if t != "" && t != "0" {
			return t
		}
	case float64:
		if t > 0 {
			return fmt.Sprintf("%.0f", t)
		}
	}
	return ""
}

// —— 正文 → 长文 HTML ——

// toArticleHTML：与短帖同一套转换，但图片用 <p><img> 而不是短帖那个
// img-single-upload 的壳（长文编辑器认的是普通 img）。
func toArticleHTML(text string, imgs []string) string {
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
			esc = linkRe.ReplaceAllStringFunc(esc, func(u string) string {
				return `<a href="` + u + `" target="_blank">` + u + `</a>`
			})
			b.WriteString("<p>" + esc + "</p>")
		}
	}
	for _, src := range imgs {
		b.WriteString(`<p><img src="` + src + `"></p>`)
	}
	return b.String()
}
