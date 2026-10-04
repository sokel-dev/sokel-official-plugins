package main

// Long-form articles (column posts): the `mp.xueqiu.com` API. A separate thing from short posts:
//
//	POST /xq/statuses/draft/save.json   save draft (title + text(HTML) + is_private)
//	POST /xq/photo/upload.json          upload image, returns {url, filename}
//	GET  /write/                        the writer page, carries window.UOM_CURRENTUSER → login state + uid
//
// **This API needs no session_token, and no risk-control param has been observed** — a
// much cleaner path than short posts, and long-form is exactly where research content
// belongs.
//
// The endpoint shapes are based on wechatsync/Wechatsync's xueqiu driver (a still
// maintained open-source implementation that takes the same approach we do: use an
// existing cookie to call the endpoints the web frontend already uses). **Only the
// endpoint knowledge was borrowed — no code was copied.**
//
// **The output is a draft, not a published post**: save.json lands in the draft box, and
// the final "publish" step is left to a human clicking it on the website. This matches
// the endpoint's own semantics, and also happens to be the safer shape from a compliance
// standpoint — automated drafting, human publishing.

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

// uploadArticleImage: the long-form image host endpoint, returning {url, filename} as two parts that must be joined manually.
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
	// It returns two parts: url is the directory (//xqimg.imedao.com/xxx), filename is the file name.
	src := strings.TrimRight(u, "/")
	if fn != "" {
		src += "/" + fn
	}
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	return src, nil
}

// —— login state ——

// currentUserRe: the writer page embeds window.UOM_CURRENTUSER = {...}, which carries
// both the login state and the uid — much more direct than guessing "is this logged in"
// from some other endpoint.
var currentUserRe = regexp.MustCompile(`(?s)UOM_CURRENTUSER\s*=\s*(\{.*?\})\s*[;<\n]`)

type currentUser struct {
	ID         any    `json:"id"`
	ScreenName string `json:"screen_name"`
}

// meFromWritePage: reads the writer page to determine login state. Returns uid and display name.
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
		// When not logged in, Xueqiu 302s the writer page to the login page, which doesn't have this script.
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

// —— text → long-form HTML ——

// toArticleHTML: the same conversion as short posts, but images use <p><img> instead of
// the short-post img-single-upload wrapper (the long-form editor expects a plain img).
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
