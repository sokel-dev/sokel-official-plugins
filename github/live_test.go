package main

// Runs a pass against **real** GitHub.
//
// The fake upstream can only validate the assembly logic we wrote ourselves. There are a
// few kinds of things it inherently can't validate, and those are exactly the ones most
// likely to be wrong:
//
//   - **Whether the GraphQL queries are actually correct**. A query string is plain text;
//     when a field name is misspelled, GitHub returns 200 + errors, and the fake upstream
//     never parses that query string — the four Projects operations have zero coverage
//     in the fake tests.
//   - **Whether /issues actually returns PRs mixed in**. This is the whole plugin's number
//     one design assumption, backed by documentation rather than an actual test.
//   - **What the Link header actually looks like, and whether a 403 actually carries
//     X-RateLimit-Remaining**. Both are criteria we hardcoded from reading the docs.
//   - **Whether the token actually has enough permission** (Checks for fine-grained
//     tokens, project scope for Projects).
//
// How to run:
//
//	# the read-only batch, any token works
//	GITHUB_TOKEN=ghp_xxx go test -run Live ./...
//
//	# add write operations (will actually create Issues/branches/PRs) — **must point at a throwaway repo**
//	GITHUB_TOKEN=ghp_xxx GITHUB_WRITE_REPO=you/scratch go test -run Live -v ./...
//
//	# Projects (needs project scope)
//	GITHUB_TOKEN=ghp_xxx GITHUB_PROJECT_OWNER=you go test -run LiveProject -v ./...
//
// With GITHUB_TOKEN unset, the whole batch is skipped, so it won't turn CI or anyone
// else's machine red.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func liveCtx(t *testing.T) *fakeCtx {
	t.Helper()
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("设 GITHUB_TOKEN=<你的令牌> 跑真实 GitHub 接口")
	}
	return &fakeCtx{
		Context: context.Background(),
		cred: map[string]string{
			"token":    token,
			"base_url": os.Getenv("GITHUB_BASE_URL"), // set this for GHES live testing, empty = github.com
		},
	}
}

// liveReadRepo is the repo used for read-only tests. Defaults to GitHub's own sample repo, readable by any token.
func liveReadRepo() string {
	if r := os.Getenv("GITHUB_TEST_REPO"); r != "" {
		return r
	}
	return "octocat/Hello-World"
}

// liveWriteRepo is the repo that write operations actually modify. **Must be set explicitly**, and please use a throwaway repo.
func liveWriteRepo(t *testing.T) string {
	t.Helper()
	r := os.Getenv("GITHUB_WRITE_REPO")
	if r == "" {
		t.Skip("设 GITHUB_WRITE_REPO=you/scratch 跑写操作（会真的建 Issue/分支/PR，请用一次性仓库）")
	}
	return r
}

// —— Read-only: validates the criteria "hardcoded from reading the docs" ——

// Whether the token works, how much quota is left, classic vs. fine-grained. Always the first live test to run.
func TestLiveHealthCheck(t *testing.T) {
	ctx := liveCtx(t)
	out, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("健康检查本身不该抛错（凭证坏了也要回 ok=false）: %v", err)
	}
	if !out.OK {
		t.Fatalf("令牌不可用：%s", out.Message)
	}
	t.Logf("登录身份=%s 权限范围=%v 本小时余量=%d", out.Login, out.Scopes, out.RateRemaining)
	if len(out.Scopes) == 0 {
		t.Log("（没有权限范围 = 细粒度令牌，正常）")
	}
	if out.RateRemaining == 0 {
		t.Error("余量为 0——要么真被限流了，要么 X-RateLimit-Remaining 头没解出来")
	}
}

// **This is the plugin's number one assumption**: does GitHub's /issues really return
// PRs mixed in? In the fake upstream, the pull_request key is something I stuffed in
// myself, which amounts to validating against my own assumption. Here we check against real data.
func TestLiveIssuesListReturnsPRs(t *testing.T) {
	ctx := liveCtx(t)
	repo := liveReadRepo()

	withPRs, err := opIssuesList(ctx, &IssuesListIn{Repo: repo, State: "all", IncludePrs: true})
	if err != nil {
		t.Fatalf("列 Issue 失败: %v", err)
	}
	dropped, err := opIssuesList(ctx, &IssuesListIn{Repo: repo, State: "all"})
	if err != nil {
		t.Fatal(err)
	}
	prs := 0
	for _, is := range withPRs.Issues {
		if is.IsPR {
			prs++
		}
	}
	t.Logf("%s 上：连 PR 一起 %d 条，其中 PR %d 条；剔完 %d 条（报告剔掉 %d）",
		repo, withPRs.Count, prs, dropped.Count, dropped.DroppedPrs)
	if prs != dropped.DroppedPrs {
		t.Errorf("剔除计数对不上：真实 PR %d 条，报告剔掉 %d 条", prs, dropped.DroppedPrs)
	}
	if withPRs.Count-dropped.Count != prs {
		t.Errorf("剔前剔后差 %d，应等于 PR 数 %d", withPRs.Count-dropped.Count, prs)
	}
	if prs == 0 {
		t.Log("（这个仓库这一页里恰好没有 PR，这条没验到剔除本身——换一个 PR 多的仓库再跑一次）")
	}
}

