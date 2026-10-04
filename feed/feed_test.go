package main

// Fake upstreams. These pin down four things that are guaranteed to break without a test:
//   - **The cursor neither drops nor repeats items** (same-second multiples and duplicate
//     deliveries);
//   - the first fetch doesn't backfill the whole history;
//   - RSS and Atom use different field names, and one parser has to recognize both;
//   - Xueqiu's timestamp is in **milliseconds** — treating it as seconds would turn 2026 into
//     the year 56000, jumping the cursor into the future and cutting off all new content.

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (c *fakeCtx) Upload(name, mime string, data []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (c *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) { panic("不用") }
func (c *fakeCtx) Fetch(*plugin.File) ([]byte, error)                           { return nil, nil }

const rss20 = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>某财经站</title>
<item><title>第二条</title><link>https://e.com/2</link><guid>g2</guid>
  <description>&lt;p&gt;正文二 &lt;img src="https://e.com/a.png"&gt;&lt;/p&gt;</description>
  <pubDate>Tue, 19 Aug 2026 02:00:00 +0000</pubDate><category>市场</category></item>
<item><title>第一条</title><link>https://e.com/1</link><guid>g1</guid>
  <description>正文一</description><pubDate>Tue, 19 Aug 2026 01:00:00 +0000</pubDate></item>
</channel></rss>`

const atom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>某博客</title>
<entry><title>A</title><link href="https://b.com/a"/><id>tag:a</id>
  <summary>摘要 A</summary><updated>2026-08-19T03:00:00Z</updated>
  <author><name>老王</name></author></entry>
</feed>`

func serve(t *testing.T, body string, ct string) *fakeCtx {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"target_base": srv.URL}}
}

func feedURL(t *testing.T, body string) (*fakeCtx, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &fakeCtx{Context: context.Background(), cred: map[string]string{}}, srv.URL + "/feed.xml"
}

