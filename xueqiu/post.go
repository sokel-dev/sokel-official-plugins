package main

// 两个操作：发帖、检查凭证。
//
// 发帖是三段：图片先传到雪球图床 → 正文转成雪球那套 HTML（图片按固定形状嵌进去）→ 提交表单。

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
		"ai_disclose":   {intStr(in.AiDisclose)}, // 合规字段：LLM 参与过就该是 1
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
	// 读**写作页**判断登录态：它里面塞了 window.UOM_CURRENTUSER，登录态与 uid 都在那儿。
	// 比拿别的接口猜「有没有登录」直接得多，而且同样无副作用——
	// 别拿发帖来检查凭证，那会在时间线上留下垃圾。
	uid, name, err := meFromWritePage(ctx)
	if err != nil {
		// 不可用是结论不是故障：平台拿 ok=false 去写凭证状态、触发「凭证失效」告警。
		return &HealthCheckOut{OK: false, UID: uidFromCookie(credOf(ctx).Cookie), Message: err.Error()}, nil
	}
	msg := "Cookie 有效（uid " + uid
	if name != "" {
		msg += " / " + name
	}
	return &HealthCheckOut{OK: true, UID: uid, Name: name, Message: msg + "）"}, nil
}

// uploadPhoto：传一张图，返回可嵌进正文的图床地址（//xqimg.imedao.com/xxx.png）。
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
	// 应答形状没有文档：**按内容找**（含图床域名的那个字符串），而不是钉某个键名。
	src := findString(raw, func(s string) bool { return strings.Contains(s, "xqimg.imedao.com") })
	if src == "" {
		return "", fmt.Errorf("传上去了但没认出图片地址——雪球可能改了应答形状，请把应答贴给开发者")
	}
	return src, nil
}

// —— 正文 → 雪球 HTML ——

var (
	boldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	// 判「已经是 HTML」必须要求形如 <p …> 的**完整标签**。
	// 只看 `<p` 的话，纯文本里一句「A<B」也会被当成 HTML 放行 → 整段不转义，
	// 用户正文里的 < 就能把结构冲掉（测试 TestTextIsEscaped 钉的就是这个）。
	htmlRe = regexp.MustCompile(`(?is)<(p|div|br|img|b|strong)(\s[^<>]*)?/?>`)
	linkRe = regexp.MustCompile(`https?://[^\s<>"'）)】\]}，。；！？]+`)
)

// toXueqiuHTML：把纯文本/简单 markdown 转成雪球发帖要的 HTML。
//
// 三条：
//   - **已经是 HTML 就原样透传**（用户自己拼好的排版不该被我们二次加工）；
//   - 纯文本必须**先转义再拼**——正文里一个 < 就能把整段结构冲掉；
//   - 图片是固定形状：<div class="img-single-upload"><img src="…!custom.jpg" class="ke_img">，
//     少了那两个 class，雪球的编辑器认不出它是图片。
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
			// 链接转义后 & 会变成 &amp;，这里只把整段链接包成 <a>，不动内部字符
			esc = linkRe.ReplaceAllStringFunc(esc, func(u string) string {
				return `<a href="` + u + `" target="_blank">` + u + `</a>`
			})
			b.WriteString("<p>" + esc + "</p>")
		}
	}
	for _, src := range imgs {
		if !strings.Contains(src, "!") {
			src += "!custom.jpg" // 雪球用后缀选缩放规格，不带的话编辑器里显示不出来
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