// The pagination criterion comes from the Link header, hardcoded from reading the docs.
// Whether pagination actually holds up against the real API can only be verified by testing it live.
func TestLivePaginationLinkHeader(t *testing.T) {
	ctx := liveCtx(t)
	// Use a repo with many commits, to guarantee a second page
	repo := liveReadRepo()
	out, err := opCommitsList(ctx, &CommitsListIn{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("第 1 页 %d 条，还有下一页=%v", out.Count, out.HasMore)
	if !out.HasMore {
		t.Skip("这个仓库不足一页，验不到 Link 头——设 GITHUB_TEST_REPO 换个大仓库")
	}
	p2, err := opCommitsList(ctx, &CommitsListIn{Repo: repo, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Count == 0 {
		t.Error("说有下一页，第 2 页却是空的——Link 头解析有问题")
	}
	if len(out.Commits) > 0 && len(p2.Commits) > 0 && out.Commits[0].SHA == p2.Commits[0].SHA {
		t.Error("第 2 页与第 1 页首条相同——page 参数没生效")
	}
}

// 404 is the same status code for both "doesn't exist" and "no permission"; our error message needs to spell that out fully.
func TestLiveNotFoundWording(t *testing.T) {
	ctx := liveCtx(t)
	_, err := opRepoGet(ctx, &RepoGetIn{Repo: "sokel-dev/definitely-not-a-real-repo-xyz"})
	if err == nil {
		t.Fatal("不存在的仓库应该报错")
	}
	if !strings.Contains(err.Error(), "权限") {
		t.Errorf("404 的文案要提醒「没权限也回 404」，当前是：%v", err)
	}
	t.Logf("404 文案：%v", err)
}

// Search is one of the few endpoints that returns a total count, and the only one that can say "results incomplete".
func TestLiveSearch(t *testing.T) {
	ctx := liveCtx(t)
	out, err := opSearch(ctx, &SearchIn{
		Type: "issues", Query: "repo:" + liveReadRepo() + " is:issue", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("命中 %d 条（本页 %d），不完整=%v", out.Total, out.Count, out.Incomplete)
	for _, h := range out.Results {
		if h.Repo == "" {
			t.Errorf("命中缺 repo 字段（从 html_url 反推那步没生效）：%+v", h)
			break
		}
	}
}

// —— Projects: whether the GraphQL query strings are correct can only be verified against the real endpoint ——

// The fake upstream has **zero coverage** for GraphQL: a query string is plain text, and
// when a field is misspelled GitHub returns 200 + errors, which our ghGraphQL turns into
// an error — so once this test passes, it means the query string is genuinely correct.
func TestLiveProjectsList(t *testing.T) {
	ctx := liveCtx(t)
	owner := os.Getenv("GITHUB_PROJECT_OWNER")
	if owner == "" {
		t.Skip("设 GITHUB_PROJECT_OWNER=<用户名或组织名> 验看板（令牌要有 project 权限）")
	}
	out, err := opProjectsList(ctx, &ProjectsListIn{Owner: owner})
	if err != nil {
		if strings.Contains(err.Error(), "看板权限") {
			t.Skipf("令牌没有 project 权限：%v", err)
		}
		t.Fatalf("GraphQL 查询失败——多半是查询串写错了: %v", err)
	}
	t.Logf("%s 名下 %d 个看板", owner, out.Count)
	for _, p := range out.Projects {
		t.Logf("  #%d %s（%d 张卡片）id=%s", p.Number, p.Title, p.Items, p.ID)
		if !strings.HasPrefix(p.ID, "PVT_") {
			t.Errorf("看板节点 ID 应以 PVT_ 开头，实际 %q", p.ID)
		}
	}
	if out.Count == 0 {
		t.Skip("这个账号下没有看板，卡片查询验不到")
	}
	// The item query uses 4 inline fragments on fieldValues, the section of the whole plugin most prone to mistakes
	items, err := opProjectItemsList(ctx, &ProjectItemsListIn{
		Owner: owner, Number: out.Projects[0].Number, Limit: 10,
	})
	if err != nil {
		t.Fatalf("看板卡片查询失败——fieldValues 那段内联片段多半写错了: %v", err)
	}
	t.Logf("看板 #%d 取到 %d 张卡片", out.Projects[0].Number, items.Count)
	for _, it := range items.Items {
		t.Logf("  [%s] %s 所在列=%q 字段=%v", it.Type, it.Title, it.Status, it.Fields)
	}
}

// —— Write operations: actually modify the repo, must use a throwaway repo ——

// A full bot workflow: open Issue → comment → label → react → close.
// Each step validates something the fake upstream can't: whether a nonexistent label
// causes a 422, whether adding the same reaction twice errors, whether state_reason is
// accepted on close.
func TestLiveIssueLifecycle(t *testing.T) {
	ctx := liveCtx(t)
	repo := liveWriteRepo(t)
	stamp := time.Now().Format("2006-01-02 15:04:05")

	created, err := opIssueCreate(ctx, &IssueCreateIn{
		Repo: repo, Title: "[sokel 联调] " + stamp,
		Body: "这条 Issue 由 sokel github 插件的 live_test 创建，可以直接关掉。",
	})
	if err != nil {
		t.Fatalf("开 Issue 失败: %v", err)
	}
	t.Logf("开了 Issue #%d：%s", created.Number, created.URL)

	cm, err := opIssueComment(ctx, &IssueCommentIn{
		Repo: repo, Number: created.Number, Body: "联调评论。",
	})
	if err != nil {
		t.Fatalf("发评论失败: %v", err)
	}

	// Add a reaction: the ChatOps acknowledgment gesture. GitHub returning 200 on a duplicate add is not an error.
	if _, err := opReactionAdd(ctx, &ReactionAddIn{
		Repo: repo, Target: "comment", ID: cm.CommentID, Content: "eyes",
	}); err != nil {
		t.Errorf("给评论加表情失败: %v", err)
	}
	if _, err := opReactionAdd(ctx, &ReactionAddIn{
		Repo: repo, Target: "comment", ID: cm.CommentID, Content: "eyes",
	}); err != nil {
		t.Errorf("重复加同一个表情不该失败（GitHub 回 200）: %v", err)
	}

	// A label must exist before it can be applied. Creating a label is idempotent; run it twice here to verify that.
	const label = "sokel-live-test"
	for i := 0; i < 2; i++ {
		out, err := opLabelCreate(ctx, &LabelCreateIn{
			Repo: repo, Name: label, Color: "ededed", Description: "sokel 联调用",
		})
		if err != nil {
			t.Fatalf("第 %d 次建标签失败（应当幂等）: %v", i+1, err)
		}
		t.Logf("建标签第 %d 次：新建=%v", i+1, out.Created)
	}
	lbl, err := opIssueLabel(ctx, &IssueLabelIn{
		Repo: repo, Number: created.Number, Mode: "add", Labels: []string{label},
	})
	if err != nil {
		t.Fatalf("打标签失败: %v", err)
	}
	if !contains(lbl.Labels, label) {
		t.Errorf("打完标签出参里没有它：%v", lbl.Labels)
	}

	// Close with not_planned — the correct behavior for a stale-bot
	closed, err := opIssueUpdate(ctx, &IssueUpdateIn{
		Repo: repo, Number: created.Number, State: "closed", StateReason: "not_planned",
	})
	if err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if closed.State != "closed" {
		t.Errorf("状态应为 closed，实际 %q", closed.State)
	}
	t.Logf("已关闭 #%d", created.Number)
}

// The PR-opening bot workflow: create branch → write file → open PR → read diff → write commit status back.
// Writing the commit status is the "bot's" core action and the step most worth testing live
// — getting the sha wrong doesn't error, the status simply never shows up on the PR page.
func TestLivePullRequestFlow(t *testing.T) {
	ctx := liveCtx(t)
	repo := liveWriteRepo(t)
	branch := fmt.Sprintf("sokel-live-%d", time.Now().Unix())

	br, err := opBranchCreate(ctx, &BranchCreateIn{Repo: repo, Branch: branch})
	if err != nil {
		t.Fatalf("建分支失败: %v", err)
	}
	t.Logf("建了分支 %s → %s", br.Branch, br.SHA)
	// Idempotent: creating it again should give existed=true rather than an error
	again, err := opBranchCreate(ctx, &BranchCreateIn{Repo: repo, Branch: branch})
	if err != nil || !again.Existed {
		t.Errorf("重复建分支应当 existed=true 且不报错，得 %+v / %v", again, err)
	}

	path := "sokel-live-test.md"
	w1, err := opFileWrite(ctx, &FileWriteIn{
		Repo: repo, Path: path, Branch: branch,
		Content: "sokel 联调 " + time.Now().Format(time.RFC3339) + "\n",
		Message: "chore: sokel live test",
	})
	if err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	t.Logf("写文件：新建=%v 提交=%s", w1.Created, w1.CommitSHA)

	// Write the same file again — **this step validates get-then-put fetching the blob sha**; without the sha it's a 422
	w2, err := opFileWrite(ctx, &FileWriteIn{
		Repo: repo, Path: path, Branch: branch,
		Content: "sokel 联调 第二次\n", Message: "chore: sokel live test 2",
	})
	if err != nil {
		t.Fatalf("覆盖已有文件失败——多半是没带 blob sha: %v", err)
	}
	if w2.Created {
		t.Error("第二次写同一个文件，created 应为 false")
	}

	pr, err := opPrCreate(ctx, &PrCreateIn{
		Repo: repo, Title: "[sokel 联调] " + branch, Head: branch,
		Body: "由 live_test 创建，可直接关闭。", Draft: true,
	})
	if err != nil {
		t.Fatalf("开 PR 失败: %v", err)
	}
	t.Logf("开了 PR #%d：%s（head_sha=%s）", pr.Number, pr.URL, pr.HeadSHA)

	files, err := opPrFiles(ctx, &PrFilesIn{Repo: repo, Number: pr.Number, WithPatch: true})
	if err != nil {
		t.Fatalf("读 PR 文件失败: %v", err)
	}
	t.Logf("PR 改了 %d 个文件，+%d/-%d", files.Count, files.Additions, files.Deletions)
	if files.Count > 0 && files.Files[0].Patch == "" {
		t.Error("打开了 with_patch 却没有 diff 正文")
	}

	// Write the commit status back: **must use head_sha**
	st, err := opCommitStatusCreate(ctx, &CommitStatusCreateIn{
		Repo: repo, SHA: pr.HeadSHA, State: "success",
		Context: "sokel/live-test", Description: "联调通过",
		TargetURL: "https://example.com/run/1",
	})
	if err != nil {
		t.Fatalf("回写提交状态失败: %v", err)
	}
	t.Logf("回写状态 id=%d context=%s —— 去 %s 看 PR 页面上有没有这一条",
		st.StatusID, st.Context, pr.URL)

	// Check run: a classic PAT gets a 403 here, and the error message needs to point the way
	if _, err := opCheckRunCreate(ctx, &CheckRunCreateIn{
		Repo: repo, SHA: pr.HeadSHA, Name: "sokel/live-check", Conclusion: "success",
		Title: "联调", Summary: "由 live_test 建",
	}); err != nil {
		if !strings.Contains(err.Error(), "提交状态") {
			t.Errorf("建检查运行失败，且文案没指路到「回写提交状态」：%v", err)
		}
		t.Logf("建检查运行不可用（经典令牌的预期结果）：%v", err)
	} else {
		t.Log("检查运行建成功——说明这是个有 Checks 权限的细粒度令牌")
	}

	// Cleanup: close the PR, don't leave a pile of open ones
	if _, err := opPrUpdate(ctx, &PrUpdateIn{Repo: repo, Number: pr.Number, State: "closed"}); err != nil {
		t.Errorf("关 PR 失败（请手动清理 %s）: %v", pr.URL, err)
	}
	t.Logf("已关闭 PR #%d；分支 %s 与文件 %s 留在仓库里，需要的话手动删",
		pr.Number, branch, path)
}

// The rate-limit message path is only reached when actually rate limited. This test
// doesn't trigger rate limiting; it only verifies the "remaining quota header actually
// exists" — distinguishing "rate limited" from "insufficient permission" depends entirely on it.
func TestLiveRateLimitHeaderExists(t *testing.T) {
	ctx := liveCtx(t)
	_, h, err := ghCall(ctx, http.MethodGet, "/rate_limit", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("X-RateLimit-Remaining") == "" {
		t.Error("响应里没有 X-RateLimit-Remaining——403 就没法区分限流与权限不够了")
	}
	t.Logf("余量=%s 重置于=%s", h.Get("X-RateLimit-Remaining"), h.Get("X-RateLimit-Reset"))
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
