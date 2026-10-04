package main

// 对着**真** GitHub 跑一遍。
//
// 假上游只能验我们自己写的装配逻辑。有几类东西它天然验不了，而这几类恰恰最容易错：
//
//   - **GraphQL 查询写对没有**。查询串是纯文本，字段名拼错时 GitHub 回 200 + errors，
//     假上游根本不会去解析那串查询——看板那四个操作在假测里是零覆盖。
//   - **/issues 到底会不会返回 PR**。这是整个插件的头号设计前提，靠的是文档而不是实测。
//   - **Link 头长什么样、403 到底带不带 X-RateLimit-Remaining**。都是我们照文档写死的判据。
//   - **令牌权限够不够**（细粒度令牌的 Checks、看板的 project scope）。
//
// 跑法：
//
//	# 只读那批，任何令牌都能跑
//	GITHUB_TOKEN=ghp_xxx go test -run Live ./...
//
//	# 加上写操作（会真的建 Issue/分支/PR）——**务必指向一个一次性仓库**
//	GITHUB_TOKEN=ghp_xxx GITHUB_WRITE_REPO=you/scratch go test -run Live -v ./...
//
//	# 看板（要 project scope）
//	GITHUB_TOKEN=ghp_xxx GITHUB_PROJECT_OWNER=you go test -run LiveProject -v ./...
//
// 没设 GITHUB_TOKEN 就整批跳过，CI 与他人机器上不会因此变红。

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
			"base_url": os.Getenv("GITHUB_BASE_URL"), // GHES 联调时设它，留空 = github.com
		},
	}
}

// liveReadRepo 只读测试用的仓库。默认拿 GitHub 自己的示例仓库，任何令牌都读得到。
func liveReadRepo() string {
	if r := os.Getenv("GITHUB_TEST_REPO"); r != "" {
		return r
	}
	return "octocat/Hello-World"
}

// liveWriteRepo 会真的写东西的仓库。**必须显式指定**，且请用一次性仓库。
func liveWriteRepo(t *testing.T) string {
	t.Helper()
	r := os.Getenv("GITHUB_WRITE_REPO")
	if r == "" {
		t.Skip("设 GITHUB_WRITE_REPO=you/scratch 跑写操作（会真的建 Issue/分支/PR，请用一次性仓库）")
	}
	return r
}

// —— 只读：验那些「照文档写死」的判据 ——

// 令牌能不能用、余量多少、是经典还是细粒度。联调第一条永远是它。
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

// **这条是整个插件的头号前提**：GitHub 的 /issues 真的会把 PR 一起返回吗？
// 假上游里是我自己塞的 pull_request 键，等于自己验自己。这里拿真数据看。
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

// 分页判据来自 Link 头，是照文档写死的。翻页在真接口上到底成不成立，只能实测。
func TestLivePaginationLinkHeader(t *testing.T) {
	ctx := liveCtx(t)
	// 拿一个提交多的仓库，确保有第二页
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

// 404 对「不存在」与「没权限」是同一个码，我们的报错文案要把这层说全。
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

// 搜索是少数会给总数的接口，也是唯一会说「结果不完整」的。
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

// —— 看板：GraphQL 查询串写对没有，只能对着真接口验 ——

// 假上游对 GraphQL 是**零覆盖**：查询串是纯文本，字段拼错时 GitHub 回 200 + errors，
// 而我们的 ghGraphQL 会把它翻成一条错误——所以这条一旦通过，说明查询串确实是对的。
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
	// 卡片查询用了 fieldValues 上的 4 个内联片段，是全插件最容易写错的一段
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

// —— 写操作：真的会改仓库，务必用一次性仓库 ——

// 一条完整的机器人链路：开 Issue → 评论 → 打标签 → 加表情 → 关闭。
// 每一步都在验一个「假上游验不了」的点：标签不存在会不会 422、表情重复加会不会报错、
// 关闭时 state_reason 收不收。
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

	// 加表情：ChatOps 的回执手势。重复加 GitHub 回 200 不是错。
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

	// 标签必须先存在。建标签是幂等的，这里连跑两次验它。
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

	// 关闭时给 not_planned——stale-bot 的正确做法
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

// 开 PR 的机器人那条链路：建分支 → 写文件 → 开 PR → 读 diff → 回写提交状态。
// 回写提交状态是「机器人」的核心动作，也是最该实测的一步——写错 sha 不会报错，
// 只是状态不出现在 PR 页面上。
func TestLivePullRequestFlow(t *testing.T) {
	ctx := liveCtx(t)
	repo := liveWriteRepo(t)
	branch := fmt.Sprintf("sokel-live-%d", time.Now().Unix())

	br, err := opBranchCreate(ctx, &BranchCreateIn{Repo: repo, Branch: branch})
	if err != nil {
		t.Fatalf("建分支失败: %v", err)
	}
	t.Logf("建了分支 %s → %s", br.Branch, br.SHA)
	// 幂等：再建一次应当 existed=true 而不是报错
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

	// 再写一次同一个文件——**这一步验的是 get-then-put 拿 blob sha**，不带 sha 会 422
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

	// 回写提交状态：**必须用 head_sha**
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

	// 检查运行：经典 PAT 会 403，这时报错文案要指路
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

	// 收尾：关掉 PR，别留一堆开着的
	if _, err := opPrUpdate(ctx, &PrUpdateIn{Repo: repo, Number: pr.Number, State: "closed"}); err != nil {
		t.Errorf("关 PR 失败（请手动清理 %s）: %v", pr.URL, err)
	}
	t.Logf("已关闭 PR #%d；分支 %s 与文件 %s 留在仓库里，需要的话手动删",
		pr.Number, branch, path)
}

// 限流文案只有在真被限流时才会走到。这条不制造限流，只验「余量头确实存在」——
// 我们区分「限流」与「权限不够」全靠它。
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
