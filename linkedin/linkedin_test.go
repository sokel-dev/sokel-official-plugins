package main

// A fake LinkedIn. Pins down four things:
//   - **Two mandatory headers** (LinkedIn-Version + X-Restli-Protocol-Version); missing either one
//     means a 426 or a mismatched shape;
//   - author must be the account's own URN, and it's **cached by token** (asking again on every
//     post would be pure waste);
//   - On a successful post, the id is in the **x-restli-id response header**, and the body can be
//     empty;
//   - A 401 must be translated to "most likely expired after 60 days" — this is this platform's
//     most common failure, and the raw message just says unauthorized.

import (
	"context"
	"encoding/json"
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

type capture struct {
	paths    []string
	bodies   []string
	versions []string
	restli   []string
}

func (c *capture) lastBody() map[string]any {
	if len(c.bodies) == 0 {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(c.bodies[len(c.bodies)-1]), &m)
	return m
}

func ctxTo(t *testing.T, cap *capture, routes map[string]func(http.ResponseWriter, *http.Request)) *fakeCtx {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		cap.paths = append(cap.paths, key)
		cap.bodies = append(cap.bodies, string(body))
		cap.versions = append(cap.versions, r.Header.Get("LinkedIn-Version"))
		cap.restli = append(cap.restli, r.Header.Get("X-Restli-Protocol-Version"))
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v2/userinfo"):
			io.WriteString(w, `{"sub":"abc123","name":"张三"}`)
		case r.URL.Path == "/rest/posts" && r.Method == http.MethodPost:
			// Successful post: id is in the response header, body is empty — an old LinkedIn convention
			w.Header().Set("x-restli-id", "urn:li:share:7100")
			w.WriteHeader(http.StatusCreated)
		case strings.HasPrefix(r.URL.Path, "/rest/posts/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/rest/images"):
			io.WriteString(w, `{"value":{"uploadUrl":"`+"http://"+r.Host+`/upload/1","image":"urn:li:image:img1"}}`)
		case strings.HasPrefix(r.URL.Path, "/upload/"):
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	apiBase = srv.URL
	meMu.Lock()
	meCache = map[string]meInfo{}
	meMu.Unlock()
	return &fakeCtx{Context: context.Background(), file: []byte("img"),
		cred: map[string]string{"access_token": "tok"}}
}

// Both headers must be sent: missing either one, LinkedIn returns a 426 or a mismatched shape, and
// the error won't tell you a header is missing.
func TestRequiredHeaders(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	if _, err := opPostCreate(ctx, &LiPostCreateIn{Text: "一条投研观点"}); err != nil {
		t.Fatal(err)
	}
	for i, p := range cap.paths {
		if cap.versions[i] == "" {
			t.Errorf("%s 少了 LinkedIn-Version 头", p)
		}
		if cap.restli[i] != "2.0.0" {
			t.Errorf("%s 的 X-Restli-Protocol-Version = %q", p, cap.restli[i])
		}
	}
}

// author must be the account's own URN; the post id is taken from the response header.
func TestPostShapeAndIDFromHeader(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	out, err := opPostCreate(ctx, &LiPostCreateIn{Text: "看好这个方向", Visibility: "CONNECTIONS"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "urn:li:share:7100" {
		t.Errorf("id 应当从 x-restli-id 头取，得到 %q", out.ID)
	}
	if out.URL != "https://www.linkedin.com/feed/update/urn:li:share:7100/" {
		t.Errorf("链接拼错了: %q", out.URL)
	}
	body := cap.lastBody()
	if body["author"] != "urn:li:person:abc123" {
		t.Errorf("author 不对: %v", body["author"])
	}
	if body["commentary"] != "看好这个方向" || body["visibility"] != "CONNECTIONS" {
		t.Errorf("正文/可见性不对: %v", body)
	}
	if body["lifecycleState"] != "PUBLISHED" {
		t.Errorf("缺 lifecycleState: %v", body)
	}
}

// The account URN never changes: cached by token, not asked again on every post.
func TestAuthorURNIsCached(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	for i := 0; i < 3; i++ {
		if _, err := opPostCreate(ctx, &LiPostCreateIn{Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, p := range cap.paths {
		if strings.Contains(p, "/v2/userinfo") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("账号信息查了 %d 次，应当只查一次并缓存", n)
	}
}

// Images are a three-step process: initializeUpload → PUT the binary → the URN goes into the post.
func TestImageUploadThreeSteps(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	_, err := opPostCreate(ctx, &LiPostCreateIn{
		Text: "带图", ImageAlts: []string{"一张图"},
		Images: []*plugin.File{{ID: "f1", Name: "a.jpg", Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var init, put bool
	for _, p := range cap.paths {
		if strings.Contains(p, "/rest/images?action=initializeUpload") {
			init = true
		}
		if strings.HasPrefix(p, "PUT /upload/") {
			put = true
		}
	}
	if !init || !put {
		t.Fatalf("三步没走全: %v", cap.paths)
	}
	content, _ := cap.lastBody()["content"].(map[string]any)
	media, _ := content["media"].(map[string]any)
	if media == nil || media["id"] != "urn:li:image:img1" {
		t.Errorf("图片 URN 没进帖子: %v", content)
	}
	if media["altText"] != "一张图" {
		t.Errorf("替代文本没带上: %v", media)
	}
}

// Multiple images go through multiImage, not media.
func TestMultipleImagesUseMultiImage(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	_, err := opPostCreate(ctx, &LiPostCreateIn{
		Text: "两张图",
		Images: []*plugin.File{
			{ID: "f1", Name: "a.jpg", Mime: "image/jpeg"},
			{ID: "f2", Name: "b.jpg", Mime: "image/jpeg"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := cap.lastBody()["content"].(map[string]any)
	if content["multiImage"] == nil {
		t.Errorf("多图应当走 multiImage: %v", content)
	}
}

// A 401 is this platform's most common failure (a token expired after 60 days); the raw message
// just says unauthorized, and it needs to be translated.
func TestExpiredTokenIsExplained(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"GET /v2/userinfo": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"Invalid access token"}`)
		},
	})

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("令牌过期是结论不是故障: %v", err)
	}
	if out.OK {
		t.Error("令牌过期却说可用")
	}
	if !strings.Contains(out.Message, "60 天") {
		t.Errorf("要点出「60 天到期」这条最常见的原因: %q", out.Message)
	}
}

// Overly long and empty content must be caught before anything gets sent.
func TestInputGuards(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	if _, err := opPostCreate(ctx, &LiPostCreateIn{Text: strings.Repeat("字", 3001)}); err == nil {
		t.Error("超过 3000 字符应当被拦下")
	}
	if _, err := opPostCreate(ctx, &LiPostCreateIn{}); err == nil {
		t.Error("空动态应当被拦下")
	}
	for _, p := range cap.paths {
		if strings.HasPrefix(p, "POST /rest/posts") {
			t.Error("拦下了却还是发了请求")
		}
	}
}

// A user might paste in a post link instead of a URN.
func TestPostURNFromLink(t *testing.T) {
	cases := map[string]string{
		"urn:li:share:7100": "urn:li:share:7100",
		"https://www.linkedin.com/feed/update/urn:li:share:7100/":     "urn:li:share:7100",
		"https://www.linkedin.com/feed/update/urn:li:activity:99?x=1": "urn:li:activity:99",
		"随便一个字符串": "",
	}
	for in, want := range cases {
		if got := postURN(in); got != want {
			t.Errorf("postURN(%q) = %q，想要 %q", in, got, want)
		}
	}
}
