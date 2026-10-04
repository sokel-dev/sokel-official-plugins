package main

// 测的是这个插件里**有判断**的地方，不是 HTTP 转发本身。
// 每条对应 schema 包顶注或代码注释里点名的一个坑——那些坑的共同点是「错了不报错」。

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

// fakeSourceCtx 记录 Trigger，用来断言「推了哪个事件、带了什么」。
type fakeSourceCtx struct {
	*fakeCtx
	fired map[string]any // 事件 id → payload
	ids   map[string]string
	order []string // 「事件名|event_id」，按推送顺序——次数与先后只有它看得见
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

// —— 验签：安全边界，错了会放进伪造的事件 ——

func sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// GitHub 用 HMAC-SHA256 签**原始请求体**，不是 GitLab 那种明文 token 比对。
// 照抄 GitLab 的写法会永远验不过；而更糟的方向是「验不过就放行」。
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
		// 大小写与前缀都不能宽容：sha1= 是 GitHub 的老签名头，安全性已不够
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

// 签名算的必须是**原始字节**。重新编码过的 JSON（键序、空格都会变）算出来的签名对不上，
// 而症状是「所有 webhook 都 401」——很容易被误当成 secret 填错。
func TestWebhookSignatureUsesRawBody(t *testing.T) {
	raw := []byte(`{"b":1,  "a":2}`) // 故意留多余空格且键无序
	ctx := newSource(map[string]string{"webhook_secret": "k"})
	req := &sokel.WebhookRequest{
		Body:    raw,
		Headers: map[string]string{"X-Hub-Signature-256": sign("k", raw)},
	}
	if _, ok := verifySignature(ctx, req); !ok {
		t.Fatal("原始字节签名应通过——说明实现里对 body 做了重新编码")
	}
}

// —— /issues 会连 PR 一起返回：GitHub API 最经典的坑 ——

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
	// 打开开关就该保留，并且如实标记 is_pr
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

// —— webhook 分发：三条「不判会派错活」的分支 ——

// closed 不等于 merged：关掉不合也是 closed。判错的话「PR 合并后发布」会在每次关闭 PR 时误触发。
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

// PR 下的评论走的是同一个 issue_comment 事件——不区分的话
// 「在 PR 里说句话」会去派 Issue 的活。
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

// 机器人自己的评论必须能被认出来——否则 ChatOps 会自我唤醒成死循环。
func TestBotDetection(t *testing.T) {
	cases := []struct {
		user map[string]any
		want bool
	}{
		{map[string]any{"login": "alice", "type": "User"}, false},
		{map[string]any{"login": "dependabot[bot]", "type": "Bot"}, true},
		// type 缺席的老 payload 靠名字后缀兜住
		{map[string]any{"login": "renovate[bot]"}, true},
		{map[string]any{"login": "botanist", "type": "User"}, false}, // 名字里有 bot 不算
	}
	for _, c := range cases {
		if got := isBot(c.user); got != c.want {
			t.Errorf("isBot(%v) = %v, want %v", c.user, got, c.want)
		}
	}
}

// event_id 用 X-GitHub-Delivery：GitHub 重投时沿用同一个，正好是平台去重要的语义。
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

// GitHub 保存 webhook 配置时立刻发一条 ping——不回 200 的话配置页显示成失败。
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

// 认不出的事件类型也要回 200：非 2xx 会让 GitHub 反复重试，连错几次后它会把 webhook 停掉。
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

// —— 地址拼接：GHES 的 GraphQL 端点不跟 REST 的 /api/v3 ——

func TestBaseURLs(t *testing.T) {
	cases := []struct{ base, rest, gql string }{
		{"", "https://api.github.com", "https://api.github.com/graphql"},
		{"https://gh.example.com", "https://gh.example.com/api/v3", "https://gh.example.com/api/graphql"},
		{"https://gh.example.com/", "https://gh.example.com/api/v3", "https://gh.example.com/api/graphql"},
		// 用户把 /api/v3 一起填进来是很常见的——不去重会拼成 /api/v3/api/v3
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

// GitHub 不回总数，翻页只能看 Link 头。
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

// 403 有两种含义，混为一谈会让人拿着「权限不够」去反复检查 scope，而其实只要等几分钟。
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
	// 404 要说清「没权限也回 404」，否则用户会一直核对路径
	err = ghErr(404, []byte(`{"message":"Not Found"}`), "/repos/o/r", http.Header{})
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "权限") {
		t.Errorf("404 要提示可能是权限问题：%v", err)
	}
}

// 提交状态的说明有 140 字上限，按字符截而不是按字节——按字节会把多字节字符切成乱码。
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

// 轮询游标：活动流 id 是递增数字串，可能超出 int64——按长度+字典序比。
func TestNewerThan(t *testing.T) {
	cases := []struct {
		id, last string
		want     bool
	}{
		{"100", "", true},
		{"101", "100", true},
		{"100", "100", false},
		{"99", "100", false},
		{"1000", "999", true}, // 位数多的更新
		{"999", "1000", false},
	}
	for _, c := range cases {
		if got := newerThan(c.id, c.last); got != c.want {
			t.Errorf("newerThan(%q,%q) = %v, want %v", c.id, c.last, got, c.want)
		}
	}
}

// GitHub 把协作者权限回成一张布尔表，取最高那档才是人想看的角色。
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

// 写文件必须带 blob sha，否则 422。这条测的是「有没有先探一次」。
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

// —— 轮询源：这条路以前一行测试都没有 ——
//
// 不是疏忽是签名：三个 poll 函数原先收 sokel.SourceCtx（结构体，假不出 Trigger）。
// webhook 有测试纯属运气——sokel.WebhookCtx 恰好是 plugin.SourceCtx 的别名，是接口。
// 收窄成接口之后才有了下面这些。

func pollSrv(t *testing.T, body string) (*httptest.Server, *fakeSourceCtx) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, newSource(map[string]string{"base_url": srv.URL, "token": "t"})
}

