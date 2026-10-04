package schema

// Event contracts + credential contract.
//
// Events have **two sources**: the platform receiving webhooks (zero latency) and a polling
// source (about 1 minute of latency, no webhook setup needed). Both paths push the same set of
// event contracts; the source field in the outputs says which one this event came from.
// **The docs say to pick one**: if both are enabled, the same action fires once from each
// (event_id is a separate namespace per source — see the top comment in webhook.go).
//
// Which events exist follows the bot shapes: ChatOps needs comment events, a CI watchdog needs
// workflow failures, triage needs new-issue and labeled events, release notifications need release.

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Fields common to all events. Every event carries repo, and the platform flattens it into the
// top level of the trigger input, so every branch can share the same {{trigger.repo}}.
type Events struct{}

func (Events) CommonFields() []string { return []string{"repo"} }

func repoEventField() contract.FieldSpec {
	return field.String("repo").Label("仓库").Desc("owner/repo")
}

func sourceField() contract.FieldSpec {
	return field.String("source").Label("来路").
		Desc("webhook 或 poll——两条路都开时用它区分").Optional()
}

// CommitPushed fires when there's a new commit.
type CommitPushed struct{}

func (CommitPushed) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "commit_pushed", Label: "有新提交",
		Desc: "有人往被监听的仓库推了提交"}
}

func (CommitPushed) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.String("ref").Label("分支").Desc("已去掉 refs/heads/ 前缀"),
		field.String("commit_sha").Label("最新提交"),
		field.String("commit_message").Label("最新提交信息"),
		field.Int("commit_count").Label("本次推了几个提交"),
		field.String("author").Label("推送者"),
		field.Bool("forced").Label("是强推").Desc("强推会改写历史——审计场景要看它").Optional(),
		field.Bool("created").Label("是新建分支").Optional(),
		field.Bool("deleted").Label("是删除分支").
			Desc("**删分支也是 push 事件**，且 commit_sha 全是 0——不判这个会去处理一个不存在的提交").Optional(),
		sourceField(),
	}
}

// PrOpened fires when a new PR is opened.
type PrOpened struct{}

func (PrOpened) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pr_opened", Label: "新 PR", Desc: "有人开了 Pull Request"}
}

func (PrOpened) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("编号"),
		field.String("title").Label("标题"),
		field.Text("body").Label("正文").Optional(),
		field.String("author").Label("作者"),
		field.String("base").Label("目标分支"),
		field.String("head").Label("来源分支"),
		field.String("head_sha").Label("来源分支最新提交").Desc("**回写提交状态用它**"),
		field.Bool("draft").Label("是草稿").Desc("草稿 PR 一般不该触发评审机器人").Optional(),
		field.Strings("labels").Label("标签").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// PrMerged fires when a PR is merged.
type PrMerged struct{}

func (PrMerged) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pr_merged", Label: "PR 被合并",
		Desc: "PR 合并进目标分支（**关掉不合不算**）"}
}

func (PrMerged) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("编号"),
		field.String("title").Label("标题"),
		field.String("author").Label("作者"),
		field.String("merged_by").Label("合并者"),
		field.String("base").Label("目标分支"),
		field.String("merge_sha").Label("合并后的提交"),
		field.Strings("labels").Label("标签").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// PrReviewSubmitted fires when a PR review is submitted.
type PrReviewSubmitted struct{}

func (PrReviewSubmitted) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pr_review_submitted", Label: "PR 有评审",
		Desc: "有人提交了评审结论（通过/请求修改/评论）"}
}

func (PrReviewSubmitted) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("PR 编号"),
		field.String("title").Label("PR 标题"),
		field.String("reviewer").Label("评审人"),
		field.String("state").Label("结论").Desc("approved / changes_requested / commented"),
		field.Text("body").Label("总评").Optional(),
		field.String("head_sha").Label("被评审的提交").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// IssueOpened fires when a new Issue is opened.
type IssueOpened struct{}

func (IssueOpened) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "issue_opened", Label: "新 Issue", Desc: "有人开了 Issue"}
}

func (IssueOpened) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("编号"),
		field.String("title").Label("标题"),
		field.Text("body").Label("正文").Optional(),
		field.String("author").Label("创建者"),
		field.Strings("labels").Label("标签").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// IssueLabeled fires when an Issue is labeled. The most common trigger point for triage pipelines.
type IssueLabeled struct{}

func (IssueLabeled) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "issue_labeled", Label: "Issue 被打标签",
		Desc: "Issue 或 PR 被加上了标签（摘标签不触发）"}
}

