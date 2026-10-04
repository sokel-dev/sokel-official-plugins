package main

// 假上游。钉的是四件「不测就一定错」的事：
//   - **游标既不漏也不重**（同一秒多条、重复推送两种情形）；
//   - 首次拉取不回溯全部历史；
//   - RSS 与 Atom 的字段名不同，一个解析器要同时认；
//   - 雪球的时间戳是**毫秒**——当秒用会把 2026 年算成 56000 年，游标一跳到未来就再也收不到新内容。

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

// RSS：条目按时间**正序**给出，摘要去 HTML，图片抽出来。
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

// Atom 的字段名与 RSS 不同（entry/updated/summary/link href），一个解析器要同时认。
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

// 不是 feed 的地址要说清楚，而不是回一个空列表让人以为「没有新内容」。
func TestNonFeedIsExplained(t *testing.T) {
	ctx, u := feedURL(t, `<html><body>我是网页不是 feed</body></html>`)
	if _, err := opFetch(ctx, &FeedFetchIn{Source: "rss", Target: u}); err == nil {
		t.Fatal("网页地址应当报错")
	}
}

// —— 游标 ——

// 第二次拉同一个源：一条新的都没有 → count=0 且**游标原样不动**。
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

// **同一秒发的多条**：只按时间戳过滤会漏掉第二条——快讯类源一秒好几条是常态。
func TestSameSecondItemsAreNotLost(t *testing.T) {
	cur := cursor{T: "2026-08-19T02:00:00Z", Seen: []string{shortHash("a")}}
	items := []schema.Item{
		{ID: "a", PublishedAt: "2026-08-19T02:00:00Z"}, // 见过
		{ID: "b", PublishedAt: "2026-08-19T02:00:00Z"}, // 同一秒但没见过 → 必须收
	}
	kept, next, _ := filterNew(items, cur, 50, false)
	if len(kept) != 1 || kept[0].ID != "b" {
		t.Fatalf("同一秒的新条目被漏掉了: %+v", kept)
	}
	if !next.hasSeen("b") {
		t.Error("新游标该记住 b")
	}
}

// 见过的 id 窗口不能无限膨胀，否则游标最后有几十 KB。
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

// 首次拉取只取最近一批——接一个发了十年的源，不该把几千条一次性灌进工作流。
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

// —— 雪球 ——

func TestXueqiuTimeline(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "user_timeline"):
			gotAuth = r.Header.Get("Cookie")
			io.WriteString(w, `{"statuses":[{"id":123,"text":"<p>看好这个方向</p>","target":"/u/1/123",
				"created_at":1787000000000,"user":{"screen_name":"投研君"}}]}`)
		default: // 首页：发匿名令牌
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
	// created_at 是**毫秒**。当秒用会算出 56000 年，游标一跳到未来就再也收不到新内容。
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

// 取不到匿名令牌时要告诉人怎么办，而不是回一句「HTTP 400」。
func TestXueqiuTokenFailureIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只发 WAF cookie，不发 xq_a_token——这正是打错入口（首页）时的真实表现
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

// 转发不是原创观点，要能过滤掉。
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

// 不认识的来源要当场说清楚有哪些。
func TestUnknownSource(t *testing.T) {
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	_, err := opFetch(ctx, &FeedFetchIn{Source: "weibo"})
	if err == nil || !strings.Contains(err.Error(), "rss") {
		t.Errorf("要列出可用来源: %v", err)
	}
}

// —— 财联社 ——

