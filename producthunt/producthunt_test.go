package main

// Every fixture under testdata/ (except schema.graphql, from github.com/producthunt/producthunt-api) is a response
// captured from api.producthunt.com on 2026-10-07 with a developer token, through the exact queries the plugin sends
// (capture_test.go). What the real API taught, each guarded below:
//   - a page is 20 whatever `first` says (first: 50 came back as 20 with hasNextPage) -> cursor pagination;
//   - a query over 500 000 complexity is refused: 20 comments × 20 replies was 1 925 608, × 5 fits;
//   - every user on a comment is id "0" / "[REDACTED]"; only isViewer is honest -> that is how "mine" is known;
//   - the makers' pinned launch comment leads page one even in NEWEST order.
// The fixtures hold no viewer-written comment (the account has none), so the isViewer tests patch one in.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/vektah/gqlparser/v2/validator"
)

func TestQueriesMatchSchema(t *testing.T) {
	src, err := os.ReadFile("testdata/schema.graphql")
	if err != nil {
		t.Fatal(err)
	}
	schema, gerr := gqlparser.LoadSchema(&ast.Source{Name: "schema.graphql", Input: string(src)})
	if gerr != nil {
		t.Fatalf("官方 schema 读不进来：%v", gerr)
	}
	check := func(name, q string) {
		doc, perr := parser.ParseQuery(&ast.Source{Name: name, Input: q})
		if perr != nil {
			t.Fatalf("%s 语法错：%v", name, perr)
		}
		if errs := validator.Validate(schema, doc); len(errs) > 0 {
			t.Errorf("%s 不符合 Product Hunt 的 schema：%v", name, errs)
		}
	}
	check("qPost", qPost)
	check("qComments", qComments)
	check("qLeaderboard", qLeaderboard)
	check("qViewer", qViewer)
	// Teeth: a misspelt field must fail the same check.
	bad := strings.Replace(qPost, "votesCount", "voteCount", 1)
	doc, _ := parser.ParseQuery(&ast.Source{Name: "bad", Input: bad})
	if errs := validator.Validate(schema, doc); len(errs) == 0 {
		t.Fatal("拼错的字段也过了校验，这个测试没牙")
	}
}

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

func newCtx() *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"developer_token": "t"}}
}

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type gqlReq struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// phFake answers each query from the fixture captured for it: comments and leaderboard by page cursor, post by slug.
// patch, when set, edits the data before it is sent. Requests are recorded.
func phFake(t *testing.T, patch func(req gqlReq, data map[string]any), seen *[]gqlReq) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if seen != nil {
			*seen = append(*seen, req)
		}
		var data map[string]any
		switch {
		case strings.HasPrefix(req.Query, "query Viewer"):
			data = fixture(t, "viewer.json")
		case strings.HasPrefix(req.Query, "query Post("):
			if req.Variables["slug"] == "rill-browser" {
				data = fixture(t, "post.json")
			} else {
				data = fixture(t, "post_missing.json")
			}
		case strings.HasPrefix(req.Query, "query Comments"):
			if req.Variables["slug"] != "rill-browser" {
				data = fixture(t, "post_missing.json")
			} else if req.Variables["after"] == "MjA" {
				data = fixture(t, "comments_p2.json")
			} else if req.Variables["after"] == nil {
				data = fixture(t, "comments_p1.json")
			} else {
				t.Fatalf("未知的评论游标 %v", req.Variables["after"])
			}
		case strings.HasPrefix(req.Query, "query Leaderboard"):
			if req.Variables["cursor"] == "MjA" {
				data = fixture(t, "leaderboard_p2.json")
			} else if req.Variables["cursor"] == nil {
				data = fixture(t, "leaderboard_p1.json")
			} else {
				t.Fatalf("未知的榜单游标 %v", req.Variables["cursor"])
			}
		default:
			t.Fatalf("未知查询：%s", req.Query[:40])
		}
		if patch != nil {
			patch(req, data)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	endpoint = srv.URL
	t.Cleanup(func() { endpoint = "https://api.producthunt.com/v2/api/graphql"; srv.Close() })
}

