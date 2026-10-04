package main

// A fake X upstream. Pins down **the things that aren't visible anywhere else**: normalization
// (author/media/retweet determination), the incremental cursor (since_id in, newest_id out, an
// empty result doesn't clobber the cursor), thread chaining and interruption, media chunk
// ordering, and the rate-limit header being an absolute timestamp rather than a number of seconds.

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

// —— Fake context ——

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

// resetCaches: me is cached per token, and this must be cleared between tests, otherwise the
// second test case gets the first fake server's account (this kind of cross-contamination is the
// hardest to track down).
func resetCaches(_ string) {
	meMu.Lock()
	meCache = map[string]rawUser{}
	meMu.Unlock()
	postFieldsParam.Store("")
}

// —— Fake upstream ——

type capture struct {
	paths   []string
	queries []url.Values
	bodies  []string
}

// fakeX matches routes by "method + path prefix"; /users/me is always built in.
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
	// xAPI is a package-level variable; point it at the fake server for the test.
	old := xAPI
	xAPI = srv.URL
	t.Cleanup(func() { xAPI = old })
	return srv
}

// —— Normalization ——

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

// Author, media, and links all live in includes/entities, unreachable from the canvas —
// normalization has to be done entirely in the plugin.
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
	// Ascending order: oldest first
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
	// Retweet detection: X doesn't give us this field; it's derived from referenced_tweets
	if first.Kind != "retweeted" || first.RefPostID != "100" {
		t.Errorf("转推没认出来: kind=%s ref=%s", first.Kind, first.RefPostID)
	}
	if first.LikeCount != 3 || first.ViewCount != 500 {
		t.Errorf("互动数没带出来: like=%d view=%d", first.LikeCount, first.ViewCount)
	}
	if second.Kind != "original" {
		t.Errorf("原创被判成了 %s", second.Kind)
	}
	// A t.co short link is useless downstream and must be expanded
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

// Cursor: what goes in is since_id, what comes back is the newest id across this batch.
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

// When a round has no new tweets, **the cursor must not be clobbered** — clobbering it would mean
// the next round fetches everything from scratch again (and pays for it all over again too).
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

// Per-page count has a floor (search: 10, mentions: 5); going below it is a flat 400 from X — an
// entirely reasonable config like "I just want 3" shouldn't fail.
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

// —— Publishing ——

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
	// everyone is X's default value; sending it explicitly gets rejected on some tiers
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

// Thread: each tweet must reply to the previous one, otherwise what gets posted is N unrelated tweets.
func TestPostThreadChains(t *testing.T) {
	var cap capture
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/users/me") {
			io.WriteString(w, `{"data":{"id":"9001","username":"acme_bot"}}`)
			return
		}
		// Only record the post bodies: letting the account lookup call (used to build a link)
		// mix in would throw off all the indices below.
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
	// The 2nd and 3rd tweets' in_reply_to must be the previous one's id
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

// A mid-thread failure: **doesn't roll back**, but the ids already posted must appear in the
// error, otherwise there's no way to know where it broke or where to pick it back up.
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

// —— Engagement ——

// These four pairs hang off **the authorized account itself**; the id in the path is me's, not
// the target's.
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

// —— Media ——

// Chunking: chunk indices must be contiguous starting at 0 — get one wrong and the whole file is
// ruined (X only reports the error at FINALIZE).
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

// If media_id is returned before transcoding finishes, posting downstream gets "media not found",
// which looks like a wrong id.
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

// The purpose determines media_category; get it wrong and it's only rejected at the publish step
// — by which point a tens-of-megabytes video has already finished uploading.
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

// —— Errors ——

// x-rate-limit-reset is an **absolute epoch-seconds timestamp**. Treating it as "wait this many
// seconds" would wait until next year.
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

// A 403 is the hardest error to debug on X: the raw message never says whether it's permissions,
// scope, or tier.
func TestForbiddenExplainsThreeCauses(t *testing.T) {
	e := &apiError{Status: 403, Detail: "Unsupported Authentication"}
	msg := e.Error()
	for _, want := range []string{"Read and write", "作用域", "Enterprise"} {
		if !strings.Contains(msg, want) {
			t.Errorf("403 的解释里缺 %q: %s", want, msg)
		}
	}
}

// An unauthorized credential should be spelled out before the request is even sent, rather than
// letting X return a 401 for an empty Bearer token.
func TestUnauthorizedCredentialIsExplained(t *testing.T) {
	_, err := accessToken(Cred{})
	if err == nil || !strings.Contains(err.Error(), "授权") {
		t.Errorf("要提示去点授权，得到: %v", err)
	}
}

// —— Event source ——

// The polling interval has a floor: X charges per read, and an accidental 10-second poll turns
// into a bill for tens of thousands of calls a day.
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
	// Bad data must not crash the event source — treating it as "no cursor" is fine (the next
	// round will record the position fresh)
	if got := parseCursors("{坏的"); got != (cursors{}) {
		t.Errorf("坏游标应当退化成零值: %+v", got)
	}
}
