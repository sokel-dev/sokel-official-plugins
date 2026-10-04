package main

// 假雪球。这个插件打的是**没有文档的私有接口**，所以测试的重点与别家不同：
//   - 请求得长得跟抓包里一模一样（表单字段、四个头），差一个就被 WAF 拦；
//   - 应答形状是猜的 → 宽松提取要能扛住键名变化；
//   - 被风控拦下时回的是**一整页 HTML**，不能报成「解析失败」；
//   - 正文转 HTML 时必须先转义（一个 < 就能冲掉整段结构）。

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const testCookie = "cookiesu=1; device_id=d1; xq_a_token=aaa; xqat=aaa; " +
	// 载荷是 {"uid":6212100204}
	"xq_id_token=h.eyJ1aWQiOjYyMTIxMDAyMDR9.s; u=6212100204"

type fakeCtx struct {
	context.Context
	cred map[string]string
	file []byte
}

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (c *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_1"}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) { panic("不用") }
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error)                           { return c.file, nil }

type capture struct {
	paths   []string
	forms   []url.Values
	headers []http.Header
	bodies  []string
}

func (c *capture) form(suffix string) url.Values {
	for i, p := range c.paths {
		if strings.HasSuffix(p, suffix) {
			return c.forms[i]
		}
	}
	return nil
}

