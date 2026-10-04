package main

// A fake Threads. Pins down four things:
//   - **Two-step publishing** (create container → publish); the container id must not be used as
//     the post id;
//   - **Media has to wait until ready** before publishing — skip that and it's random failures that
//     succeed on retry, the hardest kind to debug;
//   - Multiple media go through a **carousel** (each builds an is_carousel_item container first,
//     then they're strung into one CAROUSEL);
//   - Images and video use different parameter names (image_url / video_url), and getting it wrong
//     just gets "missing required parameter" from Meta.

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
}

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (c *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_1"}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) { panic("不用") }
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error)                           { return nil, nil }

type capture struct {
	paths  []string
	params []url.Values
}

func (c *capture) find(suffix string) []url.Values {
	var out []url.Values
	for i, p := range c.paths {
		if strings.HasSuffix(p, suffix) {
			out = append(out, c.params[i])
		}
	}
	return out
}

func ctxTo(t *testing.T, cap *capture, routes map[string]func(http.ResponseWriter, *http.Request)) *fakeCtx {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.paths = append(cap.paths, r.URL.Path)
		cap.params = append(cap.params, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		switch {
		case r.URL.Path == "/me":
			io.WriteString(w, `{"id":"u1","username":"acme"}`)
		case strings.HasSuffix(r.URL.Path, "/threads_publish"):
			io.WriteString(w, `{"id":"post_9"}`)
		case strings.HasSuffix(r.URL.Path, "/threads"):
			n++
			io.WriteString(w, `{"id":"c`+string(rune('0'+n))+`"}`)
		case strings.HasSuffix(r.URL.Path, "/threads_publishing_limit"):
			io.WriteString(w, `{"data":[{"quota_usage":12,"quota_config":{"quota_total":250}}]}`)
		default: // container status query
			io.WriteString(w, `{"status":"FINISHED"}`)
		}
	}))
	t.Cleanup(srv.Close)
	apiBase = srv.URL
	meMu.Lock()
	meCache = map[string]meInfo{}
	meMu.Unlock()
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"access_token": "tok"}}
}

// Two steps: create a container to get a creation_id, then publish it. **The container id is not
// the post id**.
func TestTwoStepPublish(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	out, err := opPostCreate(ctx, &ThPostCreateIn{Text: "一条观点"})
	if err != nil {
		t.Fatal(err)
	}
	created := cap.find("/threads")
	published := cap.find("/threads_publish")
	if len(created) != 1 || len(published) != 1 {
		t.Fatalf("应当建一次容器、发布一次: %v", cap.paths)
	}
	if created[0].Get("media_type") != "TEXT" || created[0].Get("text") != "一条观点" {
		t.Errorf("容器参数不对: %v", created[0])
	}
	if published[0].Get("creation_id") != "c1" {
		t.Errorf("发布用的不是容器 id: %v", published[0])
	}
	if out.ID != "post_9" {
		t.Errorf("帖子 id 应当来自发布那一步，得到 %q", out.ID)
	}
	if out.URL != "https://www.threads.net/@acme/post/post_9" {
		t.Errorf("链接拼错了: %q", out.URL)
	}
}

// With media, you have to wait for the container to be ready. Skip it and publishing gets
// rejected, but a retry succeeds.
func TestMediaWaitsForContainerReady(t *testing.T) {
	cap := &capture{}
	polls := 0
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/c1": func(w http.ResponseWriter, r *http.Request) {
			polls++
			if polls < 2 {
				io.WriteString(w, `{"status":"IN_PROGRESS"}`)
				return
			}
			io.WriteString(w, `{"status":"FINISHED"}`)
		},
	})

	_, err := opPostCreate(ctx, &ThPostCreateIn{
		Text: "带图", MediaUrls: []string{"https://cdn.example/a.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if polls < 2 {
		t.Errorf("应当等到容器就绪，只查了 %d 次", polls)
	}
}

// A media processing failure must carry out the reason Meta gave (most likely that it couldn't
// download the address).
func TestMediaErrorSurfaces(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/c1": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"status":"ERROR","error_message":"Media download failed"}`)
		},
	})

	_, err := opPostCreate(ctx, &ThPostCreateIn{
		Text: "带图", MediaUrls: []string{"https://cdn.example/nope.jpg"},
	})
	if err == nil {
		t.Fatal("媒体处理失败应当报错")
	}
	if !strings.Contains(err.Error(), "Media download failed") {
		t.Errorf("要带上 Meta 给的原因: %v", err)
	}
}

