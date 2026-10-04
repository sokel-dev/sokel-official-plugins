package main

// Every fixture under testdata/ is a response captured from the real APIs (hn.algolia.com, hacker-news.firebaseio.com)
// on 2026-10-04; none is hand-written. The watch fixtures come in two consistent sets:
//
//	watch1_*  user "dang": no stories in the window, 50 recent comments, 22 replies to them (the "replies to me
//	          on other people's stories" path)
//	watch2_*  user "cautiouscat": one story with 18 comments (2 by the author), 6 own comments, 6 replies to them —
//	          some of which are also comments on the story, so the two queries overlap
//
// The fake upstream applies the created_at_i lower bound from numericFilters the way Algolia does; without that,
// dedup across rounds would be tested against a server that never returns old hits anyway.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

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

func newSource() *recordSource {
	return &recordSource{fakeCtx: &fakeCtx{Context: context.Background(), cred: map[string]string{}}}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hitsOf(t *testing.T, name string) []algoliaHit {
	t.Helper()
	var p algoliaPage
	if err := json.Unmarshal(fixture(t, name), &p); err != nil {
		t.Fatal(err)
	}
	return p.Hits
}

var sinceRe = regexp.MustCompile(`created_at_i>=?(\d+)`)

// algoliaFake serves search_by_date from fixtures chosen by route(query), honouring the created_at_i bound.
func algoliaFake(t *testing.T, route func(q map[string]string) string, seen *[]map[string]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		if seen != nil {
			*seen = append(*seen, q)
		}
		name := route(q)
		var page map[string]any
		if err := json.Unmarshal(fixture(t, name), &page); err != nil {
			t.Fatal(err)
		}
		if m := sinceRe.FindStringSubmatch(q["numericFilters"]); len(m) == 2 && strings.Contains(q["numericFilters"], ">=") {
			min, _ := strconv.ParseInt(m[1], 10, 64)
			var keep []any
			for _, h := range page["hits"].([]any) {
				if int64(h.(map[string]any)["created_at_i"].(float64)) >= min {
					keep = append(keep, h)
				}
			}
			page["hits"] = keep
		}
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		_ = json.NewEncoder(w).Encode(page)
	}))
	algoliaBase = srv.URL
	t.Cleanup(func() { algoliaBase = "https://hn.algolia.com/api/v1"; srv.Close() })
	return srv
}