// commentNodes walks a comments fixture: top-level comment nodes and their reply nodes.
func commentNodes(data map[string]any, fn func(node map[string]any, parent map[string]any)) {
	edges := data["post"].(map[string]any)["comments"].(map[string]any)["edges"].([]any)
	for _, e := range edges {
		n := e.(map[string]any)["node"].(map[string]any)
		fn(n, nil)
		for _, r := range n["replies"].(map[string]any)["edges"].([]any) {
			fn(r.(map[string]any)["node"].(map[string]any), n)
		}
	}
}

func TestPostGet(t *testing.T) {
	phFake(t, nil, nil)
	out, err := opPostGet(newCtx(), &PhPostGetIn{Post: "https://www.producthunt.com/posts/rill-browser"})
	if err != nil {
		t.Fatal(err)
	}
	p := out.Post
	if p.Slug != "rill-browser" || p.Name != "Rill Browser" || p.VotesCount != 413 || p.CommentsCount != 67 || p.Makers != "williamgaoharvard,iris_tu" || p.FeaturedAt == "" || !strings.HasPrefix(p.URL, "https://www.producthunt.com/products/") {
		t.Errorf("产品字段没对上：%+v", p)
	}
	if _, err := opPostGet(newCtx(), &PhPostGetIn{Post: "no-such-product-zzz-404"}); err == nil || !strings.Contains(err.Error(), "no-such-product-zzz-404") {
		t.Errorf("不存在的 slug 应报错并点名：%v", err)
	}
	if _, err := opPostGet(newCtx(), &PhPostGetIn{Post: "Not A Slug!"}); err == nil {
		t.Error("认不出的输入应报错")
	}
}