// RSS: items come back **in ascending time order**, the summary strips HTML, images are extracted.
func TestRSSParsing(t *testing.T) {
	ctx, u := feedURL(t, rss20)
	out, err := opFetch(ctx, &FeedFetchIn{Source: "rss", Target: u, MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Fatalf("应当 2 条，得到 %d", out.Count)
	}
	if out.Items[0].Title != "第一条" {
		t.Errorf("应当按时间正序（老的在前），得到 %q", out.Items[0].Title)
	}
	second := out.Items[1]
	if second.PublishedAt != "2026-08-19T02:00:00Z" {
		t.Errorf("时间没归一成 RFC3339: %q", second.PublishedAt)
	}
	if strings.Contains(second.Summary, "<") {
		t.Errorf("摘要该是纯文本: %q", second.Summary)
	}
	if second.ContentHTML == "" {
		t.Error("正文 HTML 不该丢")
	}
	if len(second.Images) != 1 || second.Images[0] != "https://e.com/a.png" {
		t.Errorf("图片没抽出来: %v", second.Images)
	}
	if len(second.Tags) != 1 || second.Tags[0] != "市场" {
		t.Errorf("标签没带上: %v", second.Tags)
	}
	if second.DedupKey == "" {
		t.Error("缺 dedup_key")
	}
}

// Atom's field names differ from RSS's (entry/updated/summary/link href); one parser has to
// recognize both.
func TestAtomParsing(t *testing.T) {
	ctx, u := feedURL(t, atom)
	out, err := opFetch(ctx, &FeedFetchIn{Source: "rss", Target: u, MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 {
		t.Fatalf("应当 1 条，得到 %d", out.Count)
	}
	it := out.Items[0]
	if it.URL != "https://b.com/a" {
		t.Errorf("Atom 的 link 在 href 属性里: %q", it.URL)
	}
	if it.Author != "老王" || it.PublishedAt != "2026-08-19T03:00:00Z" {
		t.Errorf("作者/时间没解出来: %+v", it)
	}
}

// A URL that isn't a feed should be reported clearly, instead of returning an empty list that
// makes people think "no new content."
func TestNonFeedIsExplained(t *testing.T) {
	ctx, u := feedURL(t, `<html><body>我是网页不是 feed</body></html>`)
	if _, err := opFetch(ctx, &FeedFetchIn{Source: "rss", Target: u}); err == nil {
		t.Fatal("网页地址应当报错")
	}
}

// —— Cursor ——

// Fetching the same source a second time: no new items -> count=0 and **the cursor is unchanged**.
func TestCursorSkipsSeen(t *testing.T) {
	ctx, u := feedURL(t, rss20)
	first, err := opFetch(ctx, &FeedFetchIn{Source: "rss", Target: u, MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	second, err := opFetch(ctx, &FeedFetchIn{Source: "rss", Target: u, Cursor: first.NextCursor, MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if second.Count != 0 {
		t.Errorf("同样的内容不该再推一遍，得到 %d 条", second.Count)
	}
}

// **Multiple items published in the same second**: filtering by timestamp alone would drop the
// second item — wire-style feeds commonly emit several items per second.
func TestSameSecondItemsAreNotLost(t *testing.T) {
	cur := cursor{T: "2026-08-19T02:00:00Z", Seen: []string{shortHash("a")}}
	items := []schema.Item{
		{ID: "a", PublishedAt: "2026-08-19T02:00:00Z"}, // already seen
		{ID: "b", PublishedAt: "2026-08-19T02:00:00Z"}, // same second but not seen -> must keep
	}
	kept, next, _ := filterNew(items, cur, 50, false)
	if len(kept) != 1 || kept[0].ID != "b" {
		t.Fatalf("同一秒的新条目被漏掉了: %+v", kept)
	}
	if !next.hasSeen("b") {
		t.Error("新游标该记住 b")
	}
}

// The window of seen ids must not grow without bound, or the cursor ends up tens of KB in size.
func TestSeenWindowIsBounded(t *testing.T) {
	cur := cursor{}
	for i := 0; i < 200; i++ {
		items := []schema.Item{{ID: string(rune('a'+i%26)) + strings.Repeat("x", i), PublishedAt: ""}}
		_, cur, _ = filterNew(items, cur, 50, false)
	}
	if len(cur.Seen) > seenWindow {
		t.Errorf("游标里的 id 窗口涨到了 %d 个", len(cur.Seen))
	}
}

// The first fetch only takes the most recent batch — connecting a feed that's been publishing
// for ten years shouldn't dump thousands of items into the workflow at once.
func TestFirstRunDoesNotBacklog(t *testing.T) {
	items := make([]schema.Item, 100)
	for i := range items {
		items[i] = schema.Item{ID: strings.Repeat("i", i+1), PublishedAt: ""}
	}
	kept, _, hasMore := filterNew(items, cursor{}, 10, true)
	if len(kept) != 10 {
		t.Errorf("首次应当只给 10 条，得到 %d", len(kept))
	}
	if hasMore {
		t.Error("首次不该说还有更多——那会让下游立刻再拉一次，把历史全灌进来")
	}
}

// —— Xueqiu ——

func TestXueqiuTimeline(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "user_timeline"):
			gotAuth = r.Header.Get("Cookie")
			io.WriteString(w, `{"statuses":[{"id":123,"text":"<p>看好这个方向</p>","target":"/u/1/123",
				"created_at":1787000000000,"user":{"screen_name":"投研君"}}]}`)
		default: // homepage: issues the anonymous token
			http.SetCookie(w, &http.Cookie{Name: "xq_a_token", Value: "anon"})
			io.WriteString(w, "<html></html>")
		}
	}))
	defer srv.Close()
	xueqiuSite, xueqiuAPI = srv.URL, srv.URL
	tokMu.Lock()
	tokCache = map[string]tokenEntry{}
	tokMu.Unlock()

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	out, err := opFetch(ctx, &FeedFetchIn{Source: "xueqiu_user", Target: "https://xueqiu.com/u/1", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAuth, "xq_a_token=anon") {
		t.Errorf("没带上自取的匿名令牌: %q", gotAuth)
	}
	if out.Count != 1 {
		t.Fatalf("应当 1 条: %+v", out)
	}
	it := out.Items[0]
	// created_at is in **milliseconds**. Treating it as seconds would compute the year
	// 56000, jumping the cursor into the future and cutting off all new content.
	if !strings.HasPrefix(it.PublishedAt, "2026-") {
		t.Errorf("毫秒时间戳没换算对: %q", it.PublishedAt)
	}
	if it.Title != "看好这个方向" {
		t.Errorf("没标题时该用摘要顶上: %q", it.Title)
	}
	if it.URL != srv.URL+"/u/1/123" || it.DedupKey != "xueqiu:123" {
		t.Errorf("链接/去重键不对: %+v", it)
	}
}

// When the anonymous token can't be obtained, the error should tell people what to do, not just
// say "HTTP 400".
func TestXueqiuTokenFailureIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only issues the WAF cookie, not xq_a_token — this is exactly what happens when
		// hitting the wrong entry point (the homepage)
		http.SetCookie(w, &http.Cookie{Name: "acw_tc", Value: "waf"})
		io.WriteString(w, "<html>没有令牌</html>")
	}))
	defer srv.Close()
	xueqiuSite, xueqiuAPI = srv.URL, srv.URL
	tokMu.Lock()
	tokCache = map[string]tokenEntry{}
	tokMu.Unlock()

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	_, err := opFetch(ctx, &FeedFetchIn{Source: "xueqiu_hots"})
	if err == nil || !strings.Contains(err.Error(), "Cookie") {
		t.Errorf("要告诉人去粘一条 Cookie: %v", err)
	}
}