// 活动流是新→旧排的，GitHub 一次给 30 条。
func activityFeed(ids ...string) string {
	var parts []string
	for _, id := range ids {
		parts = append(parts, `{"id":"`+id+`","type":"PushEvent","actor":{"login":"alice"},
		 "payload":{"ref":"refs/heads/main","head":"sha`+id+`","commits":[{"message":"c`+id+`"}]}}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// **首轮只记游标不推**：轮询第一次拉到的是仓库近期全部活动。
// 照发的话，插件一装上就有几十条陈年提交/Issue 涌进工作流，
// 用户的第一反应是把触发器关掉——那比没有更糟。
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

// **按时间顺序推，旧的先**：活动流是新→旧排的，得倒着遍历。
// 顺着推的话，工作流收到的是「先看到最新一条，再看到更早的」——
// 任何按顺序累积状态的流程（比如按提交顺序发布）都会被搞反，且不会有任何东西报错。
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

// **只推游标之后的**：每轮都会重新拉到 30 条，其中大部分上一轮已经推过。
// 不过滤的话，一条提交每分钟触发一次工作流，直到它被挤出这 30 条。
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

// **id 是数字串，不能按字典序直接比**：位数一变，"9" > "10" 就成立了。
// 真出这个错的表现是：id 进位的那一刻起，之后的事件**全部被判为更旧**，
// 轮询从此彻底哑掉——而日志里一条错都没有。
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

// **活动流的 payload 与 webhook 的形状不同**，所以分派得单独写一份。
// 这里钉住最容易漂的两处：ref 要去掉 refs/heads/，提交信息取**最后一条**。
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

// **重试失败是一次新的真事件**：去重键必须带 attempt。
// 只按 run_id 去重的话，「重跑还是挂了」永远不会通知——而那恰恰是要人管的那次。
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
	for i := 0; i < 2; i++ { // 同一次失败连着看到两轮
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

// **没开 Actions 的仓库这里返回 404**，不能让它把整轮轮询带崩——
// 同一个凭证盯好几个仓库，一个没开 Actions 就会让**其余仓库的事件全部停摆**。
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
