package main

// Every fixture under testdata/ is a Reddit public feed captured on 2026-10-07 (one request a minute, which is all
// Reddit allows without login); none is hand-written:
//
//	user_submitted.xml   /user/importcjj/submitted.rss   3 posts, all from 2019–2020 (outside any watch window)
//	sub_new.xml          /r/golang/new.rss                25 newest posts
//	post_comments.xml    /comments/1wzth3z/.rss           the post (by Iwantmytshirtback) then 3 comments, one by the author
//	search_posts.xml     /search.rss?q=sokel              22 posts + 2 subreddit entries (t5_, which the parser drops)
//	subs_search.xml      /r/selfhosted+golang/search.rss?q=workflow&restrict_sr=1   25 posts, 09-28 … 10-07
//
// The fake upstream routes by path and records requests; the pacer's window is shrunk so the suite does not wait a
// minute per request.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

type pushed struct {
	event, id string
	payload   any
}

type recordSource struct {
	*fakeCtx
	mu     sync.Mutex
	events []pushed
}

func (r *recordSource) Trigger(event, id string, payload any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, pushed{event, id, payload})
	return nil
}
func (r *recordSource) UpdateCredential(map[string]string) error { return nil }
func (r *recordSource) ReportStatus(string, string)              {}

func newCtx() *fakeCtx { return &fakeCtx{Context: context.Background(), cred: map[string]string{}} }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// redditFake serves the fixtures by path. status, when set, can override the answer for a path.
func redditFake(t *testing.T, seen *[]string, status func(path string) int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if seen != nil {
			*seen = append(*seen, p+"?"+r.URL.RawQuery)
		}
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("请求没带插件自己的 User-Agent：%q", r.Header.Get("User-Agent"))
		}
		if status != nil {
			if code := status(p); code != 0 {
				w.Header().Set("x-ratelimit-reset", "0")
				w.WriteHeader(code)
				return
			}
		}
		var name string
		switch {
		case p == "/search.rss":
			name = "search_posts.xml"
		case p == "/r/selfhosted+golang/search.rss" && r.URL.Query().Get("restrict_sr") == "1":
			name = "subs_search.xml"
		case p == "/r/golang/new.rss":
			name = "sub_new.xml"
		case p == "/comments/1wzth3z/.rss":
			name = "post_comments.xml"
		case strings.HasPrefix(p, "/comments/"):
			w.WriteHeader(http.StatusNotFound)
			return
		case p == "/user/importcjj/submitted.rss" || p == "/user/Iwantmytshirtback/submitted.rss":
			name = "user_submitted.xml"
		case strings.HasPrefix(p, "/user/"):
			w.WriteHeader(http.StatusNotFound)
			return
		default:
			t.Fatalf("未知路径 %s", p)
		}
		w.Header().Set("Content-Type", "application/atom+xml; charset=UTF-8")
		_, _ = w.Write(fixture(t, name))
	}))
	base = srv.URL
	pace.window = 0
	pace.next = time.Time{}
	t.Cleanup(func() { base = "https://www.reddit.com"; pace.window = 61 * time.Second; srv.Close() })
}

