package main

// A fake Xueqiu. This plugin calls **undocumented private endpoints**, so the tests
// focus on different things than usual:
//   - a request must look exactly like the one captured from the browser (form fields,
//     four headers) — missing one gets blocked by the WAF;
//   - the response shape is guesswork → loose extraction must hold up across key renames;
//   - a WAF block returns **a full HTML page**, which must not be reported as "parse
//     failed";
//   - converting text to HTML must escape first (a single < can break the whole structure).

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
	// payload is {"uid":6212100204}
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
		// Reading body first drains it, so ParseForm would get nothing — it must be put back after reading.
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
	mpBase = srv.URL // the long-form API lives on the mp subdomain; tests point both at the same fake server
	tokMu.Lock()
	tokCache = map[string]string{}
	tokMu.Unlock()
	cred := map[string]string{"cookie": testCookie}
	for k, v := range extra {
		cred[k] = v
	}
	return &fakeCtx{Context: context.Background(), cred: cred, file: []byte("PNG")}
}

// A post request must match the captured one: all form fields present, all four headers present.
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
		"ai_disclose":   "1", // must be 1 when enabled -- this is a compliance field
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
	// uid is decoded from the xq_id_token JWT payload, used to build the link
	if out.URL != "https://xueqiu.com/6212100204/123456789" {
		t.Errorf("链接拼错了: %q", out.URL)
	}
}

// Images: uploaded to the image host first, then embedded in the text in a fixed shape. Without those two classes, Xueqiu won't recognize it as an image.
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

// The response shape is guesswork: the image address must still be recognized even with a different key name (the whole point of loose extraction).
func TestUploadResponseShapeIsTolerated(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, map[string]func(http.ResponseWriter, *http.Request){
		"/photo/upload.json": func(w http.ResponseWriter, r *http.Request) {
			// a completely different shape: renamed keys, nested deeper
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

// When blocked by risk control, Xueqiu returns a full HTML page. Parsing it as JSON
// would just say "parse failed", while the real cause is risk control — the error
// message must get to the point.
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

// An expired cookie is the most common failure, and must be explained in plain language instead of "HTTP 400".
func TestExpiredCookieIsExplained(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil, map[string]func(http.ResponseWriter, *http.Request){
		"/write/": func(w http.ResponseWriter, r *http.Request) {
			// when not logged in, Xueqiu 302s the writer page to the login page, which doesn't have that script
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
	// even with the session gone, uid should still be recovered from the cookie's JWT as a fallback, to help people identify the account
	if out.UID != "6212100204" {
		t.Errorf("uid 应当从 JWT 里兜底解出来: %q", out.UID)
	}
}

// A health check **must have no side effects**: nothing may be posted to the timeline.
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

// session_token: if one is pasted into the credential, use it — no need to scrape the page.
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

// When the token can't be scraped, tell the user where to copy it from, rather than sending a request that's bound to be rejected.
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

// Risk-control param: only attached to the URL if it's set in the credential (off by default, try clean first).
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
	// first without it, second with it -- both must succeed
	if len(cap.paths) == 0 || len(cap2.paths) == 0 {
		t.Fatal("请求没发出去")
	}
}

// Text must be escaped first: a single < can break the whole HTML structure.
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

// Text that's already HTML is passed through as-is -- a user's own hand-built markup shouldn't be reprocessed.
func TestExistingHTMLPassesThrough(t *testing.T) {
	src := `<p>我自己拼的<b>排版</b></p>`
	if got := toXueqiuHTML(src, nil); got != src {
		t.Errorf("HTML 正文被改了: %q", got)
	}
}

// An incomplete cookie must be flagged clearly before a request is sent.
func TestIncompleteCookieRejected(t *testing.T) {
	if _, err := cookieOf(Cred{Cookie: "u=123; device_id=d"}); err == nil ||
		!strings.Contains(err.Error(), "xq_a_token") {
		t.Errorf("要指出缺的是什么: %v", err)
	}
	if _, err := cookieOf(Cred{}); err == nil {
		t.Error("空 Cookie 应当被拦下")
	}
}

// —— long-form articles (the mp.xueqiu.com API) ——

// The long-form endpoint **doesn't need session_token**, and its fields differ from short posts: title + text + is_private.
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

// The long-form image host returns {url, filename} as two parts, which must be joined and given a protocol prefix.
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
	// long-form uses a plain <img>, not the short-post img-single-upload wrapper
	if strings.Contains(text, "img-single-upload") {
		t.Error("长文不该套短帖的图片壳")
	}
}

// A title is required for long-form articles -- an empty title must be caught before sending.
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

// The health check reads the writer page: gets uid and display name, and **produces no side effects whatsoever**.
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