// 签名是它唯一的门槛：参数按键排序 → SHA1 → MD5。**顺序错了签名就废**。
func TestCLSSign(t *testing.T) {
	q := url.Values{"os": {"web"}, "appName": {"CailianpressWeb"}, "sv": {"8.7.9"}, "name": {"telegraph"}}
	got := clsSign(q)
	// 手算一遍：Encode() 按键排序 → sha1 → md5
	s1 := sha1.Sum([]byte(q.Encode()))
	s2 := md5.Sum([]byte(hex.EncodeToString(s1[:])))
	if got != hex.EncodeToString(s2[:]) {
		t.Fatalf("签名算法不对: %s", got)
	}
	// 换个插入顺序，签名必须相同（Encode 排序保证的）
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
	// 财联社的 ctime 是**秒**（雪球那边是毫秒，别混）
	if !strings.HasPrefix(it.PublishedAt, "2026-") {
		t.Errorf("秒级时间戳没换算对: %q", it.PublishedAt)
	}
	if it.DedupKey != "cls:9001" || len(it.Tags) != 1 {
		t.Errorf("去重键/标签不对: %+v", it)
	}
}

// 签名失效时财联社回的是**空数据而不是报错**——不点破的话，
// 表现是「一直没有新内容」，而实际上一条都拉不到。
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

// —— 东方财富 ——

// 应答是 **JSONP**：外面裹着 jQueryxxx(...)。直接 json.Unmarshal 会得到
// 「invalid character 'j'」，看不出是这个原因——所以壳必须先剥。
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
	// 搜索结果的标题/摘要带高亮标签，要去掉
	if strings.Contains(it.Title, "<em>") || it.Title != "贵州茅台三季报" {
		t.Errorf("高亮标签没去掉: %q", it.Title)
	}
	// 它给的是东八区本地时间且没有时区标注：按 UTC 解会整体差 8 小时，
	// 而游标正是据此判断新旧。
	if it.PublishedAt != "2026-08-19T02:30:00Z" {
		t.Errorf("时区没按东八区换算: %q", it.PublishedAt)
	}
	if it.Author != "东方财富研究中心" {
		t.Errorf("来源媒体没带上: %q", it.Author)
	}
}

// 关键词是必填的——空关键词在发请求前拦下。
func TestEastmoneyNeedsKeyword(t *testing.T) {
	ctx := &fakeCtx{Context: context.Background(), cred: map[string]string{}}
	if _, err := opFetch(ctx, &FeedFetchIn{Source: "eastmoney_search"}); err == nil {
		t.Fatal("空关键词应当被拦下")
	}
}

// —— 金十数据 ——

// 两个固定请求头是全部门槛，少一个就被拒。
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
	// 与东财同一个坑：东八区本地时间、无时区标注
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

// 请求头失效时它回 4xx——错误信息要直接指向那两个头，而不是笼统的「HTTP 403」。
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

// 匿名令牌必须去 /hq 拿：**首页只回阿里云 WAF 的 acw_tc**（实测），打错入口的表现是
// 「一直取不到令牌」，而那会把人引向「去粘 Cookie」的弯路。
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

// 热帖榜是**套了一层壳**的：壳自己的 id 是榜单条目 id，正文/时间/作者全在
// original_status 里。不拆壳的话条目会变成一堆空标题 + 错 id（而且 id 撞不上去重）。
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

	// 快讯的 target 是**绝对地址**，不能再往前拼站点域名。
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

// 游标的外形：**短、不转义、人眼能看懂卡在哪个时间**。它要落进数据表一格并在调试台显示，
// 早先直接 dump JSON 的话满屏 \" 转义。老的 JSON 游标必须继续认——否则已经在跑的
// 工作流会把它当成「没有游标」，重头推一遍全部历史。
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
	// 老形态
	old := parseCursor(`{"t":"2026-08-19T08:35:52Z","seen":["2ceab3c4","6bb9e374"]}`)
	if old.T != c.T || len(old.Seen) != 2 {
		t.Errorf("老 JSON 游标没认出来: %+v", old)
	}
	// 空游标要回空串，不能回一个 "|"——那会让「有没有游标」这件事变得含糊。
	if s := (cursor{}).dump(); s != "" {
		t.Errorf("空游标 = %q", s)
	}
	if c := parseCursor("  "); c.T != "" || len(c.Seen) != 0 {
		t.Errorf("空串该解成空游标: %+v", c)
	}
}
