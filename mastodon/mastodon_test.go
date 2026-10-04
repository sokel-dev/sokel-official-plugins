package main

// A fake instance. Pins down four things: the character limit is **asked from the instance**
// rather than hardcoded to 500, publishing carries an idempotency key, a thread inherits CW and
// visibility across its whole length, and asynchronous media processing must finish before posting.

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

// fakeInstance: the default instance has a 5000 character limit (**not 500**, specifically to pin
// down the "asks the instance" behavior).
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

// The character limit **is asked from the instance**. Hardcoding 500 would reject a long post on
// a Chinese-language instance that could otherwise go through.
func TestCharLimitComesFromInstance(t *testing.T) {
	cap := &capture{}
	srv := fakeInstance(t, cap, nil)

	// 800 characters: should be rejected on a 500-limit instance, but must go through on this
	// 5000-limit one
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

// Idempotency key: a workflow retry shouldn't leave two identical posts on the timeline.
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

// CW and visibility are inherited across a whole thread: mixing in two different visibilities
// leaves readers seeing only a disjointed half.
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

// Media is asynchronous: an empty url means it's still transcoding, and posting at that point gets
// a 422. It has to wait until processing is done.
func TestAsyncMediaIsAwaited(t *testing.T) {
	cap := &capture{}
	polls := 0
	srv := fakeInstance(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/v2/media": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"id":"m9","url":null}`) // 202: still processing
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

// A poll and media can't be given together (a Mastodon limitation), and must be caught before
// sending anything.
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

// An invalid token is a conclusion, not a fault: the platform uses ok=false to write the
// credential's status.
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

// A user might paste in a post link instead of an id.
func TestStatusIDFromLink(t *testing.T) {
	if got := statusID("https://m.example/@acme/109876543210"); got != "109876543210" {
		t.Errorf("链接没取出 id: %q", got)
	}
	if got := statusID("109876543210"); got != "109876543210" {
		t.Errorf("纯 id 被改动了: %q", got)
	}
}
