package main

// A fake WeChat. Pins down five things:
//   - **Business errors are packed inside an HTTP 200** (errcode != 0); checking only the status
//     code would treat "IP not in the allowlist" as success;
//   - An invalid token (40001) should clear the cache and retry once, without the caller seeing it;
//   - A missing cover image and foreign images in the body must be caught **before creating the
//     draft** (otherwise you only find out after publishing that the article has no images);
//   - Publishing is asynchronous, and submit succeeding doesn't mean publishing succeeded;
//   - A single health check validates three things: "credentials / allowlist / permission".

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

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

type capture struct{ paths []string }

func ctxTo(t *testing.T, cap *capture, routes map[string]func(http.ResponseWriter, *http.Request)) *fakeCtx {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.paths = append(cap.paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		switch r.URL.Path {
		case "/cgi-bin/token":
			io.WriteString(w, `{"access_token":"tok1","expires_in":7200}`)
		case "/cgi-bin/draft/add":
			io.WriteString(w, `{"media_id":"m_draft"}`)
		case "/cgi-bin/freepublish/submit":
			io.WriteString(w, `{"errcode":0,"publish_id":100000001}`)
		case "/cgi-bin/freepublish/get":
			io.WriteString(w, `{"publish_status":0,"article_id":"art1",
				"article_detail":{"item":[{"article_url":"https://mp.weixin.qq.com/s/abc"}]}}`)
		case "/cgi-bin/draft/count":
			io.WriteString(w, `{"total_count":7}`)
		case "/cgi-bin/material/add_material":
			io.WriteString(w, `{"media_id":"m_cover","url":"https://mmbiz.qpic.cn/cover.jpg"}`)
		case "/cgi-bin/media/uploadimg":
			io.WriteString(w, `{"url":"https://mmbiz.qpic.cn/inline.jpg"}`)
		default:
			io.WriteString(w, `{"errcode":48001,"errmsg":"api unauthorized"}`)
		}
	}))
	t.Cleanup(srv.Close)
	apiBase = srv.URL
	tokMu.Lock()
	tokCache = map[string]cachedToken{}
	tokMu.Unlock()
	return &fakeCtx{Context: context.Background(), file: []byte("img"),
		cred: map[string]string{"app_id": "wx1", "app_secret": "sec"}}
}

// WeChat packs business errors inside an HTTP 200. Checking only the status code would treat
// "IP not in the allowlist" as success.
func TestBusinessErrorInsideHTTP200(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/cgi-bin/draft/count": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK) // 200!
			io.WriteString(w, `{"errcode":40164,"errmsg":"invalid ip 1.2.3.4 not in whitelist"}`)
		},
	})

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("健康检查不该报错，结论要落在 ok 上: %v", err)
	}
	if out.OK {
		t.Fatal("40164 被当成了成功——这正是只看状态码的后果")
	}
	if !strings.Contains(out.Message, "白名单") {
		t.Errorf("要说清是白名单的事: %q", out.Message)
	}
}

// An unverified account's revoked publishing permission (48001) must be spelled out, or nobody
// would know to go get verified.
func TestUnauthorizedAccountIsExplained(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/cgi-bin/draft/count": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"errcode":48001,"errmsg":"api unauthorized"}`)
		},
	})
	out, _ := opHealthCheck(ctx, &HealthCheckIn{})
	if out.OK || !strings.Contains(out.Message, "认证") {
		t.Errorf("48001 要指出「需要认证的服务号/订阅号」: %+v", out)
	}
}

// An invalid token (40001) clears the cache and retries once — this is what self-heals when
// multiple replicas keep invalidating each other's token.
func TestExpiredTokenIsRetriedOnce(t *testing.T) {
	cap := &capture{}
	first := true
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/cgi-bin/draft/count": func(w http.ResponseWriter, r *http.Request) {
			if first {
				first = false
				io.WriteString(w, `{"errcode":40001,"errmsg":"invalid credential"}`)
				return
			}
			io.WriteString(w, `{"total_count":3}`)
		},
	})

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.DraftCount != 3 {
		t.Fatalf("应当重试后成功: %+v", out)
	}
	n := 0
	for _, p := range cap.paths {
		if p == "/cgi-bin/token" {
			n++
		}
	}
	if n < 2 {
		t.Errorf("token 失效后应当重新换一次，只换了 %d 次", n)
	}
}

// The cover image is mandatory: WeChat would otherwise return a cryptic 41005 — better to say it
// plainly before even sending the request.
func TestMissingCoverIsRejectedEarly(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	_, err := opDraftAdd(ctx, &MpDraftAddIn{Title: "标题", Content: "<p>正文</p>"})
	if err == nil {
		t.Fatal("没有封面图应当被拦下")
	}
	if !strings.Contains(err.Error(), "封面") {
		t.Errorf("要说清缺的是什么: %v", err)
	}
	for _, p := range cap.paths {
		if p == "/cgi-bin/draft/add" {
			t.Error("拦下了却还是发了请求")
		}
	}
}

