package main

// 假 LinkedIn。钉四件事：
//   - **两个必带的头**（LinkedIn-Version + X-Restli-Protocol-Version），少一个就是 426 或形状对不上；
//   - author 必须是账号自己的 URN，且**按 token 缓存**（每次发帖都去问一次是纯浪费）；
//   - 发帖成功时 id 在 **x-restli-id 响应头**里，正文可能是空的；
//   - 401 要翻译成「多半是 60 天到期了」——这是这家最常见的失败，而原文只说 unauthorized。

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
			// 发帖成功：id 在响应头里，body 是空的——LinkedIn 的老规矩
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

// 两个头都要带：少任何一个，LinkedIn 回 426 或形状对不上，而错误不会告诉你缺的是头。
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

// author 必须是账号自己的 URN；发帖 id 从响应头里取。
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

// 账号 URN 不会变：按 token 缓存，别每次发帖都去问一次。
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

// 图片是三步：initializeUpload → PUT 二进制 → URN 进帖子。
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

// 多张图要走 multiImage，不是 media。
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

// 401 是这家最常见的失败（令牌 60 天到期），原文只说 unauthorized，要翻译。
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

// 超长与空内容要在发出去之前拦下。
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

// 用户可能粘的是动态链接而不是 URN。
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
