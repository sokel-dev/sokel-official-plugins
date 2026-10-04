package main

// 假实例。钉四件事：字数上限**问实例**而不是写死 500、发布带幂等键、
// 嘟文串整串继承 CW 与可见性、媒体异步处理要等完再发嘟。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	return &plugin.File{ID: "f_1", Name: name, Mime: mime}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("不用流式上传")
}
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return c.file, nil }

func ctxTo(t *testing.T, base string) *fakeCtx {
	t.Helper()
	instMu.Lock()
	instChars = map[string]int{}
	instMu.Unlock()
	return &fakeCtx{Context: context.Background(), file: []byte("img"),
		cred: map[string]string{"instance_url": base, "access_token": "tok"}}
}

type capture struct {
	paths  []string
	forms  []url.Values
	idemps []string
}

func (c *capture) lastForm() url.Values {
	if len(c.forms) == 0 {
		return nil
	}
	return c.forms[len(c.forms)-1]
}

// fakeInstance：默认实例字数上限 5000（**不是 500**，用来钉「问实例」这件事）。
func fakeInstance(t *testing.T, cap *capture, routes map[string]func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cap.paths = append(cap.paths, r.Method+" "+r.URL.Path)
		cap.forms = append(cap.forms, r.PostForm)
		cap.idemps = append(cap.idemps, r.Header.Get("Idempotency-Key"))
		if h, ok := routes[r.Method+" "+r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/instance":
			io.WriteString(w, `{"configuration":{"statuses":{"max_characters":5000}}}`)
		case r.URL.Path == "/api/v1/statuses" && r.Method == http.MethodPost:
			n++
			io.WriteString(w, `{"id":"`+itoa(n)+`","url":"https://m.example/@acme/`+itoa(n)+`","visibility":"`+
				firstNonEmpty(r.PostFormValue("visibility"), "public")+`"}`)
		case strings.HasPrefix(r.URL.Path, "/api/v1/statuses/") && r.Method == http.MethodDelete:
			io.WriteString(w, `{}`)
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			io.WriteString(w, `{"acct":"acme","username":"acme"}`)
		case r.URL.Path == "/api/v2/media":
			io.WriteString(w, `{"id":"m1","url":"https://m.example/media/m1"}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":"Record not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func itoa(n int) string { return string(rune('0' + n)) }

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// 字数上限**问实例**。写死 500 的话，中文实例上一条本可以发的长文会被自己拦下来。
func TestCharLimitComesFromInstance(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, nil)

	// 800 字：在 500 上限的实例上该被拦，在这个 5000 的实例上必须发得出去
	out, err := opStatusCreate(ctxTo(t, srv.URL), &MastoStatusCreateIn{Text: strings.Repeat("字", 800)})
	if err != nil {
		t.Fatalf("实例上限是 5000，800 字不该被拦: %v", err)
	}
	if out.ID == "" {
		t.Fatal("没发出去")
	}
	var asked bool
	for _, p := range cap.paths {
		if strings.HasSuffix(p, "/api/v2/instance") {
			asked = true
		}
	}
	if !asked {
		t.Error("没去问实例的字数上限——那就是写死了")
	}
}

func TestOverInstanceLimitIsRejectedBeforeSending(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/v2/instance": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"configuration":{"statuses":{"max_characters":500}}}`)
		},
	})

	_, err := opStatusCreate(ctxTo(t, srv.URL), &MastoStatusCreateIn{Text: strings.Repeat("字", 501)})
	if err == nil {
		t.Fatal("超过实例上限应当在发出去之前拦下")
	}
	for _, p := range cap.paths {
		if strings.HasSuffix(p, "POST /api/v1/statuses") {
			t.Error("拦下了却还是发了请求")
		}
	}
}

// 幂等键：工作流重试不该在时间线上留两条一样的嘟文。
func TestPublishSendsStableIdempotencyKey(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, nil)
	in := &MastoStatusCreateIn{Text: "同一条内容", Visibility: "unlisted"}

	if _, err := opStatusCreate(ctxTo(t, srv.URL), in); err != nil {
		t.Fatal(err)
	}
	if _, err := opStatusCreate(ctxTo(t, srv.URL), in); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for i, p := range cap.paths {
		if strings.HasSuffix(p, "POST /api/v1/statuses") {
			keys = append(keys, cap.idemps[i])
		}
	}
	if len(keys) != 2 {
		t.Fatalf("应当发了两次，得到 %d 次", len(keys))
	}
	if keys[0] == "" {
		t.Error("没带 Idempotency-Key")
	}
	if keys[0] != keys[1] {
		t.Errorf("同一条内容两次的幂等键不同，幂等等于没做: %q vs %q", keys[0], keys[1])
	}
}

