package main

// These tests cover the places in this plugin that **make decisions**, not plain HTTP
// forwarding. Each one corresponds to a pitfall called out in the schema package's top
// comment or in code comments — what they have in common is "fails silently, not loudly".

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

// fakeSourceCtx records Trigger calls so tests can assert "which event fired, with what payload".
type fakeSourceCtx struct {
	*fakeCtx
	fired map[string]any // event id → payload
	ids   map[string]string
	order []string // "event name|event_id", in fire order — this is the only place count and order are visible
}

func newSource(cred map[string]string) *fakeSourceCtx {
	return &fakeSourceCtx{fakeCtx: newFake(cred), fired: map[string]any{}, ids: map[string]string{}}
}

func (f *fakeSourceCtx) Trigger(event, eventID string, payload any) error {
	f.fired[event] = payload
	f.ids[event] = eventID
	f.order = append(f.order, event+"|"+eventID)
	return nil
}
func (f *fakeSourceCtx) UpdateCredential(map[string]string) error { return nil }
func (f *fakeSourceCtx) ReportStatus(string, string)              {}

// —— Signature verification: a security boundary — get it wrong and forged events get through ——

func sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// GitHub signs the **raw request body** with HMAC-SHA256, unlike GitLab's plain-text
// token comparison. Copying GitLab's approach will never verify successfully — and the
// worse failure mode is "let it through when verification fails".
func TestWebhookSignature(t *testing.T) {
	body := []byte(`{"zen":"x"}`)
	cases := []struct {
		name     string
		secret   string
		header   string
		wantPass bool
	}{
		{"签名正确", "s3cret", sign("s3cret", body), true},
		{"签名错误", "s3cret", sign("wrong", body), false},
		{"配了 secret 但请求没带签名", "s3cret", "", false},
		{"凭证没配 secret 就跳过校验", "", "", true},
		// Case and prefix must both be strict: sha1= is GitHub's old signature header, no longer secure enough
		{"拿 sha1 的形状冒充", "s3cret", "sha1=" + hex.EncodeToString([]byte("x")), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := newSource(map[string]string{"webhook_secret": c.secret})
			req := &sokel.WebhookRequest{
				Body:    body,
				Headers: map[string]string{"X-Hub-Signature-256": c.header},
			}
			_, ok := verifySignature(ctx, req)
			if ok != c.wantPass {
				t.Fatalf("放行 = %v, want %v", ok, c.wantPass)
			}
		})
	}
}

// The signature must be computed over the **raw bytes**. Re-encoded JSON (key order and
// spacing can both change) produces a signature that won't match, and the symptom is
// "every webhook gets 401" — easy to mistake for a wrong secret.
func TestWebhookSignatureUsesRawBody(t *testing.T) {
	raw := []byte(`{"b":1,  "a":2}`) // deliberately left with extra spaces and unordered keys
	ctx := newSource(map[string]string{"webhook_secret": "k"})
	req := &sokel.WebhookRequest{
		Body:    raw,
		Headers: map[string]string{"X-Hub-Signature-256": sign("k", raw)},
	}
	if _, ok := verifySignature(ctx, req); !ok {
		t.Fatal("原始字节签名应通过——说明实现里对 body 做了重新编码")
	}
}

// —— /issues also returns PRs: the most classic GitHub API pitfall ——

func TestIssuesListDropsPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"真 Issue"},
			{"number":2,"title":"其实是 PR","pull_request":{"url":"x"}},
			{"number":3,"title":"另一条 Issue"}
		]`))
	}))
	defer srv.Close()
	ctx := newFake(map[string]string{"base_url": srv.URL, "token": "t"})

	out, err := opIssuesList(ctx, &IssuesListIn{Repo: "o/r"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 || out.DroppedPrs != 1 {
		t.Fatalf("默认应剔掉 PR：count=%d dropped=%d", out.Count, out.DroppedPrs)
	}
	for _, is := range out.Issues {
		if is.IsPR {
			t.Errorf("剔完还剩 PR：%+v", is)
		}
	}
	// Turning on the switch should keep them, and mark is_pr truthfully
	out2, err := opIssuesList(ctx, &IssuesListIn{Repo: "o/r", IncludePrs: true})
	if err != nil {
		t.Fatal(err)
	}
	if out2.Count != 3 || out2.DroppedPrs != 0 {
		t.Fatalf("打开 include_prs 应保留全部：count=%d dropped=%d", out2.Count, out2.DroppedPrs)
	}
	if !out2.Issues[1].IsPR {
		t.Error("带 pull_request 键的那条应标 is_pr")
	}
}

// —— webhook dispatch: three branches that misroute work if left unchecked ——

// closed does not mean merged: closing without merging is also "closed". Getting this wrong
// means "publish after PR merge" fires spuriously on every PR close.
func TestWebhookClosedNotMerged(t *testing.T) {
	for _, c := range []struct{ merged, wantFire bool }{{true, true}, {false, false}} {
		ctx := newSource(nil)
		body, _ := json.Marshal(map[string]any{
			"action":     "closed",
			"repository": map[string]any{"full_name": "o/r"},
			"pull_request": map[string]any{
				"number": 7, "title": "t", "merged": c.merged,
			},
		})
		resp := handleWebhook(ctx, &sokel.WebhookRequest{
			Body:    body,
			Headers: map[string]string{"X-GitHub-Event": "pull_request", "X-GitHub-Delivery": "d1"},
		})
		if resp.Status != 200 {
			t.Fatalf("应回 200，得 %d", resp.Status)
		}
		_, fired := ctx.fired["pr_merged"]
		if fired != c.wantFire {
			t.Errorf("merged=%v 时触发 pr_merged = %v，want %v", c.merged, fired, c.wantFire)
		}
	}
}

// Comments on a PR go through the same issue_comment event — without distinguishing them,
// "commenting on a PR" would dispatch to the Issue path instead.
func TestWebhookCommentRoutesByTarget(t *testing.T) {
	mk := func(isPR bool) map[string]any {
		iss := map[string]any{"number": 3, "title": "t"}
		if isPR {
			iss["pull_request"] = map[string]any{"url": "x"}
		}
		return map[string]any{
			"action": "created", "repository": map[string]any{"full_name": "o/r"},
			"issue": iss,
			"comment": map[string]any{
				"id": 99, "body": "/deploy", "user": map[string]any{"login": "alice", "type": "User"},
				"author_association": "MEMBER",
			},
		}
	}
	for _, c := range []struct {
		isPR          bool
		want, notWant string
	}{
		{true, "pr_commented", "issue_commented"},
		{false, "issue_commented", "pr_commented"},
	} {
		ctx := newSource(nil)
		body, _ := json.Marshal(mk(c.isPR))
		handleWebhook(ctx, &sokel.WebhookRequest{
			Body:    body,
			Headers: map[string]string{"X-GitHub-Event": "issue_comment", "X-GitHub-Delivery": "d2"},
		})
		if _, ok := ctx.fired[c.want]; !ok {
			t.Errorf("isPR=%v 应触发 %s，实际触发了 %v", c.isPR, c.want, keysOf(ctx.fired))
		}
		if _, ok := ctx.fired[c.notWant]; ok {
			t.Errorf("isPR=%v 不该触发 %s", c.isPR, c.notWant)
		}
	}
}

// A bot's own comments must be recognizable — otherwise ChatOps can wake itself up into an infinite loop.
func TestBotDetection(t *testing.T) {
	cases := []struct {
		user map[string]any
		want bool
	}{
		{map[string]any{"login": "alice", "type": "User"}, false},
		{map[string]any{"login": "dependabot[bot]", "type": "Bot"}, true},
		// Old payloads missing type fall back to the name suffix
		{map[string]any{"login": "renovate[bot]"}, true},
		{map[string]any{"login": "botanist", "type": "User"}, false}, // having "bot" in the name doesn't count
	}
	for _, c := range cases {
		if got := isBot(c.user); got != c.want {
			t.Errorf("isBot(%v) = %v, want %v", c.user, got, c.want)
		}
	}
}

// event_id uses X-GitHub-Delivery: GitHub reuses the same value on redelivery, which is
// exactly the dedup semantics the platform needs.
func TestWebhookUsesDeliveryAsEventID(t *testing.T) {
	ctx := newSource(nil)
	body, _ := json.Marshal(map[string]any{
		"action": "opened", "repository": map[string]any{"full_name": "o/r"},
		"issue": map[string]any{"number": 1, "title": "t"},
	})
	handleWebhook(ctx, &sokel.WebhookRequest{
		Body:    body,
		Headers: map[string]string{"X-GitHub-Event": "issues", "X-GitHub-Delivery": "uuid-1"},
	})
	if got := ctx.ids["issue_opened"]; got != "wh:uuid-1" {
		t.Errorf("event_id = %q，应基于投递 id", got)
	}
}

// GitHub sends a ping immediately when a webhook config is saved — if we don't return 200, the config page shows it as failed.
func TestWebhookPing(t *testing.T) {
	ctx := newSource(nil)
	resp := handleWebhook(ctx, &sokel.WebhookRequest{
		Body:    []byte(`{"zen":"Keep it logically awesome."}`),
		Headers: map[string]string{"X-GitHub-Event": "ping", "X-GitHub-Delivery": "d"},
	})
	if resp.Status != 200 {
		t.Fatalf("ping 应回 200，得 %d", resp.Status)
	}
}

// Unrecognized event types must also get a 200: a non-2xx makes GitHub retry repeatedly, and after enough failures it disables the webhook.
func TestWebhookUnknownEventStill200(t *testing.T) {
	ctx := newSource(nil)
	resp := handleWebhook(ctx, &sokel.WebhookRequest{
		Body:    []byte(`{"action":"created"}`),
		Headers: map[string]string{"X-GitHub-Event": "star", "X-GitHub-Delivery": "d"},
	})
	if resp.Status != 200 {
		t.Fatalf("未知事件应回 200，得 %d", resp.Status)
	}
	if len(ctx.fired) != 0 {
		t.Errorf("未知事件不该触发任何东西：%v", keysOf(ctx.fired))
	}
}

// —— URL assembly: GHES's GraphQL endpoint does not follow REST's /api/v3 ——

func TestBaseURLs(t *testing.T) {
	cases := []struct{ base, rest, gql string }{
		{"", "https://api.github.com", "https://api.github.com/graphql"},
		{"https://gh.example.com", "https://gh.example.com/api/v3", "https://gh.example.com/api/graphql"},
		{"https://gh.example.com/", "https://gh.example.com/api/v3", "https://gh.example.com/api/graphql"},
		// It's common for users to include /api/v3 in the base URL themselves — without dedup this becomes /api/v3/api/v3
		{"https://gh.example.com/api/v3", "https://gh.example.com/api/v3", "https://gh.example.com/api/graphql"},
	}
	for _, c := range cases {
		cred := Cred{BaseURL: c.base}
		if got := restBase(cred); got != c.rest {
			t.Errorf("restBase(%q) = %q, want %q", c.base, got, c.rest)
		}
		if got := graphQLBase(cred); got != c.gql {
			t.Errorf("graphQLBase(%q) = %q, want %q", c.base, got, c.gql)
		}
	}
}

func TestRepoSplit(t *testing.T) {
	ok := []struct{ in, want string }{
		{"owner/repo", "/repos/owner/repo"},
		{"  owner/repo  ", "/repos/owner/repo"},
		{"https://github.com/owner/repo", "/repos/owner/repo"},
		{"https://github.com/owner/repo.git", "/repos/owner/repo"},
	}
	for _, c := range ok {
		got, err := repoPath(c.in)
		if err != nil || got != c.want {
			t.Errorf("repoPath(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "repo", "a/b/c", "/", "owner/"} {
		if _, err := repoPath(bad); err == nil {
			t.Errorf("repoPath(%q) 应报错", bad)
		}
	}
}

// GitHub doesn't return a total count; pagination can only be determined from the Link header.
func TestHasNext(t *testing.T) {
	h := http.Header{}
	if hasNext(h) {
		t.Error("没有 Link 头时不该说还有下一页")
	}
	h.Set("Link", `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=9>; rel="last"`)
	if !hasNext(h) {
		t.Error("有 rel=next 就是还有下一页")
	}
	h.Set("Link", `<https://api.github.com/x?page=1>; rel="prev", <https://api.github.com/x?page=1>; rel="first"`)
	if hasNext(h) {
		t.Error("只有 prev/first 时已经是最后一页")
	}
}

