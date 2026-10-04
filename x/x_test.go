package main

// 假 X 上游。钉的是**别处看不出来的那几件事**：归一化（作者/媒体/转推判定）、
// 增量游标（since_id 进、newest_id 出、空结果不冲游标）、推串的串接与中断、
// 媒体分片的片序、以及限流头是绝对时间戳而不是秒数。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// —— 假上下文 ——

type fakeCtx struct {
	context.Context
	cred map[string]string
	file []byte
}

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (c *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f_test", Name: name, Mime: mime, Size: int64(len(data))}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("本插件不用流式上传")
}
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return c.file, nil }

func ctxTo(t *testing.T, url string) *fakeCtx {
	t.Helper()
	resetCaches(url)
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"access_token": "tok_" + url}}
}

// resetCaches：me 是按 token 缓存的，测试之间必须清掉，
// 否则第二个用例拿到的是第一个假服务器的账号（这类串味最难查）。
func resetCaches(_ string) {
	meMu.Lock()
	meCache = map[string]rawUser{}
	meMu.Unlock()
	postFieldsParam.Store("")
}

// —— 假上游 ——

type capture struct {
	paths   []string
	queries []url.Values
	bodies  []string
}

// fakeX：按「方法 + 路径前缀」匹配路由；/users/me 总是内置的。
func fakeX(t *testing.T, cap *capture, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.paths = append(cap.paths, r.Method+" "+r.URL.Path)
		cap.queries = append(cap.queries, r.URL.Query())
		cap.bodies = append(cap.bodies, string(body))

		if strings.HasSuffix(r.URL.Path, "/users/me") {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":{"id":"9001","username":"acme_bot","name":"Acme"}}`)
			return
		}
		for prefix, resp := range routes {
			if strings.HasPrefix(r.Method+" "+r.URL.Path, prefix) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, resp)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"title":"Not Found Error","detail":"没有这条路由: `+r.URL.Path+`"}`)
	}))
	t.Cleanup(srv.Close)
	// xAPI 是包级常量，测试里指到假服务器上。
	old := xAPI
	xAPI = srv.URL
	t.Cleanup(func() { xAPI = old })
	return srv
}

// —— 归一化 ——

const searchResp = `{
  "data": [
    {"id":"200","text":"RT @someone: 原文在这","author_id":"11","created_at":"2026-08-18T02:00:00Z",
     "conversation_id":"200","referenced_tweets":[{"type":"retweeted","id":"100"}],
     "public_metrics":{"reply_count":1,"retweet_count":2,"like_count":3,"quote_count":4,"impression_count":500}},
    {"id":"201","text":"看看这个 https://t.co/abc #AI","author_id":"12","created_at":"2026-08-18T03:00:00Z",
     "conversation_id":"201","attachments":{"media_keys":["3_777"]},
     "entities":{"urls":[{"url":"https://t.co/abc","expanded_url":"https://example.com/post"}],
                 "hashtags":[{"tag":"AI"}]}}
  ],
  "includes": {
    "users":[{"id":"11","username":"alice","name":"Alice"},{"id":"12","username":"bob","name":"Bob"}],
    "media":[{"media_key":"3_777","type":"photo","url":"https://pbs.x.com/p.jpg","alt_text":"一张图"}]
  },
  "meta": {"result_count":2,"newest_id":"201","oldest_id":"200"}
}`

// 作者、媒体、外链都在 includes/entities 里，画布上引用不到——归一化必须在插件里做完。
func TestSearchNormalizesIncludes(t *testing.T) {
	var cap capture
	srv := fakeX(t, &cap, map[string]string{"GET /tweets/search/recent": searchResp})

	out, err := opSearch(ctxTo(t, srv.URL), &XSearchIn{Query: "#AI", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Fatalf("拿到 %d 条", out.Count)
	}
	// 正序：老的在前
	first, second := out.Items[0], out.Items[1]
	if first.ID != "200" || second.ID != "201" {
		t.Errorf("应当按 id 正序给出，得到 %s,%s", first.ID, second.ID)
	}
	if first.AuthorUsername != "alice" || first.AuthorName != "Alice" {
		t.Errorf("作者没关联上: %+v", first)
	}
	if first.URL != "https://x.com/alice/status/200" {
		t.Errorf("链接 = %q", first.URL)
	}
	// 转推判定：X 不给这个字段，靠 referenced_tweets 推
	if first.Kind != "retweeted" || first.RefPostID != "100" {
		t.Errorf("转推没认出来: kind=%s ref=%s", first.Kind, first.RefPostID)
	}
	if first.LikeCount != 3 || first.ViewCount != 500 {
		t.Errorf("互动数没带出来: like=%d view=%d", first.LikeCount, first.ViewCount)
	}
	if second.Kind != "original" {
		t.Errorf("原创被判成了 %s", second.Kind)
	}
	// t.co 短链下游用不了，必须展开
	if len(second.Links) != 1 || second.Links[0] != "https://example.com/post" {
		t.Errorf("外链没展开: %v", second.Links)
	}
	if len(second.Tags) != 1 || second.Tags[0] != "AI" {
		t.Errorf("话题标签: %v", second.Tags)
	}
	if len(second.Media) != 1 || second.Media[0].URL != "https://pbs.x.com/p.jpg" || second.Media[0].AltText != "一张图" {
		t.Errorf("媒体没关联上: %+v", second.Media)
	}
}

// 游标：传进去的是 since_id，回来的是本批最新 id。
func TestSearchCursorIsSinceID(t *testing.T) {
	var cap capture
	srv := fakeX(t, &cap, map[string]string{"GET /tweets/search/recent": searchResp})

	out, err := opSearch(ctxTo(t, srv.URL), &XSearchIn{Query: "x", Cursor: "150", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got := cap.queries[0].Get("since_id"); got != "150" {
		t.Errorf("游标没变成 since_id: %q", got)
	}
	if out.NextCursor != "201" {
		t.Errorf("下一个游标应当是本批最新的 id，得到 %q", out.NextCursor)
	}
	if out.HasMore {
		t.Error("一页就拉完了，不该说还有更多")
	}
}

// 一轮没有新推文时**不能把游标冲掉**——冲掉就等于下一轮从头再拉一遍（还要再花一遍钱）。
func TestSearchEmptyKeepsCursor(t *testing.T) {
	var cap capture
	srv := fakeX(t, &cap, map[string]string{
		"GET /tweets/search/recent": `{"data":[],"meta":{"result_count":0}}`,
	})

	out, err := opSearch(ctxTo(t, srv.URL), &XSearchIn{Query: "x", Cursor: "150", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if out.NextCursor != "150" {
		t.Errorf("空结果应当原样返回游标，得到 %q", out.NextCursor)
	}
	if out.Count != 0 {
		t.Errorf("不该凭空多出条目: %d", out.Count)
	}
}

// 每页条数有下限（搜索 10、提及 5），低于下限 X 直接 400——
// 「我只要 3 条」这种再正常不过的配置不该失败。
func TestPageSizeRespectsEndpointFloor(t *testing.T) {
	if got := pageSize(3, "/tweets/search/recent"); got != 10 {
		t.Errorf("搜索的下限是 10，得到 %d", got)
	}
	if got := pageSize(3, "/users/9001/mentions"); got != 5 {
		t.Errorf("提及的下限是 5，得到 %d", got)
	}
	if got := pageSize(500, "/tweets/search/recent"); got != 100 {
		t.Errorf("上限是 100，得到 %d", got)
	}
}

// —— 发布 ——

func TestPostCreateBody(t *testing.T) {
	var cap capture
	srv := fakeX(t, &cap, map[string]string{
		"POST /tweets": `{"data":{"id":"999","text":"你好"}}`,
	})

	out, err := opPostCreate(ctxTo(t, srv.URL), &XPostCreateIn{
		Text: "你好", ReplyToID: "888", MediaIDs: []string{"m1", "m2"}, ReplySettings: "everyone",
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(cap.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	reply, _ := body["reply"].(map[string]any)
	if reply == nil || reply["in_reply_to_tweet_id"] != "888" {
		t.Errorf("回复没按 X 的嵌套形状发: %v", body)
	}
	media, _ := body["media"].(map[string]any)
	if media == nil || len(media["media_ids"].([]any)) != 2 {
		t.Errorf("媒体没带上: %v", body)
	}
	// everyone 是 X 的默认值，显式传反而被某些档位拒
	if _, ok := body["reply_settings"]; ok {
		t.Error("reply_settings=everyone 不该发出去")
	}
	if out.URL != "https://x.com/acme_bot/status/999" {
		t.Errorf("发完要给链接，得到 %q", out.URL)
	}
}

func TestPostCreateRejectsPollWithMedia(t *testing.T) {
	var cap capture
	srv := fakeX(t, &cap, map[string]string{"POST /tweets": `{"data":{"id":"1"}}`})

	_, err := opPostCreate(ctxTo(t, srv.URL), &XPostCreateIn{
		Text: "投票", MediaIDs: []string{"m1"}, PollOptions: []string{"A", "B"},
	})
	if err == nil {
		t.Fatal("投票+媒体是 X 的禁忌组合，应当在发出去之前就拦下")
	}
	if len(cap.paths) > 0 {
		t.Errorf("拦下了却还是发了请求: %v", cap.paths)
	}
}

// 推串：后一条必须回复前一条，否则发出去的是 N 条互不相干的推文。
func TestPostThreadChains(t *testing.T) {
	var cap capture
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/users/me") {
			io.WriteString(w, `{"data":{"id":"9001","username":"acme_bot"}}`)
			return
		}
		// 只记发推的 body：查账号那次（拼链接用）混进来会把下面的下标全带偏。
		body, _ := io.ReadAll(r.Body)
		cap.paths = append(cap.paths, r.Method+" "+r.URL.Path)
		cap.bodies = append(cap.bodies, string(body))
		n++
		io.WriteString(w, `{"data":{"id":"100`+string(rune('0'+n))+`"}}`)
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()
	resetCaches(srv.URL)

	out, err := opPostThread(&fakeCtx{Context: context.Background(),
		cred: map[string]string{"access_token": "t"}}, &XPostThreadIn{
		Texts: []string{"一", "二", "三"}, IntervalMs: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 3 || len(out.IDs) != 3 {
		t.Fatalf("应当发出 3 条: %+v", out)
	}
	// 第 2、3 条的 in_reply_to 必须是前一条的 id
	var second, third map[string]any
	_ = json.Unmarshal([]byte(cap.bodies[len(cap.bodies)-2]), &second)
	_ = json.Unmarshal([]byte(cap.bodies[len(cap.bodies)-1]), &third)
	if r, _ := second["reply"].(map[string]any); r == nil || r["in_reply_to_tweet_id"] != out.IDs[0] {
		t.Errorf("第二条没接在第一条后面: %v", second)
	}
	if r, _ := third["reply"].(map[string]any); r == nil || r["in_reply_to_tweet_id"] != out.IDs[1] {
		t.Errorf("第三条没接在第二条后面: %v", third)
	}
}

// 推串中途失败：**不回滚**，但已发出的 id 必须出现在错误里，否则人不知道断在哪、从哪接。
func TestPostThreadReportsWhatWasSent(t *testing.T) {
	var cap capture
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/users/me") {
			io.WriteString(w, `{"data":{"id":"9001","username":"acme_bot"}}`)
			return
		}
		n++
		if n == 2 {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"title":"Forbidden","detail":"重复内容"}`)
			return
		}
		io.WriteString(w, `{"data":{"id":"777"}}`)
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()
	resetCaches(srv.URL)
	_ = cap

	_, err := opPostThread(&fakeCtx{Context: context.Background(),
		cred: map[string]string{"access_token": "t"}}, &XPostThreadIn{
		Texts: []string{"一", "二"}, IntervalMs: 1,
	})
	if err == nil {
		t.Fatal("第二条失败了，应当报错")
	}
	if !strings.Contains(err.Error(), "777") {
		t.Errorf("错误里要带上已发出的 id，得到: %v", err)
	}
}