func TestParseFeed(t *testing.T) {
	items, err := parseFeed(fixture(t, "post_comments.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 || items[0].Kind != "post" || items[0].Name != "t3_1wzth3z" {
		t.Fatalf("帖子评论 feed 应是 1 帖 + 3 评论、帖子在前：%+v", items)
	}
	c := items[2]
	if c.Kind != "comment" || c.Name != "t1_peem68b" || c.PostID != "1wzth3z" || c.Author != "Diligent_Intention49" ||
		c.AuthorURL != "https://www.reddit.com/user/Diligent_Intention49" || c.Subreddit != "golang" ||
		!strings.HasSuffix(c.URL, "/comments/1wzth3z/v_basic_semaphore_syntax_help/peem68b/") {
		t.Errorf("评论字段没解对：%+v", c)
	}
	if c.Created != 1791370751 { // 2026-10-07T10:59:11Z: comments carry only <updated>
		t.Errorf("评论没有 published，应取 updated：%d", c.Created)
	}
	if items[0].Created != 1791370536 { // the post's <published>
		t.Errorf("帖子应取 published：%d", items[0].Created)
	}
	text := plainText(items[1].HTML)
	if text != "Screenshot: https://imgur.com/a/W6gmX5R" {
		t.Errorf("纯文本应去标签、链接留地址：%q", text)
	}
	if got := plainText(c.HTML); !strings.Contains(got, "you're calling release") || strings.Contains(got, "&#39;") || strings.Contains(got, "<") {
		t.Errorf("纯文本应还原转义、去标签：%q", got)
	}
	// Search results carry subreddit entries (t5_), which are neither posts nor comments.
	items, err = parseFeed(fixture(t, "search_posts.xml"))
	if err != nil || len(items) != 22 {
		t.Fatalf("搜索结果应只留 22 个帖子（丢掉 2 个版块条目）：%d %v", len(items), err)
	}
	for _, it := range items {
		if it.Kind != "post" {
			t.Errorf("混进了 %s", it.Name)
		}
	}
	if _, err := parseFeed([]byte("<html>blocked</html>")); err == nil {
		t.Error("不是 feed 应报错")
	}
}

func TestOps(t *testing.T) {
	var reqs []string
	redditFake(t, &reqs, nil)
	ctx := newCtx()

	s, err := opSearch(ctx, &RedditSearchIn{Query: "workflow", Subreddits: "selfhosted,golang"})
	if err != nil || s.Count != 25 || !strings.Contains(reqs[0], "/r/selfhosted+golang/search.rss?") || !strings.Contains(reqs[0], "restrict_sr=1") || !strings.Contains(reqs[0], "sort=new") || !strings.Contains(reqs[0], "t=week") {
		t.Fatalf("版块内搜索：count=%d err=%v req=%v", s.Count, err, reqs)
	}
	s, err = opSearch(ctx, &RedditSearchIn{Query: "sokel"})
	if err != nil || s.Count != 22 || !strings.HasPrefix(reqs[1], "/search.rss?") {
		t.Fatalf("全站搜索：count=%d err=%v req=%v", s.Count, err, reqs[1])
	}
	if _, err := opSearch(ctx, &RedditSearchIn{Query: "x", Subreddits: "r/bad name"}); err == nil {
		t.Error("版块写错应报错")
	}

	n, err := opSubredditNew(ctx, &RedditSubredditNewIn{Subreddits: "r/golang"})
	if err != nil || n.Count != 25 || n.Items[0].Name != "t3_1wzth3z" || n.Items[0].Author != "Iwantmytshirtback" {
		t.Fatalf("版块最新：count=%d err=%v first=%+v", n.Count, err, n.Items[0])
	}

	pc, err := opPostComments(ctx, &RedditPostCommentsIn{Post: "https://www.reddit.com/r/golang/comments/1wzth3z/v_basic_semaphore_syntax_help/"})
	if err != nil || pc.Count != 3 || pc.Post.Name != "t3_1wzth3z" || pc.Items[0].Kind != "comment" || pc.Items[0].PostID != "1wzth3z" {
		t.Fatalf("帖子评论：%+v err=%v", pc, err)
	}
	if _, err := opPostComments(ctx, &RedditPostCommentsIn{Post: "t3_zzzzzz"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("不存在的帖子应报 404：%v", err)
	}

	u, err := opUserPosts(ctx, &RedditUserPostsIn{Username: "u/importcjj"})
	if err != nil || u.Count != 3 || u.Items[0].Kind != "post" {
		t.Fatalf("用户帖子：count=%d err=%v", u.Count, err)
	}

	h, _ := opHealthCheck(&fakeCtx{Context: context.Background(), cred: map[string]string{"watch_user": "importcjj"}}, &HealthCheckIn{})
	if !h.OK || !strings.Contains(h.Message, "3 个帖子") {
		t.Errorf("健康检查应认出用户：%+v", h)
	}
	h, _ = opHealthCheck(&fakeCtx{Context: context.Background(), cred: map[string]string{"watch_user": "no_such_user_zz"}}, &HealthCheckIn{})
	if h.OK || !strings.Contains(h.Message, "404") {
		t.Errorf("不存在的用户应报不可用：%+v", h)
	}
}

// Requests are serialised a window apart; one 429 is waited out and retried, a second one is an error.
func TestPacer(t *testing.T) {
	var reqs []string
	hits := map[string]int{}
	redditFake(t, &reqs, func(p string) int {
		hits[p]++
		switch {
		case p == "/r/golang/new.rss" && hits[p] == 1:
			return http.StatusTooManyRequests // first try limited, retry succeeds
		case p == "/search.rss":
			return http.StatusTooManyRequests // always limited
		}
		return 0
	})
	pace.window = 40 * time.Millisecond
	ctx := newCtx()
	start := time.Now()
	if _, err := opSubredditNew(ctx, &RedditSubredditNewIn{Subreddits: "golang"}); err != nil {
		t.Fatalf("一次 429 后重试应成功：%v", err)
	}
	if _, err := opUserPosts(ctx, &RedditUserPostsIn{Username: "importcjj"}); err != nil {
		t.Fatal(err)
	}
	if n := len(reqs); n != 3 {
		t.Fatalf("应发 3 个请求（429 + 重试 + 1），实发 %d", n)
	}
	if d := time.Since(start); d < 2*40*time.Millisecond {
		t.Errorf("三个请求应按窗口间隔串行（≥80ms），实际 %s", d)
	}
	if _, err := opSearch(ctx, &RedditSearchIn{Query: "x"}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("连续两次 429 应报限流错误：%v", err)
	}
	// Two plain successes back to back must still be a window apart: the slot is claimed before the request goes out.
	pace.next = time.Time{}
	t0 := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := opUserPosts(ctx, &RedditUserPostsIn{Username: "importcjj"}); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(t0); d < 40*time.Millisecond {
		t.Errorf("连续两个成功请求应隔一个窗口，实际 %s", d)
	}
}

func commentEvents(r *recordSource) []CommentReceivedEvent {
	var out []CommentReceivedEvent
	for _, e := range r.events {
		if e.event == "comment_received" {
			out = append(out, *e.payload.(*CommentReceivedEvent))
		}
	}
	return out
}

func keywordEvents(r *recordSource) []KeywordMatchedEvent {
	var out []KeywordMatchedEvent
	for _, e := range r.events {
		if e.event == "keyword_matched" {
			out = append(out, *e.payload.(*KeywordMatchedEvent))
		}
	}
	return out
}

const now = 1791373200 // 2026-10-07T11:40:00Z, after every comment in post_comments.xml and most posts in subs_search.xml

// First round: remember now, push nothing. With the cursor set back: every comment on the watched post newer than
// it is pushed once, the watched user's own comments are not, old posts of the user are outside the window, and
// a repeat round pushes nothing more.
func TestWatchComments(t *testing.T) {
	var reqs []string
	redditFake(t, &reqs, nil)
	src := &recordSource{fakeCtx: newCtx()}
	cred := Cred{WatchUser: "Iwantmytshirtback", WatchItems: "https://www.reddit.com/r/golang/comments/1wzth3z/x/, t3_1wzth3z"}
	cfg, err := configOf(cred)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.items) != 1 {
		t.Fatalf("同一帖子两种写法应只算一个：%v", cfg.items)
	}
	var cur cursors
	if errs := watchRound(src, cred, cfg, &cur, now); len(errs) > 0 || len(src.events) != 0 || cur.Comments == nil || cur.Comments.T != now {
		t.Fatalf("首轮不该推历史：errs=%v events=%d cur=%+v", errs, len(src.events), cur.Comments)
	}
	if len(reqs) != 0 {
		t.Fatalf("首轮只记位置，不该发请求：%v", reqs)
	}

	cur.Comments.T = 1791370506 // just before the post (10:55:36Z); the three comments follow it
	if errs := watchRound(src, cred, cfg, &cur, now); len(errs) > 0 {
		t.Fatal(errs)
	}
	all, _ := parseFeed(fixture(t, "post_comments.xml"))
	want := 0
	for _, it := range all {
		if it.Kind == "comment" && it.Author != "Iwantmytshirtback" {
			want++
		}
	}
	got := commentEvents(src)
	if want == 0 || len(got) != want {
		t.Fatalf("评论去掉作者自己的应推 %d 条，实推 %d：%+v", want, len(got), got)
	}
	for _, e := range got {
		if e.Author == "Iwantmytshirtback" || e.PostID != "1wzth3z" || e.PostTitle != "V basic semaphore syntax help" || e.Subreddit != "golang" || e.Text == "" || !strings.HasPrefix(e.URL, "https://www.reddit.com/r/golang/comments/1wzth3z/") || e.CreatedAt == "" || !strings.HasPrefix(e.CommentID, "t1_") {
			t.Errorf("事件字段不对：%+v", e)
		}
	}
	// One request for the user's posts (all from 2020: outside the 14-day window) and one for the listed post.
	if len(reqs) != 2 || !strings.HasPrefix(reqs[0], "/user/Iwantmytshirtback/submitted.rss") || !strings.HasPrefix(reqs[1], "/comments/1wzth3z/.rss") {
		t.Fatalf("应只查用户帖子 + 列出的 1 个帖子，实际 %v", reqs)
	}
	if cur.Comments.T != 1791370751 {
		t.Errorf("游标应推进到最新评论的时间：%d", cur.Comments.T)
	}

	before := len(src.events)
	if errs := watchRound(src, cred, cfg, &cur, now); len(errs) > 0 || len(src.events) != before {
		t.Errorf("同样的上游再来一轮不该再推：errs=%v extra=%d", errs, len(src.events)-before)
	}
}

// The user's own recent posts are watched: with "now" inside the window of the fixture's old posts, each of the
// newest ones (capped per round) costs one comments request, and the listed posts come first.
func TestWatchedPostsWindow(t *testing.T) {
	var reqs []string
	redditFake(t, &reqs, nil)
	cfg := watchConfig{user: "importcjj", days: 14, items: []string{"1wzth3z"}}
	ids, err := watchedPosts(context.Background(), Cred{}, cfg, 1586300000) // 2020-04-07T23:33Z: the newest post is 13 h old, the others months
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "1wzth3z" || ids[1] != "fwi48j" {
		t.Fatalf("应是列出的帖子 + 窗口内的 1 个帖子：%v", ids)
	}
	cfg.days = 365
	ids, _ = watchedPosts(context.Background(), Cred{}, cfg, 1586300000)
	if len(ids) != 4 {
		t.Fatalf("窗口放宽到一年应有 1 + 3 个：%v", ids)
	}
	cfg.items = []string{"a1", "a2", "a3", "a4", "a5", "a6"}
	ids, _ = watchedPosts(context.Background(), Cred{}, cfg, 1586300000)
	if len(ids) != maxPostsPerRound {
		t.Fatalf("每轮最多 %d 个帖子，实际 %d", maxPostsPerRound, len(ids))
	}
}

// Keyword stream: first round records the position; then every post newer than the cursor is pushed once,
// in the subreddits asked for; changing the query starts over from now.
func TestWatchKeyword(t *testing.T) {
	var reqs []string
	redditFake(t, &reqs, nil)
	src := &recordSource{fakeCtx: newCtx()}
	cred := Cred{WatchQuery: "workflow", WatchSubreddits: "selfhosted+golang"}
	cfg, _ := configOf(cred)
	var cur cursors
	watchRound(src, cred, cfg, &cur, now)
	if len(src.events) != 0 || cur.Keyword == nil || cur.Query != "workflow\x00selfhosted+golang" {
		t.Fatalf("首轮不该推历史：%d %+v", len(src.events), cur)
	}
	cur.Keyword.T = 1791331200 // 2026-10-07T00:00:00Z
	if errs := watchRound(src, cred, cfg, &cur, now); len(errs) > 0 {
		t.Fatal(errs)
	}
	items, _ := parseFeed(fixture(t, "subs_search.xml"))
	want := 0
	for _, it := range items {
		if it.Created >= 1791331200-lookbackSeconds {
			want++
		}
	}
	got := keywordEvents(src)
	if want == 0 || len(got) != want {
		t.Fatalf("游标之后的帖子应全部推出：应 %d 实 %d", want, len(got))
	}
	for _, e := range got {
		if e.MatchedQuery != "workflow" || e.Title == "" || e.URL == "" || (e.Subreddit != "selfhosted" && e.Subreddit != "golang") || !strings.HasPrefix(e.Name, "t3_") {
			t.Errorf("事件字段不对：%+v", e)
		}
	}
	if !strings.Contains(reqs[0], "/r/selfhosted+golang/search.rss?") || !strings.Contains(reqs[0], "t=day") {
		t.Errorf("关键词应在指定版块里按天搜：%s", reqs[0])
	}
	before := len(src.events)
	watchRound(src, cred, cfg, &cur, now)
	if len(src.events) != before {
		t.Errorf("再来一轮不该重推：%d", len(src.events)-before)
	}
	cred.WatchQuery = "sokel"
	cfg, _ = configOf(cred)
	watchRound(src, cred, cfg, &cur, now+10)
	if len(src.events) != before || cur.Keyword.T != now+10 || cur.Query != "sokel\x00selfhosted+golang" {
		t.Errorf("换了关键词应从现在重新开始、不回放新词的历史：events=%d cur=%+v", len(src.events)-before, cur)
	}
}

func TestConfig(t *testing.T) {
	if cfg, err := configOf(Cred{}); err != nil || cfg.watching() {
		t.Errorf("空凭证 = 不监听：%+v %v", cfg, err)
	}
	if _, err := configOf(Cred{WatchUser: "u/has space"}); err == nil {
		t.Error("用户名不合法应报错")
	}
	if _, err := configOf(Cred{WatchSubreddits: "golang selfhosted"}); err == nil {
		t.Error("版块写法不对应报错")
	}
	if cfg, _ := configOf(Cred{WatchUser: "u/importcjj", WatchDays: "x", WatchSubreddits: "r/golang,selfhosted"}); cfg.user != "importcjj" || cfg.days != defaultWatchDays || cfg.subs != "golang+selfhosted" {
		t.Errorf("规整后：%+v", cfg)
	}
	if pollInterval("10") != minPollSeconds*time.Second || pollInterval("") != defaultPollSeconds*time.Second {
		t.Error("轮询间隔的下限/默认值不对")
	}
}