func ctxTo(t *testing.T, cap *capture, extra map[string]string,
	routes map[string]func(http.ResponseWriter, *http.Request)) *fakeCtx {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 先读 body 会把它抽干，ParseForm 就啥也解不出来——读完得塞回去。
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		_ = r.ParseForm()
		cap.paths = append(cap.paths, r.URL.Path)
		cap.forms = append(cap.forms, r.PostForm)
		cap.headers = append(cap.headers, r.Header)
		cap.bodies = append(cap.bodies, string(body))
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
			io.WriteString(w, `<html><script>var SNB={session_token:"tok_from_page"};</script></html>`)
		case "/write/":
			io.WriteString(w, `<html><script>window.UOM_CURRENTUSER = {"currentUser":{"id":6212100204,"screen_name":"投研君"}};</script></html>`)
		case "/xq/statuses/draft/save.json":
			io.WriteString(w, `{"id":88881111,"title":"t"}`)
		case "/xq/photo/upload.json":
			io.WriteString(w, `{"url":"//xqimg.imedao.com/dir","filename":"pic.png"}`)
		case "/statuses/update.json":
			io.WriteString(w, `{"id":123456789,"text":"x"}`)
		case "/photo/upload.json":
			io.WriteString(w, `{"success":true,"files":[{"path":"//xqimg.imedao.com/abc.png"}]}`)
		case "/etc/private_fund/state.json":
			io.WriteString(w, `{"state":0}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	base = srv.URL
	mpBase = srv.URL // 长文那套在 mp 子站上，测试里指同一个假服务器
	tokMu.Lock()
	tokCache = map[string]string{}
	tokMu.Unlock()
	cred := map[string]string{"cookie": testCookie}
	for k, v := range extra {
		cred[k] = v
	}
	return &fakeCtx{Context: context.Background(), cred: cred, file: []byte("PNG")}
}

// 发帖请求得跟抓包里一样：表单字段齐、四个头齐。
func TestPostRequestShape(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	out, err := opPostCreate(ctx, &XqPostCreateIn{Text: "看好这个方向", AiDisclose: true})
	if err != nil {
		t.Fatal(err)
	}
	f := cap.form("/statuses/update.json")
	if f == nil {
		t.Fatal("没发出发帖请求")
	}
	for k, want := range map[string]string{
		"allow_reward":  "false",
		"ai_disclose":   "1", // 打开了就必须是 1——这是合规字段
		"post_position": "pc_home_post",
		"session_token": "tok_from_page",
	} {
		if f.Get(k) != want {
			t.Errorf("%s = %q，想要 %q", k, f.Get(k), want)
		}
	}
	if !strings.Contains(f.Get("status"), "<p>看好这个方向</p>") {
		t.Errorf("正文没转成 HTML: %q", f.Get("status"))
	}
	var h http.Header
	for i, p := range cap.paths {
		if p == "/statuses/update.json" {
			h = cap.headers[i]
		}
	}
	for _, k := range []string{"Cookie", "User-Agent", "X-Requested-With", "Referer", "Origin"} {
		if h.Get(k) == "" {
			t.Errorf("少了 %s 头——雪球的 WAF 认这几个", k)
		}
	}
	if out.ID != "123456789" {
		t.Errorf("帖子 id = %q", out.ID)
	}
	// uid 从 xq_id_token 的 JWT 载荷里解出来，用来拼链接
	if out.URL != "https://xueqiu.com/6212100204/123456789" {
		t.Errorf("链接拼错了: %q", out.URL)
	}
}

// 图片：先传图床，再按固定形状嵌进正文。少了那两个 class 雪球认不出它是图片。
func TestImageUploadAndEmbed(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	if _, err := opPostCreate(ctx, &XqPostCreateIn{
		Text: "带图", Images: []*plugin.File{{ID: "f1", Name: "a.png", Mime: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}
	var uploaded bool
	for i, p := range cap.paths {
		if p == "/photo/upload.json" {
			uploaded = true
			if ct := cap.headers[i].Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
				t.Errorf("传图应当走 multipart: %q", ct)
			}
			if !strings.Contains(cap.bodies[i], `name="file"`) {
				t.Error("字段名必须是 file")
			}
		}
	}
	if !uploaded {
		t.Fatal("没传图")
	}
	status := cap.form("/statuses/update.json").Get("status")
	if !strings.Contains(status, `class="img-single-upload"`) || !strings.Contains(status, `class="ke_img"`) {
		t.Errorf("图片没按雪球的形状嵌进去: %q", status)
	}
	if !strings.Contains(status, "//xqimg.imedao.com/abc.png!custom.jpg") {
		t.Errorf("图床地址/缩放后缀不对: %q", status)
	}
}

// 应答形状是猜的：换个键名也要能认出图片地址（宽松提取的意义）。
func TestUploadResponseShapeIsTolerated(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, map[string]func(http.ResponseWriter, *http.Request){
		"/photo/upload.json": func(w http.ResponseWriter, r *http.Request) {
			// 完全不同的形状：键名变了、嵌得更深
			io.WriteString(w, `{"data":{"result":{"image_url":"https://xqimg.imedao.com/zzz.jpg"}}}`)
		},
	})

	if _, err := opPostCreate(ctx, &XqPostCreateIn{
		Text: "带图", Images: []*plugin.File{{ID: "f1", Name: "a.png", Mime: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.form("/statuses/update.json").Get("status"), "xqimg.imedao.com/zzz.jpg") {
		t.Error("换个键名就认不出图片地址了——宽松提取没起作用")
	}
}

// 被风控拦下时雪球回的是一整页 HTML。按 JSON 解会得到「解析失败」，
// 而真正的原因是风控——错误信息必须说到点子上。
func TestWAFHtmlIsExplained(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, map[string]func(http.ResponseWriter, *http.Request){
		"/statuses/update.json": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<html><body>访问被拒绝</body></html>`)
		},
	})

	_, err := opPostCreate(ctx, &XqPostCreateIn{Text: "x"})
	if err == nil {
		t.Fatal("应当报错")
	}
	if !strings.Contains(err.Error(), "风控") || !strings.Contains(err.Error(), "md5__1038") {
		t.Errorf("要指出是风控并告诉人怎么办: %v", err)
	}
}