// A repost isn't an original take and should be filterable.
func TestSkipReposts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "hot/list") {
			io.WriteString(w, `{"list":[{"id":1,"text":"原创"},{"id":2,"text":"转的","retweeted_status":{"id":9}}]}`)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "xq_a_token", Value: "anon"})
		io.WriteString(w, "<html></html>")
	}))
	defer srv.Close()
	xueqiuSite, xueqiuAPI = srv.URL, srv.URL
	tokMu.Lock()
	tokCache = map[string]tokenEntry{}
	tokMu.Unlock()

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	out, err := opFetch(ctx, &FeedFetchIn{Source: "xueqiu_hots", SkipReposts: true, MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || out.Items[0].DedupKey != "xueqiu:1" {
		t.Errorf("转发没被过滤掉: %+v", out.Items)
	}
}

// An unrecognized source should immediately list which ones are available.
func TestUnknownSource(t *testing.T) {
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	_, err := opFetch(ctx, &FeedFetchIn{Source: "weibo"})
	if err == nil || !strings.Contains(err.Error(), "rss") {
		t.Errorf("要列出可用来源: %v", err)
	}
}

// —— Cailianpress (CLS) ——

// The signature is its only gate: params sorted by key -> SHA1 -> MD5. **Wrong order ruins the
// signature.**
func TestCLSSign(t *testing.T) {
	q := url.Values{"os": {"web"}, "appName": {"CailianpressWeb"}, "sv": {"8.7.9"}, "name": {"telegraph"}}
	got := clsSign(q)
	// Compute it by hand: Encode() sorts by key -> sha1 -> md5
	s1 := sha1.Sum([]byte(q.Encode()))
	s2 := md5.Sum([]byte(hex.EncodeToString(s1[:])))
	if got != hex.EncodeToString(s2[:]) {
		t.Fatalf("签名算法不对: %s", got)
	}
	// With a different insertion order, the signature must be the same (guaranteed by Encode's sorting)
	q2 := url.Values{"sv": {"8.7.9"}, "name": {"telegraph"}, "appName": {"CailianpressWeb"}, "os": {"web"}}
	if clsSign(q2) != got {
		t.Error("参数插入顺序影响了签名——没按键排序")
	}
}

func TestCLSTelegraph(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		io.WriteString(w, `{"data":{"roll_data":[
			{"id":9001,"title":"","content":"央行开展 5000 亿逆回购","shareurl":"https://www.cls.cn/detail/9001",
			 "ctime":1787000000,"subjects":[{"subject_name":"货币政策"}]}]}}`)
	}))
	defer srv.Close()
	clsSite = srv.URL

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	out, err := opFetch(ctx, &FeedFetchIn{Source: "cls_telegraph", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("sign") == "" || gotQuery.Get("name") != "telegraph" {
		t.Errorf("公共参数/签名没带上: %v", gotQuery)
	}
	if out.Count != 1 {
		t.Fatalf("应当 1 条: %+v", out)
	}
	it := out.Items[0]
	if it.Title != "央行开展 5000 亿逆回购" {
		t.Errorf("电报常常没有标题，该用正文顶上: %q", it.Title)
	}
	// CLS's ctime is in **seconds** (Xueqiu uses milliseconds — don't mix them up)
	if !strings.HasPrefix(it.PublishedAt, "2026-") {
		t.Errorf("秒级时间戳没换算对: %q", it.PublishedAt)
	}
	if it.DedupKey != "cls:9001" || len(it.Tags) != 1 {
		t.Errorf("去重键/标签不对: %+v", it)
	}
}

// When the signature is invalid, CLS responds with **empty data instead of an error** — without
// calling that out, it looks like "no new content ever," when in fact nothing is being fetched
// at all.
func TestCLSEmptyIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errno":1001,"error":"sign error","data":{"roll_data":[]}}`)
	}))
	defer srv.Close()
	clsSite = srv.URL

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	_, err := opFetch(ctx, &FeedFetchIn{Source: "cls_telegraph"})
	if err == nil || !strings.Contains(err.Error(), "签名") {
		t.Errorf("空数据要说成可能的签名问题: %v", err)
	}
}