// CW 与可见性整串继承：混进两种可见性的话，读者只能看到断断续续的半串。
func TestThreadInheritsVisibilityAndCW(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, nil)

	out, err := opStatusThread(ctxTo(t, srv.URL), &MastoStatusThreadIn{
		Texts: []string{"一", "二", "三"}, Visibility: "unlisted", SpoilerText: "财报解读", IntervalMs: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 3 {
		t.Fatalf("应当发出 3 条: %+v", out)
	}
	var posts []url.Values
	for i, p := range cap.paths {
		if strings.HasSuffix(p, "POST /api/v1/statuses") {
			posts = append(posts, cap.forms[i])
		}
	}
	if len(posts) != 3 {
		t.Fatalf("发了 %d 次", len(posts))
	}
	for i, f := range posts {
		if f.Get("visibility") != "unlisted" {
			t.Errorf("第 %d 条的可见性没继承: %q", i+1, f.Get("visibility"))
		}
		if f.Get("spoiler_text") != "财报解读" {
			t.Errorf("第 %d 条的 CW 没继承: %q", i+1, f.Get("spoiler_text"))
		}
	}
	if posts[0].Get("in_reply_to_id") != "" {
		t.Error("第一条不该是回复")
	}
	if posts[1].Get("in_reply_to_id") != out.IDs[0] || posts[2].Get("in_reply_to_id") != out.IDs[1] {
		t.Errorf("没串起来: %v", []string{posts[1].Get("in_reply_to_id"), posts[2].Get("in_reply_to_id")})
	}
}

// 媒体异步：url 为空表示还在转码，这时发嘟会被 422 拒。要等到处理完。
func TestAsyncMediaIsAwaited(t *testing.T) {
	cap := &capture{}
	polls := 0
	srv := fakeInstance(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/v2/media": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"id":"m9","url":null}`) // 202：还在处理
		},
		"GET /api/v1/media/m9": func(w http.ResponseWriter, r *http.Request) {
			polls++
			if polls < 2 {
				io.WriteString(w, `{"id":"m9","url":null}`)
				return
			}
			io.WriteString(w, `{"id":"m9","url":"https://m.example/media/m9"}`)
		},
	})

	out, err := opStatusCreate(ctxTo(t, srv.URL), &MastoStatusCreateIn{
		Text: "带视频", Images: []*plugin.File{{ID: "f1", Name: "v.mp4", Mime: "video/mp4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if polls < 2 {
		t.Errorf("应当等到转码完成，只查了 %d 次", polls)
	}
	if got := cap.lastForm().Get("media_ids[]"); got != "m9" {
		t.Errorf("媒体没挂上: %q", got)
	}
	if out.ID == "" {
		t.Error("没发出去")
	}
}

// 投票与媒体不能同时给（Mastodon 的限制），要在发出去之前拦下。
func TestPollWithMediaRejected(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, nil)

	_, err := opStatusCreate(ctxTo(t, srv.URL), &MastoStatusCreateIn{
		Text: "投票", PollOptions: []string{"A", "B"},
		Images: []*plugin.File{{ID: "f1", Name: "a.jpg", Mime: "image/jpeg"}},
	})
	if err == nil {
		t.Fatal("投票 + 媒体应当被拦下")
	}
}

// 令牌无效是结论不是故障：平台拿 ok=false 去写凭证状态。
func TestHealthCheckReportsFailureAsResult(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/v1/accounts/verify_credentials": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"The access token is invalid"}`)
		},
	})

	out, err := opHealthCheck(ctxTo(t, srv.URL), &HealthCheckIn{})
	if err != nil {
		t.Fatalf("令牌无效不该报错: %v", err)
	}
	if out.OK {
		t.Error("令牌无效却说可用")
	}
	if !strings.Contains(out.Message, "撤销") {
		t.Errorf("要给人话原因: %q", out.Message)
	}
}

// 用户粘的可能是嘟文链接而不是 id。
func TestStatusIDFromLink(t *testing.T) {
	if got := statusID("https://m.example/@acme/109876543210"); got != "109876543210" {
		t.Errorf("链接没取出 id: %q", got)
	}
	if got := statusID("109876543210"); got != "109876543210" {
		t.Errorf("纯 id 被改动了: %q", got)
	}
}