func (IssueLabeled) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("编号"),
		field.String("title").Label("标题"),
		field.Text("body").Label("正文").Optional(),
		field.String("added_label").Label("这次加的标签").
			Desc("**一次事件只对应一个标签**——GitHub 每加一个标签发一次，比 GitLab 的批量 diff 干净"),
		field.Strings("labels").Label("现在的全部标签"),
		field.Bool("is_pr").Label("其实是 PR").Desc("GitHub 给 PR 打标签走的也是 issues 事件").Optional(),
		field.String("actor").Label("操作者"),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// IssueCommented fires on a new Issue comment. The entry point for ChatOps.
type IssueCommented struct{}

func (IssueCommented) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "issue_commented", Label: "Issue 有新评论",
		Desc: "Issue 下有人发言（ChatOps 斜杠命令从这里进）"}
}

func (IssueCommented) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("编号"),
		field.String("title").Label("标题"),
		field.Text("comment").Label("评论内容"),
		field.Int("comment_id").Label("评论 ID").Desc("**加表情回执用它**"),
		field.String("author").Label("发言人"),
		field.String("author_association").Label("发言人身份").
			Desc("OWNER / MEMBER / COLLABORATOR / CONTRIBUTOR / NONE。" +
				"**ChatOps 必须查它**——否则任何路人都能用斜杠命令指挥你的机器人").Optional(),
		field.Bool("is_bot").Label("是机器人发的").
			Desc("**必须挡掉**：机器人回复自己的评论会无限循环，这是 ChatOps 第一个踩的坑").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// PrCommented fires on a new PR comment. Kept as a separate event from the Issue comment event so
// that "saying something in a PR" doesn't get routed to Issue-handling logic.
type PrCommented struct{}

func (PrCommented) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pr_commented", Label: "PR 有新评论",
		Desc: "PR 下有人发言（GitHub 与 Issue 评论同一个事件，这里按是不是 PR 拆开）"}
}

func (PrCommented) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.Int("number").Label("PR 编号"),
		field.String("title").Label("PR 标题"),
		field.Text("comment").Label("评论内容"),
		field.Int("comment_id").Label("评论 ID"),
		field.String("author").Label("发言人"),
		field.String("author_association").Label("发言人身份").Optional(),
		field.Bool("is_bot").Label("是机器人发的").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// ReleasePublished fires when a new release is published.
type ReleasePublished struct{}

func (ReleasePublished) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "release_published", Label: "有新发布",
		Desc: "发布了一个 Release（草稿转正式也算）"}
}

func (ReleasePublished) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.String("tag").Label("标签"),
		field.String("name").Label("标题"),
		field.Text("body").Label("发布说明").Optional(),
		field.Bool("prerelease").Label("是预发布").Optional(),
		field.String("author").Label("发布者"),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// WorkflowFailed fires when a workflow run fails. The trigger point for a CI watchdog.
type WorkflowFailed struct{}

func (WorkflowFailed) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "workflow_failed", Label: "工作流失败",
		Desc: "Actions 跑挂了（只在结论是 failure 时触发，取消与跳过不算）"}
}

func (WorkflowFailed) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoEventField(),
		field.String("workflow").Label("工作流名"),
		field.Int("run_id").Label("运行 ID").Desc("拿它去查作业与日志"),
		field.String("branch").Label("分支"),
		field.String("commit_sha").Label("提交"),
		field.String("event").Label("原始触发事件").Desc("push / pull_request / schedule…"),
		field.String("actor").Label("触发者"),
		field.Int("attempt").Label("第几次重试").
			Desc("**重试失败会再发一次事件**——只想报一次的话判 attempt==1").Optional(),
		field.String("url").Label("页面地址"),
		sourceField(),
	}
}

// —— Credential ——

// Credential: instance address + token + webhook signature verification + which repos to watch.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("base_url").Label("GitHub 地址").
			Desc("GitHub Enterprise Server 填根地址（如 https://github.example.com，" +
				"**不用带 /api/v3**，插件会补）；留空 = github.com").Optional(),
		field.Secret("token").Label("访问令牌").
			Desc("个人访问令牌。经典令牌勾 repo（私有仓库）与 workflow（触发 Actions）；" +
				"细粒度令牌按仓库授权，至少给 Contents/Issues/Pull requests 读写，" +
				"要回写检查结果再加 Commit statuses 与 Checks。建议专建一个机器人账号"),
		field.Secret("webhook_secret").Label("Webhook Secret").
			Desc("平台代收 webhook 用：GitHub 仓库 Settings→Webhooks 里填的 Secret，" +
				"两边一致才收（**HMAC-SHA256 验签**）。不用 webhook 可留空").Optional(),
		field.Text("watch_repos").Label("事件盯哪些仓库").
			Desc("owner/repo 逗号分隔（如 sokel-dev/sokel,acme/infra）。**填了才启动轮询事件源**：" +
				"新提交/新 PR/合并/新 Issue/评论/工作流失败 会触发工作流（约 1 分钟延迟）。" +
				"配了 Webhook 就**不要**再填这个——两条路会各触发一次").Optional(),
	}
}