// Cookie 过期是最常见的失败，要说成人话而不是「HTTP 400」。
func TestExpiredCookieIsExplained(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, map[string]func(http.ResponseWriter, *http.Request){
		"/write/": func(w http.ResponseWriter, r *http.Request) {
			// 没登录时雪球把写作页 302 到登录页，页面上没有那段脚本
			io.WriteString(w, `<html><body>请登录</body></html>`)
		},
	})

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("凭证失效是结论不是故障: %v", err)
	}
	if out.OK {
		t.Error("Cookie 过期却说可用")
	}
	if !strings.Contains(out.Message, "重新登录") {
		t.Errorf("要告诉人怎么办: %q", out.Message)
	}
	// 登录态没了也该从 cookie 的 JWT 里兜出 uid，方便人对号入座
	if out.UID != "6212100204" {
		t.Errorf("uid 应当从 JWT 里兜底解出来: %q", out.UID)
	}
}

// 健康检查**不能有副作用**：不许往时间线上发东西。
func TestHealthCheckLeavesNoTrace(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil || !out.OK {
		t.Fatalf("应当通过: %+v %v", out, err)
	}
	for _, p := range cap.paths {
		if p == "/statuses/update.json" || p == "/photo/upload.json" {
			t.Errorf("健康检查产生了副作用: %s", p)
		}
	}
}

// session_token：凭证里粘了就用粘的，不必去抓页面。
func TestSessionTokenFromCredentialWins(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]string{"session_token": "manual_tok"}, nil)

	if _, err := opPostCreate(ctx, &XqPostCreateIn{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if got := cap.form("/statuses/update.json").Get("session_token"); got != "manual_tok" {
		t.Errorf("没用凭证里的 token: %q", got)
	}
	for _, p := range cap.paths {
		if p == "/" {
			t.Error("凭证里已经有 token 了，不该再去抓页面")
		}
	}
}

// 抓不到 token 时要告诉人去哪儿复制，而不是发一个必然被拒的请求。
func TestMissingSessionTokenIsActionable(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, map[string]func(http.ResponseWriter, *http.Request){
		"/": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `<html>没有那个东西</html>`)
		},
	})

	_, err := opPostCreate(ctx, &XqPostCreateIn{Text: "x"})
	if err == nil {
		t.Fatal("取不到 token 应当报错")
	}
	if !strings.Contains(err.Error(), "update.json") {
		t.Errorf("要告诉人去哪儿复制: %v", err)
	}
	for _, p := range cap.paths {
		if p == "/statuses/update.json" {
			t.Error("没有 token 却还是发了帖")
		}
	}
}

// 风控参数：凭证里填了才挂到 URL 上（默认不带，先试干净的）。
func TestRiskParamIsOptional(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)
	if _, err := opHealthCheck(ctx, &HealthCheckIn{}); err != nil {
		t.Fatal(err)
	}

	cap2 := &capture{}
	ctx2 := ctxTo(t, cap2, map[string]string{"risk_param": "2790552d-xyz"}, nil)
	if _, err := opHealthCheck(ctx2, &HealthCheckIn{}); err != nil {
		t.Fatal(err)
	}
	// 第一次不带、第二次带——两次都要能跑通
	if len(cap.paths) == 0 || len(cap2.paths) == 0 {
		t.Fatal("请求没发出去")
	}
}

// 正文必须先转义：一个 < 就能把整段 HTML 结构冲掉。
func TestTextIsEscaped(t *testing.T) {
	got := toXueqiuHTML(`风险提示 <script>alert(1)</script> 与 A<B`, nil)
	if strings.Contains(got, "<script>") {
		t.Errorf("正文里的标签没转义: %q", got)
	}
	if !strings.Contains(got, "A&lt;B") {
		t.Errorf("< 没转义: %q", got)
	}
}

func TestMarkdownAndLinks(t *testing.T) {
	got := toXueqiuHTML("**重点**在这\n详见 https://example.com/a", nil)
	if !strings.Contains(got, "<b>重点</b>") {
		t.Errorf("加粗没转: %q", got)
	}
	if !strings.Contains(got, `<a href="https://example.com/a"`) {
		t.Errorf("链接没包成 <a>: %q", got)
	}
	if strings.Count(got, "<p>") != 2 {
		t.Errorf("应当分成两段: %q", got)
	}
}