// 403 has two meanings; conflating them sends people chasing "insufficient permission" by
// repeatedly checking scopes, when really they just need to wait a few minutes.
func TestRateLimitVsPermission(t *testing.T) {
	limited := http.Header{}
	limited.Set("X-RateLimit-Remaining", "0")
	err := ghErr(403, []byte(`{"message":"API rate limit exceeded"}`), "/x", limited)
	if !strings.Contains(err.Error(), "限流") {
		t.Errorf("余量为 0 的 403 要说成限流：%v", err)
	}
	perm := http.Header{}
	perm.Set("X-RateLimit-Remaining", "4999")
	err = ghErr(403, []byte(`{"message":"Resource not accessible"}`), "/x", perm)
	if strings.Contains(err.Error(), "限流") {
		t.Errorf("有余量的 403 是权限问题，不该说成限流：%v", err)
	}
	// The 404 message must spell out "no permission also returns 404", otherwise users keep re-checking the path
	err = ghErr(404, []byte(`{"message":"Not Found"}`), "/repos/o/r", http.Header{})
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "权限") {
		t.Errorf("404 要提示可能是权限问题：%v", err)
	}
}

// The commit status description has a 140-character cap, truncated by character, not by
// byte — truncating by byte would cut multi-byte characters into garbage.
func TestClipRunes(t *testing.T) {
	s := strings.Repeat("中", 200)
	got := clipRunes(s, 140)
	if n := len([]rune(got)); n != 140 {
		t.Errorf("截断后 %d 个字符，want 140", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("截断要留个省略号")
	}
	if short := clipRunes("短", 140); short != "短" {
		t.Errorf("没超长不该动：%q", short)
	}
}

// Polling cursor: activity feed ids are increasing numeric strings that may exceed
// int64 — compare by length, then lexicographically.
func TestNewerThan(t *testing.T) {
	cases := []struct {
		id, last string
		want     bool
	}{
		{"100", "", true},
		{"101", "100", true},
		{"100", "100", false},
		{"99", "100", false},
		{"1000", "999", true}, // more digits means newer
		{"999", "1000", false},
	}
	for _, c := range cases {
		if got := newerThan(c.id, c.last); got != c.want {
			t.Errorf("newerThan(%q,%q) = %v, want %v", c.id, c.last, got, c.want)
		}
	}
}

// GitHub returns collaborator permissions as a table of booleans; taking the highest tier is the role people actually want to see.
func TestHighestPermission(t *testing.T) {
	all := map[string]any{"admin": true, "maintain": true, "push": true, "pull": true}
	if got := highestPermission(all); got != "admin" {
		t.Errorf("= %q, want admin", got)
	}
	readonly := map[string]any{"admin": false, "push": false, "pull": true}
	if got := highestPermission(readonly); got != "pull" {
		t.Errorf("= %q, want pull", got)
	}
	if got := highestPermission(nil); got != "" {
		t.Errorf("空表应给空串，得 %q", got)
	}
}

// Writing a file must include the blob sha, or it's a 422. This test checks whether we probe first.
func TestFileWriteFetchesShaFirst(t *testing.T) {
	var sawGet bool
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sawGet = true
			_, _ = w.Write([]byte(`{"sha":"oldblob"}`))
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			_, _ = w.Write([]byte(`{"commit":{"sha":"c1"},"content":{"sha":"newblob"}}`))
		}
	}))
	defer srv.Close()
	ctx := newFake(map[string]string{"base_url": srv.URL, "token": "t"})
	out, err := opFileWrite(ctx, &FileWriteIn{
		Repo: "o/r", Path: "a/b.md", Content: "x", Message: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawGet {
		t.Error("写之前应先探一次拿 blob sha")
	}
	if putBody["sha"] != "oldblob" {
		t.Errorf("PUT 应带上已有文件的 sha，实际 body=%v", putBody)
	}
	if out.Created {
		t.Error("覆盖已有文件时 created 应为 false")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// —— Poll sources: this path had zero test coverage before ——
//
// It wasn't an oversight, it was the signature: the three poll functions originally took
// sokel.SourceCtx (a struct, which can't be faked to assert Trigger calls). The webhook path
// only had tests by luck — sokel.WebhookCtx happens to be an alias for plugin.SourceCtx, an
// interface. Only after narrowing to an interface did the tests below become possible.

func pollSrv(t *testing.T, body string) (*httptest.Server, *fakeSourceCtx) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, newSource(map[string]string{"base_url": srv.URL, "token": "t"})
}

// The activity feed is ordered newest-to-oldest, and GitHub gives 30 items per page.
func activityFeed(ids ...string) string {
	var parts []string
	for _, id := range ids {
		parts = append(parts, `{"id":"`+id+`","type":"PushEvent","actor":{"login":"alice"},
		 "payload":{"ref":"refs/heads/main","head":"sha`+id+`","commits":[{"message":"c`+id+`"}]}}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// **The first round only records the cursor, it doesn't fire**: the first poll pulls in the
// repo's whole recent activity. Firing all of it would flood the workflow with dozens of
// stale commits/issues the moment the plugin is installed, and the user's first reaction
// would be to turn the trigger off — which is worse than not having it at all.
func TestPollFirstRoundDoesNotFire(t *testing.T) {
	_, ctx := pollSrv(t, activityFeed("300", "299", "298"))

	seen, primed := map[string]string{}, map[string]bool{}
	if err := pollRepo(ctx, "o/r", seen, primed); err != nil {
		t.Fatal(err)
	}
	if len(ctx.order) != 0 {
		t.Fatalf("首轮不该推任何东西，实际推了 %v", ctx.order)
	}
	if seen["o/r"] != "300" || !primed["o/r"] {
		t.Fatalf("首轮必须把游标推到最新并标记 primed，实际 seen=%q primed=%v", seen["o/r"], primed["o/r"])
	}
}

// **Fire in chronological order, oldest first**: the activity feed is newest-to-oldest, so
// it must be iterated in reverse. Firing in feed order would mean the workflow sees the
// newest item first and older ones after — any process that accumulates state in order
// (e.g. publishing in commit order) would be reversed, with no error reported anywhere.
func TestPollFiresOldestFirst(t *testing.T) {
	_, ctx := pollSrv(t, activityFeed("103", "102", "101"))

	seen, primed := map[string]string{"o/r": "100"}, map[string]bool{"o/r": true}
	if err := pollRepo(ctx, "o/r", seen, primed); err != nil {
		t.Fatal(err)
	}
	want := []string{"commit_pushed|poll:101", "commit_pushed|poll:102", "commit_pushed|poll:103"}
	if !reflect.DeepEqual(ctx.order, want) {
		t.Fatalf("必须按 %v 的顺序推（旧→新），实际 %v", want, ctx.order)
	}
}

// **Only fire items after the cursor**: every round re-fetches 30 items, most of which were
// already fired last round. Without filtering, a single commit would trigger the workflow
// once per minute until it gets pushed out of those 30 items.
func TestPollSkipsAlreadySeen(t *testing.T) {
	_, ctx := pollSrv(t, activityFeed("103", "102", "101"))

	seen, primed := map[string]string{"o/r": "102"}, map[string]bool{"o/r": true}
	if err := pollRepo(ctx, "o/r", seen, primed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ctx.order, []string{"commit_pushed|poll:103"}) {
		t.Fatalf("只该推 103，实际 %v", ctx.order)
	}
	if seen["o/r"] != "103" {
		t.Fatalf("游标该推进到 103，实际 %q", seen["o/r"])
	}
}

// **ids are numeric strings and cannot be compared lexicographically**: once the digit
// count changes, "9" > "10" becomes true. When this bug actually happens, the symptom is
// that from the moment the id gains a digit, every subsequent event **gets judged as
// older**, and polling goes permanently silent — with not a single error in the logs.
func TestPollEventIDComparesNumerically(t *testing.T) {
	if !newerThan("10", "9") {
		t.Fatal("10 比 9 新——按字典序直接比会判反，一进位轮询就永久哑掉")
	}
	if newerThan("9", "10") {
		t.Fatal("9 不比 10 新")
	}
	if !newerThan("999", "") {
		t.Fatal("没有游标时一切都算新")
	}

	_, ctx := pollSrv(t, activityFeed("100", "99"))
	seen, primed := map[string]string{"o/r": "99"}, map[string]bool{"o/r": true}
	if err := pollRepo(ctx, "o/r", seen, primed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ctx.order, []string{"commit_pushed|poll:100"}) {
		t.Fatalf("进位后的 100 该被推出来，实际 %v", ctx.order)
	}
}

// **The activity feed payload has a different shape from the webhook payload**, so the
// dispatch logic has to be written separately. This pins down the two spots most likely
// to drift: ref must have refs/heads/ stripped, and the commit message must be the **last** one.
func TestPollActivityPayloadShape(t *testing.T) {
	_, ctx := pollSrv(t, `[{"id":"200","type":"PushEvent","actor":{"login":"alice"},
	 "payload":{"ref":"refs/heads/release/1.0","head":"abc",
	  "commits":[{"message":"早的"},{"message":"晚的"}]}}]`)

	seen, primed := map[string]string{"o/r": "1"}, map[string]bool{"o/r": true}
	if err := pollRepo(ctx, "o/r", seen, primed); err != nil {
		t.Fatal(err)
	}
	ev, ok := ctx.fired["commit_pushed"].(*CommitPushedEvent)
	if !ok {
		t.Fatalf("没推出 commit_pushed，实际 %v", ctx.order)
	}
	if ev.Ref != "release/1.0" {
		t.Fatalf("ref 要脱掉 refs/heads/（否则分支过滤条件全对不上），实际 %q", ev.Ref)
	}
	if ev.CommitMessage != "晚的" || ev.CommitCount != 2 {
		t.Fatalf("提交信息该取最后一条、条数该是 2，实际 %q / %d", ev.CommitMessage, ev.CommitCount)
	}
	if ev.Source != "poll" {
		t.Fatalf("来路必须标 poll——同一件事 webhook 也会推一次，排查靠它区分，实际 %q", ev.Source)
	}
}

// **A retried failure is a new, real event**: the dedup key must include attempt.
// Deduping by run_id alone means "still failing after a rerun" never gets notified — and
// that's exactly the case that needs someone's attention.
func TestPollFailedRunRetryIsNewEvent(t *testing.T) {
	runs := func(attempt string) string {
		return `{"workflow_runs":[{"id":77,"name":"CI","run_attempt":` + attempt + `,
		 "head_branch":"main","head_sha":"deadbeef","event":"push",
		 "actor":{"login":"alice"},"html_url":"https://x/77"}]}`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, runs("1"))
	}))
	defer srv.Close()
	ctx := newSource(map[string]string{"base_url": srv.URL, "token": "t"})

	seen := map[string]bool{}
	for i := 0; i < 2; i++ { // see the same failure across two consecutive rounds
		if err := pollFailedRuns(ctx, "o/r", seen, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(ctx.order) != 1 {
		t.Fatalf("同一次失败只该推一次，实际 %v", ctx.order)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, runs("2"))
	}))
	defer srv2.Close()
	ctx.cred["base_url"] = srv2.URL
	if err := pollFailedRuns(ctx, "o/r", seen, true); err != nil {
		t.Fatal(err)
	}
	if len(ctx.order) != 2 {
		t.Fatalf("重跑又挂了是一次新事件（去重键要带 attempt），实际 %v", ctx.order)
	}
}

// **A repo with Actions disabled returns 404 here**, and that must not crash the whole poll
// round — one credential can watch several repos, and one without Actions enabled would
// otherwise **stop all events from every other repo**.
func TestPollFailedRunsToleratesActionsDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()
	ctx := newSource(map[string]string{"base_url": srv.URL, "token": "t"})

	if err := pollFailedRuns(ctx, "o/r", map[string]bool{}, true); err != nil {
		t.Fatalf("没开 Actions 该静静跳过，实际报错让整轮停摆：%v", err)
	}
}