// A hotlinked image in the body gets blocked by WeChat — you'd only discover after publishing that
// the article has no images. This must be caught before creating the draft.
func TestForeignImageIsRejectedEarly(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	_, err := opDraftAdd(ctx, &MpDraftAddIn{
		Title: "标题", ThumbMediaID: "m_cover",
		Content: `<p>看图</p><img src="https://example.com/a.png">`,
	})
	if err == nil {
		t.Fatal("外链图应当被拦下")
	}
	if !strings.Contains(err.Error(), "上传图片") {
		t.Errorf("要指出路: %v", err)
	}
	// An image already on WeChat's own domain shouldn't get caught by mistake
	if _, err := opDraftAdd(ctx, &MpDraftAddIn{
		Title: "标题", ThumbMediaID: "m_cover",
		Content: `<img src="https://mmbiz.qpic.cn/x.jpg" alt="ok">`,
	}); err != nil {
		t.Errorf("微信域名的图被误伤了: %v", err)
	}
}

// The shape of a create-draft request: an articles array + the three required fields.
func TestDraftAddShape(t *testing.T) {
	cap := &capture{}
	var body string
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/cgi-bin/draft/add": func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			io.WriteString(w, `{"media_id":"m_draft"}`)
		},
	})

	out, err := opDraftAdd(ctx, &MpDraftAddIn{
		Title: "A股复盘", Content: "<p>正文</p>", ThumbMediaID: "m_cover",
		Author: "投研部", Digest: "摘要", ContentSourceURL: "https://example.com/r/1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.MediaID != "m_draft" {
		t.Errorf("没拿到草稿 media_id: %+v", out)
	}
	if !strings.Contains(body, `"articles"`) || !strings.Contains(body, `"thumb_media_id":"m_cover"`) {
		t.Errorf("请求形状不对: %s", body)
	}
	// Chinese characters shouldn't be escaped to \uXXXX (WeChat endpoints handle this
	// inconsistently; sending the raw text is the safest option)
	if !strings.Contains(body, "A股复盘") {
		t.Errorf("中文被转义了: %s", body)
	}
}

// Publishing is asynchronous: submit succeeding doesn't mean publishing succeeded; it has to wait
// for a terminal state and translate that status into something human-readable.
func TestPublishWaitsForResult(t *testing.T) {
	cap := &capture{}
	polls := 0
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/cgi-bin/freepublish/get": func(w http.ResponseWriter, r *http.Request) {
			polls++
			if polls < 2 {
				io.WriteString(w, `{"publish_status":1}`) // publishing
				return
			}
			io.WriteString(w, `{"publish_status":0,"article_id":"art1",
				"article_detail":{"item":[{"article_url":"https://mp.weixin.qq.com/s/abc"}]}}`)
		},
	})

	out, err := opPublish(ctx, &MpPublishIn{MediaID: "m_draft", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	if polls < 2 {
		t.Errorf("应当等到终态，只查了 %d 次", polls)
	}
	if out.Status != "success" || out.ArticleURL == "" {
		t.Errorf("产出不对: %+v", out)
	}
	if out.PublishID != "100000001" {
		t.Errorf("publish_id 是数字型，要归一成字符串: %q", out.PublishID)
	}
}

// A failed review must report an error **with a human-readable status attached**, not be treated as
// a successful publish.
func TestPublishFailureSurfaces(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/cgi-bin/freepublish/get": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"publish_status":3}`) // failed review
		},
	})

	out, err := opPublish(ctx, &MpPublishIn{MediaID: "m_draft", Wait: true})
	if err == nil {
		t.Fatal("审核不通过不该报成功")
	}
	if out.Status != "audit_failed" {
		t.Errorf("状态要翻译成人话: %q", out.Status)
	}
}

// The two purposes produce different outputs: cover needs a media_id, body needs a URL; the size
// limits differ too.
func TestImageUploadPurposes(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	cover, err := opImageUpload(ctx, &MpImageUploadIn{
		File: &plugin.File{ID: "f1", Name: "c.jpg", Mime: "image/jpeg"}, Purpose: "cover"})
	if err != nil {
		t.Fatal(err)
	}
	if cover.MediaID != "m_cover" {
		t.Errorf("封面要 media_id: %+v", cover)
	}
	inline, err := opImageUpload(ctx, &MpImageUploadIn{
		File: &plugin.File{ID: "f2", Name: "i.jpg", Mime: "image/jpeg"}, Purpose: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	if inline.URL == "" || inline.MediaID != "" {
		t.Errorf("正文配图只回地址: %+v", inline)
	}

	// The limit for an inline body image is 1MB (not the cover's 10MB), and must be caught before
	// uploading
	big := &fakeCtx{Context: context.Background(), cred: ctx.cred, file: make([]byte, (1<<20)+1)}
	if _, err := opImageUpload(big, &MpImageUploadIn{
		File: &plugin.File{ID: "f3", Name: "big.jpg", Mime: "image/jpeg"}, Purpose: "inline"}); err == nil {
		t.Error("超过 1MB 的正文配图应当被拦下")
	}
}

func TestPublishStatusNames(t *testing.T) {
	for code, want := range map[int]string{0: "success", 1: "publishing", 2: "original_failed",
		3: "audit_failed", 9: "banned"} {
		if got := publishStatus(code); got != want {
			t.Errorf("publishStatus(%d) = %q，想要 %q", code, got, want)
		}
	}
}