// 已经是 HTML 的正文原样透传——用户自己拼好的排版不该被二次加工。
func TestExistingHTMLPassesThrough(t *testing.T) {
	src := `<p>我自己拼的<b>排版</b></p>`
	if got := toXueqiuHTML(src, nil); got != src {
		t.Errorf("HTML 正文被改了: %q", got)
	}
}

// Cookie 不全时在发请求之前就说清楚。
func TestIncompleteCookieRejected(t *testing.T) {
	if _, err := cookieOf(Cred{Cookie: "u=123; device_id=d"}); err == nil ||
		!strings.Contains(err.Error(), "xq_a_token") {
		t.Errorf("要指出缺的是什么: %v", err)
	}
	if _, err := cookieOf(Cred{}); err == nil {
		t.Error("空 Cookie 应当被拦下")
	}
}

// —— 长文（mp.xueqiu.com 那套）——

// 长文接口**不要 session_token**，字段与短帖也不同：title + text + is_private。
func TestArticleDraftShape(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	out, err := opArticleDraft(ctx, &XqArticleDraftIn{
		Title: "四月债市观察", Content: "**核心结论**在这\n第二段",
	})
	if err != nil {
		t.Fatal(err)
	}
	f := cap.form("/xq/statuses/draft/save.json")
	if f == nil {
		t.Fatal("没发出存草稿请求")
	}
	if f.Get("title") != "四月债市观察" {
		t.Errorf("标题不对: %q", f.Get("title"))
	}
	if !strings.Contains(f.Get("text"), "<b>核心结论</b>") || strings.Count(f.Get("text"), "<p>") != 2 {
		t.Errorf("正文没转成 HTML: %q", f.Get("text"))
	}
	if f.Get("is_private") != "false" {
		t.Errorf("is_private = %q", f.Get("is_private"))
	}
	if f.Get("session_token") != "" {
		t.Error("长文那套不需要 session_token，别多发一个字段")
	}
	if out.ID != "88881111" || !strings.Contains(out.EditURL, "draft_id=88881111") {
		t.Errorf("产出不对: %+v", out)
	}
}

// 长文图床回的是 {url, filename} 两段，要拼起来并补上协议。
func TestArticleImageURLIsAssembled(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	if _, err := opArticleDraft(ctx, &XqArticleDraftIn{
		Title: "带图", Content: "正文",
		Images: []*plugin.File{{ID: "f1", Name: "a.png", Mime: "image/png"}},
	}); err != nil {
		t.Fatal(err)
	}
	text := cap.form("/xq/statuses/draft/save.json").Get("text")
	if !strings.Contains(text, `<img src="https://xqimg.imedao.com/dir/pic.png">`) {
		t.Errorf("图片地址没拼对（url + filename + 协议）: %q", text)
	}
	// 长文用普通 <img>，不是短帖那个 img-single-upload 的壳
	if strings.Contains(text, "img-single-upload") {
		t.Error("长文不该套短帖的图片壳")
	}
}

// 标题是长文的必填项——空标题在发出去之前拦下。
func TestArticleNeedsTitle(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	if _, err := opArticleDraft(ctx, &XqArticleDraftIn{Content: "正文"}); err == nil {
		t.Fatal("没标题应当被拦下")
	}
	for _, p := range cap.paths {
		if strings.Contains(p, "draft/save") {
			t.Error("拦下了却还是发了请求")
		}
	}
}

// 健康检查读写作页：拿到 uid 与昵称，且**不产生任何副作用**。
func TestHealthCheckReadsWritePage(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, nil)

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil || !out.OK {
		t.Fatalf("应当通过: %+v %v", out, err)
	}
	if out.UID != "6212100204" || out.Name != "投研君" {
		t.Errorf("uid/昵称没解出来: %+v", out)
	}
	for _, p := range cap.paths {
		if strings.Contains(p, "update.json") || strings.Contains(p, "draft/save") {
			t.Errorf("健康检查产生了副作用: %s", p)
		}
	}
}