// Multiple media go through a carousel: each one builds an is_carousel_item container first, then
// they're strung into a CAROUSEL.
func TestMultipleMediaUseCarousel(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	_, err := opPostCreate(ctx, &ThPostCreateIn{
		Text:      "两张图",
		MediaUrls: []string{"https://cdn.example/a.jpg", "https://cdn.example/b.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	created := cap.find("/threads")
	if len(created) != 3 {
		t.Fatalf("两个子容器 + 一个轮播容器 = 3 次，得到 %d 次", len(created))
	}
	for i := 0; i < 2; i++ {
		if created[i].Get("is_carousel_item") != "true" {
			t.Errorf("第 %d 个子容器缺 is_carousel_item: %v", i+1, created[i])
		}
	}
	last := created[2]
	if last.Get("media_type") != "CAROUSEL" || last.Get("children") == "" {
		t.Errorf("轮播容器不对: %v", last)
	}
}

// Images and video use different parameter names, and getting it wrong just gets "missing required
// parameter" from Meta.
func TestVideoUsesVideoURLParam(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	if _, err := opPostCreate(ctx, &ThPostCreateIn{
		Text: "视频", MediaUrls: []string{"https://cdn.example/v.mp4?sig=abc"},
	}); err != nil {
		t.Fatal(err)
	}
	c := cap.find("/threads")[0]
	if c.Get("media_type") != "VIDEO" || c.Get("video_url") == "" {
		t.Errorf("视频应当用 VIDEO + video_url: %v", c)
	}
	if c.Get("image_url") != "" {
		t.Error("视频不该带 image_url")
	}
}

// Thread chain: each post replies to the one before it.
func TestThreadChains(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	out, err := opPostThread(ctx, &ThPostThreadIn{Texts: []string{"一", "二", "三"}, IntervalMs: 1})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 3 {
		t.Fatalf("应当发出 3 条: %+v", out)
	}
	created := cap.find("/threads")
	if created[0].Get("reply_to_id") != "" {
		t.Error("第一条不该是回复")
	}
	if created[1].Get("reply_to_id") != out.IDs[0] || created[2].Get("reply_to_id") != out.IDs[1] {
		t.Errorf("没串起来: %v / %v", created[1].Get("reply_to_id"), created[2].Get("reply_to_id"))
	}
}

// The health check surfaces the remaining quota along the way — a workflow can decide whether to
// keep publishing based on it.
func TestHealthCheckCarriesQuota(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Username != "acme" {
		t.Fatalf("产出不对: %+v", out)
	}
	if out.QuotaUsed != 12 || out.QuotaTotal != 250 {
		t.Errorf("额度没带出来: %d/%d", out.QuotaUsed, out.QuotaTotal)
	}
}

// An expired token is a conclusion, not a fault (the platform uses ok=false to write the
// credential's status), and it must call out "60 days".
func TestExpiredTokenIsExplained(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, map[string]func(http.ResponseWriter, *http.Request){
		"/me": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"Session has expired","code":190}}`)
		},
	})

	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("令牌过期不该报错: %v", err)
	}
	if out.OK || !strings.Contains(out.Message, "60 天") {
		t.Errorf("要点出 60 天到期这条最常见的原因: %+v", out)
	}
}

// Overly long and empty content are caught before anything gets sent.
func TestInputGuards(t *testing.T) {
	cap := &capture{}
	ctx := ctxTo(t, cap, nil)

	if _, err := opPostCreate(ctx, &ThPostCreateIn{Text: strings.Repeat("字", 501)}); err == nil {
		t.Error("超过 500 字符应当被拦下")
	}
	if _, err := opPostCreate(ctx, &ThPostCreateIn{}); err == nil {
		t.Error("空帖子应当被拦下")
	}
	for _, p := range cap.paths {
		if strings.HasSuffix(p, "/threads") {
			t.Error("拦下了却还是建了容器")
		}
	}
}

// A platform file carries a signed absolute URL; anything without an absolute URL is discarded
// (Threads can't download a relative path).
func TestOnlyAbsoluteURLsAreUsed(t *testing.T) {
	got := mediaURLs([]*plugin.File{
		{ID: "f1", URL: "https://files.example/f1?sig=x"},
		{ID: "f2", URL: "/api/v1/files/f2"}, // relative path: Threads can't download it
		nil,
	}, []string{"https://cdn.example/c.jpg", "  "})
	want := []string{"https://files.example/f1?sig=x", "https://cdn.example/c.jpg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("媒体地址筛选不对: %v", got)
	}
}