// —— Eastmoney ——

// The response is **JSONP**: wrapped in jQueryxxx(...). A direct json.Unmarshal would fail with
// "invalid character 'j'", which doesn't make the real cause obvious — so the shell must be
// stripped first.
func TestEastmoneyJSONP(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		io.WriteString(w, `jQuery12345_678({"result":{"cmsArticleWebOld":[
			{"title":"贵州<em>茅台</em>三季报","content":"营收同比 <em>增长</em> 15%","url":"https://finance.eastmoney.com/a/1.html",
			 "date":"2026-08-19 10:30:00","mediaName":"东方财富研究中心"}]}});`)
	}))
	defer srv.Close()
	emSearchAPI = srv.URL

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	out, err := opFetch(ctx, &FeedFetchIn{Source: "eastmoney_search", Target: "茅台", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("cb") == "" || !strings.Contains(gotQuery.Get("param"), "茅台") {
		t.Errorf("请求参数不对: %v", gotQuery)
	}
	if out.Count != 1 {
		t.Fatalf("应当 1 条: %+v", out)
	}
	it := out.Items[0]
	// Titles/summaries in search results carry highlight tags, which should be stripped
	if strings.Contains(it.Title, "<em>") || it.Title != "贵州茅台三季报" {
		t.Errorf("高亮标签没去掉: %q", it.Title)
	}
	// What it returns is UTC+8 local time with no timezone marker: parsing it as UTC would
	// be off by 8 hours across the board, and the cursor uses this value to judge new vs. old.
	if it.PublishedAt != "2026-08-19T02:30:00Z" {
		t.Errorf("时区没按东八区换算: %q", it.PublishedAt)
	}
	if it.Author != "东方财富研究中心" {
		t.Errorf("来源媒体没带上: %q", it.Author)
	}
}

// A keyword is required — an empty keyword is rejected before sending the request.
func TestEastmoneyNeedsKeyword(t *testing.T) {
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	if _, err := opFetch(ctx, &FeedFetchIn{Source: "eastmoney_search"}); err == nil {
		t.Fatal("空关键词应当被拦下")
	}
}

// —— Jin10 ——

// The two fixed headers are the whole gate; missing either gets the request rejected.
func TestJin10Headers(t *testing.T) {
	var gotHdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHdr = r.Header
		io.WriteString(w, `{"data":[{"id":"20260819103000","time":"2026-08-19 10:30:00","important":1,
			"data":{"content":"央行开展 5000 亿逆回购","link":"https://www.jin10.com/x/1"}}]}`)
	}))
	defer srv.Close()
	jin10API = srv.URL

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	out, err := opFetch(ctx, &FeedFetchIn{Source: "jin10", MaxItems: 50})
	if err != nil {
		t.Fatal(err)
	}
	if gotHdr.Get("x-app-id") == "" || gotHdr.Get("x-version") == "" {
		t.Errorf("两个门槛头没带上: %v", gotHdr)
	}
	if out.Count != 1 {
		t.Fatalf("应当 1 条: %+v", out)
	}
	it := out.Items[0]
	// Same pitfall as Eastmoney: UTC+8 local time, no timezone marker
	if it.PublishedAt != "2026-08-19T02:30:00Z" {
		t.Errorf("时区没按东八区换算: %q", it.PublishedAt)
	}
	if len(it.Tags) != 1 || it.Tags[0] != "重要" {
		t.Errorf("important 标记没带出来: %v", it.Tags)
	}
	if it.DedupKey != "jin10:20260819103000" {
		t.Errorf("去重键: %q", it.DedupKey)
	}
}