// Pages are 20 whatever is asked (measured); a limit beyond one page follows the cursor, a limit within it does not.
func TestCommentsPaged(t *testing.T) {
	var reqs []gqlReq
	phFake(t, nil, &reqs)
	p1, p2 := fixture(t, "comments_p1.json"), fixture(t, "comments_p2.json")
	top, all := 0, 0
	for _, f := range []map[string]any{p1, p2} {
		commentNodes(f, func(_ map[string]any, parent map[string]any) {
			all++
			if parent == nil {
				top++
			}
		})
	}
	out, err := opComments(newCtx(), &PhCommentsIn{Post: "rill-browser", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[0].Variables["first"] != float64(pageSize) || reqs[1].Variables["after"] != "MjA" {
		t.Fatalf("limit 50 应按 20 一页取两页（第二页带游标 MjA），实际请求：%+v", reqs)
	}
	if out.Count != all || len(out.Items) != all || out.Total != 28 {
		t.Fatalf("两页合并应得 %d 条（含回复）、总数 28，实得 count=%d items=%d total=%d", all, out.Count, len(out.Items), out.Total)
	}
	if top != 28 {
		t.Fatalf("夹具两页顶层评论应为 28 条，实为 %d", top)
	}
	replies := 0
	for _, c := range out.Items {
		if c.Username != "" {
			t.Fatalf("评论人身份被 PH 隐去，不该把 [REDACTED] 当名字输出：%+v", c)
		}
		if c.ParentID != "" {
			replies++
		}
	}
	if replies == 0 {
		t.Fatal("夹具里有回复，输出里却没有带 parent_id 的")
	}
	// Default limit: one page, one request.
	reqs = nil
	out, err = opComments(newCtx(), &PhCommentsIn{Post: "rill-browser"})
	if err != nil || len(reqs) != 1 || len(out.Items) != 44 {
		t.Fatalf("默认只取一页：reqs=%d items=%d err=%v", len(reqs), len(out.Items), err)
	}
}

func TestLeaderboardPaged(t *testing.T) {
	var reqs []gqlReq
	phFake(t, nil, &reqs)
	out, err := opLeaderboard(newCtx(), &PhLeaderboardIn{Date: "2026-10-06", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 || reqs[1].Variables["cursor"] != "MjA" || out.Count != 25 || len(out.Items) != 25 {
		t.Fatalf("limit 25 应取两页截到 25 条：reqs=%d count=%d", len(reqs), out.Count)
	}
	if reqs[0].Variables["after"] != "2026-10-06T00:00:00-07:00" || reqs[0].Variables["before"] != "2026-10-07T00:00:00-07:00" || reqs[0].Variables["order"] != "RANKING" {
		t.Errorf("日期应按太平洋时间切一天：%+v", reqs[0].Variables)
	}
	if out.Items[0].Slug == "" || out.Items[0].VotesCount == 0 {
		t.Errorf("榜单条目没解出来：%+v", out.Items[0])
	}
	if _, err := opLeaderboard(newCtx(), &PhLeaderboardIn{Date: "10/06/2026"}); err == nil {
		t.Error("日期格式不对应报错")
	}
}

func TestHealthCheck(t *testing.T) {
	phFake(t, nil, nil)
	out, err := opHealthCheck(newCtx(), &HealthCheckIn{})
	if err != nil || !out.OK || out.Username != "jiaju_chen" {
		t.Fatalf("健康检查应读出账号：%+v %v", out, err)
	}
	if out, _ := opHealthCheck(&fakeCtx{Context: context.Background(), cred: map[string]string{}}, &HealthCheckIn{}); out.OK {
		t.Error("没有 token 不该报可用")
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

// mineInFixtures marks, in the served fixtures, top-level comment 5925539 and reply 5926893 (one of its two
// replies) as written by the viewer. So reply 5926234 is a reply to me, 5926893 is my own and must not be pushed.
func mineInFixtures(_ gqlReq, data map[string]any) {
	if data["post"] == nil || data["post"].(map[string]any)["comments"] == nil {
		return
	}
	commentNodes(data, func(n map[string]any, _ map[string]any) {
		if id := n["id"]; id == "5925539" || id == "5926893" {
			n["user"].(map[string]any)["isViewer"] = true
		}
	})
}

// First round: remember now, push no history. Then a round with the cursor set back pushes every comment newer
// than it exactly once (minus my own), flags replies to me, follows to page two only while pages can still hold
// newer comments, and a repeat round pushes nothing.
func TestWatch(t *testing.T) {
	var reqs []gqlReq
	phFake(t, mineInFixtures, &reqs)
	src := &recordSource{fakeCtx: newCtx()}
	src.cred["watch_posts"] = "https://www.producthunt.com/posts/rill-browser"
	cred := Cred{DeveloperToken: "t", WatchPosts: src.cred["watch_posts"], WatchMyPosts: "on"}

	// Cursor set between page one's oldest chronological comment (09:42:53Z) and page two's newest (09:30:03Z)
	// would stop at one page; set it inside page two instead so the second page is needed and the third is not.
	since := parseTime("2026-10-06T09:00:00Z") // between p2's 08:59:00Z and 09:23:07Z
	cur := &cursor{T: since + lookbackSeconds, Seen: map[string]int64{}}
	if err := pollRound(src, cred, true, slugList(cred.WatchPosts), cur); err != nil {
		t.Fatal(err)
	}
	var want, toMe int
	for _, f := range []string{"comments_p1.json", "comments_p2.json"} {
		data := fixture(t, f)
		mineInFixtures(gqlReq{}, data)
		commentNodes(data, func(n map[string]any, parent map[string]any) {
			if parseTime(n["createdAt"].(string)) < since || n["user"].(map[string]any)["isViewer"] == true {
				return
			}
			want++
			if parent != nil && parent["user"].(map[string]any)["isViewer"] == true {
				toMe++
			}
		})
	}
	got := commentEvents(src)
	if len(got) != want {
		t.Fatalf("游标之后的评论应全部推出（不含我自己的）：应 %d 条，实 %d 条", want, len(got))
	}
	var comments, viewer int
	for _, r := range reqs {
		switch {
		case strings.HasPrefix(r.Query, "query Comments"):
			comments++
		case strings.HasPrefix(r.Query, "query Viewer"):
			viewer++
		}
	}
	if comments != 2 || viewer != 1 {
		t.Fatalf("应查一次账号、取两页评论（第二页的末尾已早于游标），实际 viewer=%d comments=%d", viewer, comments)
	}
	ids, flagged, mineSeen := map[string]int{}, 0, false
	for _, e := range got {
		ids[e.CommentID]++
		if e.IsReplyToMe {
			flagged++
			if e.CommentID != "5926234" {
				t.Errorf("只有 5926234 回复了我的评论，却标了 %s", e.CommentID)
			}
		}
		if e.CommentID == "5926893" {
			mineSeen = true
		}
		if e.PostName != "Rill Browser" || e.PostID != "1270095" || e.URL == "" || e.Body == "" || e.CreatedAt == "" {
			t.Errorf("事件字段不全：%+v", e)
		}
		if e.Username != "" {
			t.Errorf("PH 隐去的评论人不该带名字：%q", e.Username)
		}
		// The seen set is pruned to the lookback window behind the newest comment after each round.
		if _, ok := cur.Seen[e.CommentID]; ok != (parseTime(e.CreatedAt) >= cur.T-lookbackSeconds) {
			t.Errorf("已见集合应只保留最新评论前 %ds 内的：%s (%s) in=%v", lookbackSeconds, e.CommentID, e.CreatedAt, ok)
		}
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("评论 %s 推了 %d 次", id, n)
		}
	}
	if flagged != toMe || toMe != 1 {
		t.Errorf("「回复的是我」应有 %d 条（夹具按 1 条设计），实有 %d 条", toMe, flagged)
	}
	if mineSeen {
		t.Error("我自己写的回复 5926893 不该推")
	}
	if cur.T != parseTime("2026-10-07T06:39:53Z") {
		t.Errorf("游标应推进到最新一条评论的时间，实为 %d", cur.T)
	}

	// Same upstream again: nothing new, and the newest page alone suffices.
	before, reqsBefore := len(src.events), len(reqs)
	if err := pollRound(src, cred, true, slugList(cred.WatchPosts), cur); err != nil {
		t.Fatal(err)
	}
	if len(src.events) != before {
		t.Errorf("第二轮没有新评论，却又推了 %d 条", len(src.events)-before)
	}
	if n := len(reqs) - reqsBefore; n != 2 {
		t.Errorf("第二轮应只查账号 + 一页评论（第一页末尾已早于游标），实发 %d 个请求", n)
	}
}

// A post listed twice (by slug and by address) is polled once; a slug Product Hunt does not know fails the round
// without touching the cursor.
func TestWatchSlugsAndMissing(t *testing.T) {
	var reqs []gqlReq
	phFake(t, nil, &reqs)
	src := &recordSource{fakeCtx: newCtx()}
	cur := &cursor{T: parseTime("2026-10-07T00:00:00Z"), Seen: map[string]int64{}}
	if err := pollRound(src, Cred{DeveloperToken: "t"}, false, slugList("rill-browser, https://www.producthunt.com/posts/rill-browser"), cur); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("同一产品写了两遍应只查一次，实发 %d 个请求", len(reqs))
	}
	if len(commentEvents(src)) == 0 {
		t.Fatal("游标之后有评论却没推")
	}
	before, _ := json.Marshal(cur)
	if err := pollRound(src, Cred{DeveloperToken: "t"}, false, []string{"no-such-product-zzz-404"}, cur); err == nil || !strings.Contains(err.Error(), "no-such-product-zzz-404") {
		t.Fatalf("不存在的产品应让这一轮失败并点名：%v", err)
	}
	if after, _ := json.Marshal(cur); string(after) != string(before) {
		t.Error("失败的一轮不该动游标")
	}
}