func watchRoute(set, user string) func(map[string]string) string {
	return func(q map[string]string) string {
		switch tags := q["tags"]; {
		case tags == "story,author_"+user:
			if set == "watch1" {
				return "empty.json"
			}
			return set + "_my_stories.json"
		case tags == "comment,author_"+user:
			return set + "_my_comments.json"
		case strings.HasPrefix(tags, "comment,("):
			return set + "_story_comments.json"
		case tags == "comment" && strings.Contains(q["numericFilters"], "parent_id="):
			return set + "_replies.json"
		}
		return "empty.json"
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

// Replies to the user's comments on other people's stories: every reply is pushed once, flagged as a reply to me.
func TestWatchRepliesToMyComments(t *testing.T) {
	var reqs []map[string]string
	algoliaFake(t, watchRoute("watch1", "dang"), &reqs)
	src := newSource()
	s := &stream{}
	if err := pollComments(src, clientFor(""), "dang", nil, 14, s); err != nil {
		t.Fatal(err)
	}
	want := len(hitsOf(t, "watch1_replies.json"))
	got := commentEvents(src)
	if len(got) != want {
		t.Fatalf("回复我评论的 %d 条应全部推出，实推 %d 条", want, len(got))
	}
	for _, e := range got {
		if !e.IsReplyToMe || e.Author == "dang" || !strings.HasPrefix(e.URL, "https://news.ycombinator.com/item?id=") || e.StoryID == "" {
			t.Errorf("事件字段不对：%+v", e)
		}
	}
	var asked bool
	for _, q := range reqs {
		if q["tags"] == "comment" && strings.Contains(q["numericFilters"], "parent_id=") {
			asked = true
		}
	}
	if !asked {
		t.Error("没有按 parent_id 去查别人对我评论的回复")
	}
}

// Comments on the user's own story, overlapping with replies to the user's comments in that story: each comment is
// pushed exactly once, the user's own comments are not pushed, and only replies to the user are flagged.
func TestWatchMyStoryComments(t *testing.T) {
	algoliaFake(t, watchRoute("watch2", "cautiouscat"), nil)
	src := newSource()
	s := &stream{}
	if err := pollComments(src, clientFor(""), "cautiouscat", nil, 14, s); err != nil {
		t.Fatal(err)
	}
	mine := map[string]bool{}
	for _, h := range append(hitsOf(t, "watch2_my_stories.json"), hitsOf(t, "watch2_my_comments.json")...) {
		mine[h.ObjectID] = true
	}
	expect, toMe := map[string]bool{}, 0
	for _, h := range append(hitsOf(t, "watch2_story_comments.json"), hitsOf(t, "watch2_replies.json")...) {
		if h.Author != "cautiouscat" && !expect[h.ObjectID] {
			expect[h.ObjectID] = true
			if mine[idStr(h.ParentID)] {
				toMe++
			}
		}
	}
	got := commentEvents(src)
	if len(got) != len(expect) {
		t.Fatalf("两个查询去重后应推 %d 条（不含作者自己的），实推 %d 条", len(expect), len(got))
	}
	ids, flagged := map[string]int{}, 0
	for _, e := range got {
		ids[e.CommentID]++
		if e.Author == "cautiouscat" {
			t.Errorf("作者自己的评论不该推：%s", e.CommentID)
		}
		if e.IsReplyToMe {
			flagged++
		}
		if e.StoryTitle == "" {
			t.Errorf("评论 %s 缺所属帖子标题", e.CommentID)
		}
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("评论 %s 推了 %d 次", id, n)
		}
	}
	if flagged != toMe {
		t.Errorf("「回复的是我」应有 %d 条，实有 %d 条", toMe, flagged)
	}

	// Next round against the same upstream: nothing new.
	before := len(src.events)
	if err := pollComments(src, clientFor(""), "cautiouscat", nil, 14, s); err != nil {
		t.Fatal(err)
	}
	if len(src.events) != before {
		t.Errorf("第二轮没有新评论，却又推了 %d 条", len(src.events)-before)
	}
}

// The first round records the position and pushes no history; a changed keyword starts over from now.
func TestWatchFirstRoundPushesNothing(t *testing.T) {
	algoliaFake(t, func(map[string]string) string { return "keyword.json" }, nil)
	src := newSource()
	var cur cursors
	cfg := watchConfig{user: "someone", query: "postgres", days: 14}
	if errs := watchRound(src, clientFor(""), cfg, &cur, 1791130000); len(errs) > 0 || len(src.events) != 0 {
		t.Fatalf("首轮不该推历史：errs=%v events=%d", errs, len(src.events))
	}
	if cur.Comments == nil || cur.Keyword == nil || cur.Comments.T != 1791130000 || cur.Query != "postgres" {
		t.Fatalf("首轮应只记下当前位置：%+v", cur)
	}
	cur.Keyword.T = 0                 // pretend the cursor is old so the fixture's hits are new
	cfg.user, cfg.query = "", "mysql" // keyword stream only: this fake answers every query with keyword hits
	watchRound(src, clientFor(""), cfg, &cur, 1791130500)
	if len(src.events) != 0 || cur.Keyword.T != 1791130500 {
		t.Errorf("换了关键词应从现在重新开始，不回放新词的历史：events=%d cursor=%d", len(src.events), cur.Keyword.T)
	}
}

func TestWatchKeyword(t *testing.T) {
	var reqs []map[string]string
	algoliaFake(t, func(map[string]string) string { return "keyword.json" }, &reqs)
	src := newSource()
	s := &stream{}
	if err := pollKeyword(src, clientFor(""), "postgres", s); err != nil {
		t.Fatal(err)
	}
	hits := hitsOf(t, "keyword.json")
	if len(src.events) != len(hits) {
		t.Fatalf("应推 %d 条，实推 %d", len(hits), len(src.events))
	}
	for _, e := range src.events {
		ev := e.payload.(*KeywordMatchedEvent)
		if (ev.Kind != "story" && ev.Kind != "comment") || ev.MatchedQuery != "postgres" || ev.ItemID == "" || ev.StoryID == "" {
			t.Errorf("事件字段不对：%+v", ev)
		}
		if ev.Kind == "story" && ev.Title == "" {
			t.Errorf("帖子命中应有标题：%+v", ev)
		}
	}
	if q := reqs[0]; q["query"] != "postgres" || q["tags"] != "(story,comment)" {
		t.Errorf("请求参数不对：%v", q)
	}
	pollKeyword(src, clientFor(""), "postgres", s)
	if len(src.events) != len(hits) {
		t.Error("第二轮重复推了")
	}
}

func TestPlainText(t *testing.T) {
	cases := map[string]string{
		`They don&#x27;t exist.<p>Second para`: "They don't exist.\n\nSecond para",
		`see <a href="https:&#x2F;&#x2F;example.com&#x2F;a-very-long-path" rel="nofollow">https:&#x2F;&#x2F;example.com&#x2F;a-ver...</a> ok`: "see https://example.com/a-very-long-path ok",
		`<i>quoted</i> &gt; text`: "quoted > text",
		"":                        "",
	}
	for in, want := range cases {
		if got := plainText(in); got != want {
			t.Errorf("plainText(%q) = %q，want %q", in, got, want)
		}
	}
	// Real comment bodies: no tags or entities survive.
	for _, h := range hitsOf(t, "watch1_replies.json") {
		if got := plainText(h.CommentText); strings.ContainsAny(got, "<>") && strings.Contains(got, "</") || strings.Contains(got, "&#x") {
			t.Errorf("还残留 HTML：%q", got)
		}
	}
}

func TestThreadComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "tree.json"))
	}))
	defer srv.Close()
	algoliaBase = srv.URL
	defer func() { algoliaBase = "https://hn.algolia.com/api/v1" }()
	out, err := opThreadComments(&fakeCtx{Context: context.Background()}, &HnThreadCommentsIn{ID: "https://news.ycombinator.com/item?id=49954745"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count == 0 || out.Title == "" {
		t.Fatalf("应取到评论和标题：%+v", out)
	}
	seen := map[string]int{"49954745": 0}
	for _, it := range out.Items {
		d, ok := seen[it.ParentID]
		if !ok {
			t.Errorf("评论 %s 出现在它的上一级 %s 之前（应先父后子）", it.ID, it.ParentID)
		}
		if it.Depth != d+1 {
			t.Errorf("评论 %s 层级 %d，上一级层级 %d", it.ID, it.Depth, d)
		}
		seen[it.ID] = it.Depth
		if it.StoryID != "49954745" || it.Author == "" {
			t.Errorf("字段不对：%+v", it)
		}
	}
}

func TestItemGetAndList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/topstories.json"):
			_, _ = w.Write(fixture(t, "top5.json"))
		default:
			_, _ = w.Write(fixture(t, "item_story.json"))
		}
	}))
	defer srv.Close()
	firebaseBase = srv.URL
	defer func() { firebaseBase = "https://hacker-news.firebaseio.com/v0" }()
	ctx := &fakeCtx{Context: context.Background()}
	got, err := opItemGet(ctx, &HnItemGetIn{ID: "49954745"})
	if err != nil {
		t.Fatal(err)
	}
	if it := got.Item; it.Type != "story" || it.Author != "cautiouscat" || it.Score == 0 || it.Comments != 18 || it.StoryID != "49954745" {
		t.Errorf("条目字段不对：%+v", it)
	}
	if _, err := opItemGet(ctx, &HnItemGetIn{ID: "not an id"}); err == nil {
		t.Error("非 id 应报错")
	}
	list, err := opList(ctx, &HnListIn{List: "top", Limit: 3})
	if err != nil || list.Count != 3 {
		t.Fatalf("取榜单前 3 条：%+v %v", list, err)
	}
}