// When the headers are invalid it responds with 4xx — the error message should point directly
// at those two headers, not just say "HTTP 403".
func TestJin10RejectedIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, `{"message":"forbidden"}`)
	}))
	defer srv.Close()
	jin10API = srv.URL

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	_, err := opFetch(ctx, &FeedFetchIn{Source: "jin10"})
	if err == nil || !strings.Contains(err.Error(), "x-app-id") {
		t.Errorf("要指出是请求头的问题: %v", err)
	}
}

// The anonymous token must be obtained from /hq: **the homepage only returns Alibaba Cloud
// WAF's acw_tc** (confirmed in practice), and hitting the wrong entry point shows up as "the
// token never arrives," which sends people down the detour of "go paste a cookie."
func TestXueqiuTokenComesFromHQ(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "hot/list"):
			io.WriteString(w, `{"list":[{"id":1,"text":"x"}]}`)
		case strings.HasSuffix(r.URL.Path, "/hq"):
			http.SetCookie(w, &http.Cookie{Name: "xq_a_token", Value: "anon"})
			io.WriteString(w, "<html></html>")
		default:
			http.SetCookie(w, &http.Cookie{Name: "acw_tc", Value: "waf"})
			io.WriteString(w, "<html></html>")
		}
	}))
	defer srv.Close()
	xueqiuSite, xueqiuAPI = srv.URL, srv.URL
	tokMu.Lock()
	tokCache = map[string]tokenEntry{}
	tokMu.Unlock()

	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	if _, err := opFetch(ctx, &FeedFetchIn{Source: "xueqiu_hots", MaxItems: 10}); err != nil {
		t.Fatal(err)
	}
	var hitHQ bool
	for _, p := range paths {
		if strings.HasSuffix(p, "/hq") {
			hitHQ = true
		}
	}
	if !hitHQ {
		t.Errorf("没去 /hq 取令牌: %v", paths)
	}
}

