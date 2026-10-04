package main

// 假 Threads。钉四件事：
//   - **两步发布**（建容器 → 发布），容器 id 不能当帖子 id 用；
//   - **带媒体要等就绪**再发布——不等的话随机失败、重试又能成，最难查的那种；
//   - 多个媒体要走**轮播**（每个先建 is_carousel_item 容器，再串成 CAROUSEL）；
//   - 图片与视频的参数名不同（image_url / video_url），给错了 Meta 只说「缺必需参数」。

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
		default: // 容器状态查询
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

// 两步：建容器拿 creation_id，再发布它。**容器 id 不是帖子 id**。
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

// 带媒体要等容器就绪。不等的话发布会被拒，而重试又能成。
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

// 媒体处理失败要把 Meta 给的原因带出来（多半是那个地址它下载不到）。
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

// 多个媒体走轮播：先各建 is_carousel_item 容器，再串成 CAROUSEL。
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

// 图片与视频的参数名不同，给错了 Meta 只说「缺必需参数」。
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

// 帖串：后一条回复前一条。
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

// 健康检查顺带带出剩余额度——工作流可以据此决定还发不发。
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

// 令牌过期是结论不是故障（平台拿 ok=false 去写凭证状态），且要点出「60 天」。
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

// 超长与空内容在发出去之前拦下。
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

// 平台文件带的是签名后的绝对地址；拿不到绝对地址的一律丢弃（Threads 下载不了相对路径）。
func TestOnlyAbsoluteURLsAreUsed(t *testing.T) {
	got := mediaURLs([]*plugin.File{
		{ID: "f1", URL: "https://files.example/f1?sig=x"},
		{ID: "f2", URL: "/api/v1/files/f2"}, // 相对路径：Threads 下载不到
		nil,
	}, []string{"https://cdn.example/c.jpg", "  "})
	want := []string{"https://files.example/f1?sig=x", "https://cdn.example/c.jpg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("媒体地址筛选不对: %v", got)
	}
}