// —— 互动 ——

// 这四对挂在**授权账号自己**身上，路径里的 id 是 me 的不是被操作对象的。
func TestEngageUsesMyID(t *testing.T) {
	var cap capture
	srv := fakeX(t, &cap, map[string]string{
		"POST /users/9001/likes":       `{"data":{"liked":true}}`,
		"DELETE /users/9001/retweets/": `{"data":{"retweeted":false}}`,
	})
	ctx := ctxTo(t, srv.URL)

	if _, err := opLike(ctx, &XLikeIn{PostID: "555"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cap.paths[len(cap.paths)-1], "/users/9001/likes") {
		t.Errorf("点赞路径不对: %v", cap.paths)
	}
	if !strings.Contains(cap.bodies[len(cap.bodies)-1], `"tweet_id":"555"`) {
		t.Errorf("点赞 body 不对: %s", cap.bodies[len(cap.bodies)-1])
	}
	if _, err := opUnrepost(ctx, &XUnrepostIn{PostID: "555"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cap.paths[len(cap.paths)-1], "/users/9001/retweets/555") {
		t.Errorf("取消转推路径不对: %v", cap.paths)
	}
}

// —— 媒体 ——

// 分片：片序必须从 0 连续递增，错一片整个文件就废了（X 只在 FINALIZE 时才报错）。
func TestMediaUploadChunks(t *testing.T) {
	var segs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/media/upload/initialize"):
			io.WriteString(w, `{"data":{"id":"m123","media_key":"3_m123","expires_after_secs":3600}}`)
		case strings.HasSuffix(r.URL.Path, "/append"):
			_ = r.ParseMultipartForm(16 << 20)
			segs = append(segs, r.FormValue("segment_index"))
			io.WriteString(w, `{"data":{"expires_at":1}}`)
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			io.WriteString(w, `{"data":{"id":"m123","media_key":"3_m123","size":10}}`)
		case strings.HasSuffix(r.URL.Path, "/media/metadata"):
			io.WriteString(w, `{"data":{"id":"m123"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()

	ctx := &fakeCtx{Context: context.Background(),
		cred: map[string]string{"access_token": "t"}, file: make([]byte, chunkSize+10)}
	out, err := opMediaUpload(ctx, &XMediaUploadIn{
		File: &plugin.File{ID: "f1", Name: "v.mp4", Mime: "video/mp4"}, AltText: "描述", Purpose: "post",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 || segs[0] != "0" || segs[1] != "1" {
		t.Errorf("片序必须从 0 连续递增，得到 %v", segs)
	}
	if out.MediaID != "m123" || out.State != "succeeded" {
		t.Errorf("产出不对: %+v", out)
	}
}

// 转码没完就返回 media_id 的话，下游发推会得到「media not found」，看起来像 id 错了。
func TestMediaUploadWaitsForProcessing(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/initialize"):
			io.WriteString(w, `{"data":{"id":"m9"}}`)
		case strings.HasSuffix(r.URL.Path, "/append"):
			io.WriteString(w, `{"data":{}}`)
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			io.WriteString(w, `{"data":{"id":"m9","processing_info":{"state":"in_progress","check_after_secs":0}}}`)
		case r.URL.Query().Get("command") == "STATUS":
			polls++
			if polls < 2 {
				io.WriteString(w, `{"data":{"id":"m9","processing_info":{"state":"in_progress","check_after_secs":0}}}`)
				return
			}
			io.WriteString(w, `{"data":{"id":"m9","processing_info":{"state":"succeeded"}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := xAPI
	xAPI = srv.URL
	defer func() { xAPI = old }()

	ctx := &fakeCtx{Context: context.Background(),
		cred: map[string]string{"access_token": "t"}, file: []byte("小文件")}
	out, err := opMediaUpload(ctx, &XMediaUploadIn{
		File: &plugin.File{ID: "f1", Name: "v.mp4", Mime: "video/mp4"}, Purpose: "post",
	})
	if err != nil {
		t.Fatal(err)
	}
	if polls < 2 {
		t.Errorf("应当等到转码结束，只查了 %d 次", polls)
	}
	if out.State != "succeeded" {
		t.Errorf("状态 = %q", out.State)
	}
}

// 用途决定 media_category，填错要到发布那一步才被拒——那时已经传完一个几十兆的视频了。
func TestMediaCategory(t *testing.T) {
	cases := map[[2]string]string{
		{"image/jpeg", "post"}: "tweet_image",
		{"image/gif", "post"}:  "tweet_gif",
		{"video/mp4", "post"}:  "tweet_video",
		{"image/png", "dm"}:    "dm_image",
		{"video/mp4", "dm"}:    "dm_video",
	}
	for in, want := range cases {
		got, err := mediaCategory(in[0], in[1])
		if err != nil || got != want {
			t.Errorf("mediaCategory(%q,%q) = %q,%v；想要 %q", in[0], in[1], got, err, want)
		}
	}
	if _, err := mediaCategory("application/pdf", "post"); err == nil {
		t.Error("PDF 不是 X 收的媒体，应当当场报错")
	}
}

// —— 错误 ——

// x-rate-limit-reset 是**绝对 epoch 秒**。当成「等几秒」用会等到明年。
func TestRateLimitResetIsAbsolute(t *testing.T) {
	reset := time.Now().Add(90 * time.Second).Unix()
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	resp.Header.Set("x-rate-limit-reset", strconv.FormatInt(reset, 10))

	e := parseError(resp, []byte(`{"title":"Too Many Requests"}`))
	if e.Reset.IsZero() {
		t.Fatal("没解析出恢复时刻")
	}
	if d := backoff(e, 0); d < 80*time.Second || d > 100*time.Second {
		t.Errorf("退避时长应当约等于 90 秒，得到 %s", d)
	}
	if !strings.Contains(e.Error(), "才恢复") {
		t.Errorf("错误里要说清几点恢复: %v", e)
	}
}

// 403 是 X 上最难查的错：原文从不说是权限、作用域还是档位。
func TestForbiddenExplainsThreeCauses(t *testing.T) {
	e := &apiError{Status: 403, Detail: "Unsupported Authentication"}
	msg := e.Error()
	for _, want := range []string{"Read and write", "作用域", "Enterprise"} {
		if !strings.Contains(msg, want) {
			t.Errorf("403 的解释里缺 %q: %s", want, msg)
		}
	}
}

// 没授权的凭证要在发请求之前就说清楚，而不是让 X 回一个空 Bearer 的 401。
func TestUnauthorizedCredentialIsExplained(t *testing.T) {
	_, err := accessToken(Cred{})
	if err == nil || !strings.Contains(err.Error(), "授权") {
		t.Errorf("要提示去点授权，得到: %v", err)
	}
}

// —— 事件源 ——

// 轮询间隔有下限：X 的读按条计费，一个手滑的 10 秒轮询就是一天几万条的账单。
func TestPollIntervalFloor(t *testing.T) {
	if got := pollInterval("10"); got != minPollSeconds*time.Second {
		t.Errorf("10 秒应当被抬到下限，得到 %s", got)
	}
	if got := pollInterval(""); got != defaultPollSeconds*time.Second {
		t.Errorf("留空应当用默认值，得到 %s", got)
	}
	if got := pollInterval("600"); got != 600*time.Second {
		t.Errorf("正常值不该被改，得到 %s", got)
	}
}

func TestCursorsRoundTrip(t *testing.T) {
	c := cursors{Mentions: "111", Query: "222"}
	if got := parseCursors(dumpCursors(c)); got != c {
		t.Errorf("游标存回凭证再读出来变了: %+v", got)
	}
	if got := parseCursors(""); got != (cursors{}) {
		t.Errorf("空游标应当是零值: %+v", got)
	}
	// 坏数据不该让事件源崩掉，当成「没有游标」即可（下一轮会重新记位置）
	if got := parseCursors("{坏的"); got != (cursors{}) {
		t.Errorf("坏游标应当退化成零值: %+v", got)
	}
}