// The hot-posts list is **wrapped in a shell**: the shell's own id is the list-entry id, while
// the body/time/author all live inside original_status. Without unwrapping it, items end up
// with empty titles and wrong ids (and ids won't collide for dedup purposes).
func TestXueqiuHotsUnwrapsShell(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "hot/listV2"):
			io.WriteString(w, `{"next_max_id":9,"items":[{"id":953522,"data":null,
				"original_status":{"id":405671880,"text":"真正的正文","created_at":1787123599000,
				"target":"/3501797505/405671880","user":{"screen_name":"老王"}}}]}`)
		case strings.Contains(r.URL.Path, "livenews"):
			io.WriteString(w, `{"items":[{"id":4838439,"text":"快讯一条",
				"target":"http://xueqiu.com/5124430882/405685723","created_at":1787127935000}]}`)
		case strings.HasSuffix(r.URL.Path, "/hq"):
			http.SetCookie(w, &http.Cookie{Name: "xq_a_token", Value: "anon"})
			io.WriteString(w, "<html></html>")
		default:
			io.WriteString(w, "<html></html>")
		}
	}))
	defer srv.Close()
	xueqiuSite, xueqiuAPI = srv.URL, srv.URL
	tokMu.Lock()
	tokCache = map[string]tokenEntry{}
	tokMu.Unlock()
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}

	out, err := opFetch(ctx, &FeedFetchIn{Source: "xueqiu_hots", MaxItems: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("热帖条数 = %d", len(out.Items))
	}
	if got := out.Items[0].ID; got != "405671880" {
		t.Errorf("拿的是壳的 id 而不是帖子 id: %q", got)
	}
	if out.Items[0].Author != "老王" || !strings.Contains(out.Items[0].Summary, "真正的正文") {
		t.Errorf("壳没拆开: %+v", out.Items[0])
	}

	// A flash-news target is an **absolute URL**, which must not be concatenated with the
	// site domain again.
	out, err = opFetch(ctx, &FeedFetchIn{Source: "xueqiu_livenews", MaxItems: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("快讯条数 = %d", len(out.Items))
	}
	if got := out.Items[0].URL; got != "https://xueqiu.com/5124430882/405685723" {
		t.Errorf("绝对 target 被重复拼接: %q", got)
	}
}

// The cursor's shape: **short, no escaping, a human can tell at a glance where it's stuck**. It
// has to land in a data table cell and be shown on the debug console, and dumping JSON directly
// used to fill the screen with \" escapes. Old JSON cursors must still be recognized —
// otherwise a workflow already running would treat it as "no cursor" and push the entire
// history again from scratch.
func TestCursorWireFormat(t *testing.T) {
	c := cursor{T: "2026-08-19T08:35:52Z", Seen: []string{"2ceab3c4", "6bb9e374"}}
	got := c.dump()
	if want := "2026-08-19T08:35:52Z|2ceab3c4,6bb9e374"; got != want {
		t.Errorf("游标外形 = %q, 想要 %q", got, want)
	}
	if strings.ContainsAny(got, `"\{}`) {
		t.Errorf("游标里还有要转义的字符: %q", got)
	}
	if back := parseCursor(got); back.T != c.T || len(back.Seen) != 2 || back.Seen[1] != "6bb9e374" {
		t.Errorf("往返对不上: %+v", back)
	}
	// old shape
	old := parseCursor(`{"t":"2026-08-19T08:35:52Z","seen":["2ceab3c4","6bb9e374"]}`)
	if old.T != c.T || len(old.Seen) != 2 {
		t.Errorf("老 JSON 游标没认出来: %+v", old)
	}
	// An empty cursor should return an empty string, not "|" — that would make "is there a
	// cursor" ambiguous.
	if s := (cursor{}).dump(); s != "" {
		t.Errorf("空游标 = %q", s)
	}
	if c := parseCursor("  "); c.T != "" || len(c.Seen) != 0 {
		t.Errorf("空串该解成空游标: %+v", c)
	}
}
