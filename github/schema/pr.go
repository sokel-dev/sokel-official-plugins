package schema

// PR 域 + 机器人回执面。
//
// 覆盖的机器人形态：reviewdog / danger / CodeRabbit（读 diff → 行内评论 → 提交状态）、
// Mergify / bors（条件满足就合）、Renovate（开 PR）、自动指派 reviewer。
//
// **回执面**（CommitStatusCreate / CheckRunCreate / ReactionAdd）是「GitHub 机器人」区别于
// 「GitHub API 客户端」的地方：机器人要能把结论写回 PR 页面，而不只是读。

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// PrList PR 列表。
type PrList struct{}

func (PrList) Meta() contract.Meta {
	return contract.Meta{ID: "pr_list", Label: "PR 列表", Desc: "按状态/分支筛 Pull Request"}
}

func (PrList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Enum("state", field.Opt("open", "未关闭"), field.Opt("closed", "已关闭"),
			field.Opt("all", "全部")).Label("状态").Default("open").Optional(),
		field.String("base").Label("目标分支").Desc("只看合往这个分支的").Optional(),
		field.String("head").Label("来源分支").Desc("owner:branch 形态；跨 fork 时要带 owner").Optional(),
		field.Enum("sort", field.Opt("created", "创建时间"), field.Opt("updated", "更新时间"),
			field.Opt("popularity", "评论数"), field.Opt("long-running", "久未合并")).
			Label("排序").Default("created").Optional(),
		pageField(),
	}
}

func (PrList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("prs", []PR{}).Label("PR 列表"),
		countField(), hasMoreField(),
	}
}

// PR 一个 Pull Request。
type PR struct {
	Number    int      `sokel:"number" label:"编号"`
	Title     string   `sokel:"title" label:"标题"`
	Body      string   `sokel:"body" label:"正文"`
	State     string   `sokel:"state" label:"状态" desc:"open / closed"`
	Merged    bool     `sokel:"merged" label:"已合并" desc:"**closed 不等于 merged**——关掉不合也是 closed，判据看这个"`
	Draft     bool     `sokel:"draft" label:"草稿" desc:"草稿 PR 合不了，要先转正式"`
	Author    string   `sokel:"author" label:"作者"`
	Base      string   `sokel:"base" label:"目标分支"`
	Head      string   `sokel:"head" label:"来源分支"`
	HeadSHA   string   `sokel:"head_sha" label:"来源分支最新提交" desc:"**回写提交状态用它**"`
	Labels    []string `sokel:"labels" label:"标签"`
	Assignees []string `sokel:"assignees" label:"指派给"`
	Reviewers []string `sokel:"reviewers" label:"待评审人"`
	Mergeable string   `sokel:"mergeable" label:"能不能合" desc:"true/false/unknown——GitHub 是异步算的，刚开的 PR 多半是 unknown，等几秒再查"`
	Additions int      `sokel:"additions" label:"新增行数"`
	Deletions int      `sokel:"deletions" label:"删除行数"`
	URL       string   `sokel:"url" label:"页面地址"`
	CreatedAt string   `sokel:"created_at" label:"创建时间"`
	UpdatedAt string   `sokel:"updated_at" label:"更新时间"`
}

// PrGet PR 详情。
type PrGet struct{}

func (PrGet) Meta() contract.Meta {
	return contract.Meta{ID: "pr_get", Label: "PR 详情", Desc: "取单个 PR 的完整信息"}
}

func (PrGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{repoField(), numberField("PR 编号", "")}
}

func (PrGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("pr", PR{}).Label("PR")}
}

// PrCreate 开 PR。
type PrCreate struct{}

func (PrCreate) Meta() contract.Meta {
	return contract.Meta{ID: "pr_create", Label: "开 PR", Desc: "新建一个 Pull Request"}
}

func (PrCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("title").Label("标题"),
		field.String("head").Label("来源分支").Desc("跨 fork 时写 owner:branch"),
		field.String("base").Label("目标分支").Desc("留空 = 默认分支").Optional(),
		field.Text("body").Label("正文").Desc("markdown").Optional(),
		field.Bool("draft").Label("开成草稿").
			Desc("机器人开的 PR 建议先草稿，人看过再转正式").Optional(),
		field.Bool("maintainer_can_modify").Label("允许维护者改").Default(true).Optional(),
	}
}

func (PrCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("number").Label("编号"),
		field.String("url").Label("页面地址"),
		field.String("head_sha").Label("来源分支最新提交"),
	}
}

// PrUpdate 改 PR。
type PrUpdate struct{}

func (PrUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "pr_update", Label: "改 PR",
		Desc: "改标题/正文/目标分支/状态；草稿转正式也在这"}
}

func (PrUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("PR 编号", ""),
		field.String("title").Label("标题").Desc("留空 = 不改").Optional(),
		field.Text("body").Label("正文").Desc("留空 = 不改").Optional(),
		field.String("base").Label("目标分支").Desc("换目标分支；留空 = 不改").Optional(),
		field.Enum("state", field.Opt("open", "重开"), field.Opt("closed", "关闭")).
			Label("状态").Desc("留空 = 不改").Optional(),
		field.Bool("ready_for_review").Label("草稿转正式").
			Desc("**这一项走 GraphQL**：REST 没有把草稿转正式的接口").Optional(),
	}
}

func (PrUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("number").Label("编号"),
		field.String("state").Label("当前状态"),
		field.Bool("draft").Label("是否草稿"),
		field.String("url").Label("页面地址"),
	}
}

// PrMerge 合 PR。
type PrMerge struct{}

func (PrMerge) Meta() contract.Meta {
	return contract.Meta{ID: "pr_merge", Label: "合 PR",
		Desc: "合并 Pull Request（三种合法都支持）", TimeoutSec: 60}
}

func (PrMerge) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("PR 编号", ""),
		field.Enum("method", field.Opt("merge", "合并提交"), field.Opt("squash", "压成一个提交"),
			field.Opt("rebase", "变基")).Label("合并方式").Default("merge").Optional(),
		field.String("title").Label("合并提交标题").Desc("留空 = GitHub 默认").Optional(),
		field.Text("message").Label("合并提交正文").Optional(),
		field.String("expect_sha").Label("期望的最新提交").
			Desc("**并发保护**：填了就只在来源分支仍是这个 sha 时才合。" +
				"机器人「检查通过就合」的场景必须填——从检查通过到发起合并之间对方可能又推了一版").Optional(),
	}
}

func (PrMerge) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("merged").Label("是否已合"),
		field.String("sha").Label("合并后的提交"),
		field.String("message").Label("GitHub 的说明").
			Desc("没合成时这里是原因（如「Base branch was modified」= expect_sha 对不上）"),
	}
}

// PrFiles PR 改了哪些文件。评审机器人读 diff 的入口。
type PrFiles struct{}

func (PrFiles) Meta() contract.Meta {
	return contract.Meta{ID: "pr_files", Label: "PR 改了哪些文件",
		Desc: "列出 PR 的文件改动与 diff（评审机器人的输入）"}
}

func (PrFiles) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("PR 编号", ""),
		field.Bool("with_patch").Label("带上 diff 正文").
			Desc("默认只给文件名与增删行数；打开才给 patch（很长，注意下游上下文）").Optional(),
		pageField(),
	}
}

func (PrFiles) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("files", []PRFile{}).Label("文件改动"),
		countField(), hasMoreField(),
		field.Int("additions").Label("本页新增行数"),
		field.Int("deletions").Label("本页删除行数"),
	}
}

// PRFile PR 里的一个文件改动。
type PRFile struct {
	Path      string `sokel:"path" label:"路径"`
	Status    string `sokel:"status" label:"状态" desc:"added / modified / removed / renamed"`
	Additions int    `sokel:"additions" label:"新增行数"`
	Deletions int    `sokel:"deletions" label:"删除行数"`
	Patch     string `sokel:"patch" label:"diff" desc:"只有 with_patch 打开才有；二进制文件恒为空"`
	SHA       string `sokel:"sha" label:"blob sha"`
}

// PrReview 提交评审（含行内评论）。
type PrReview struct{}

func (PrReview) Meta() contract.Meta {
	return contract.Meta{ID: "pr_review", Label: "提交评审",
		Desc: "以 approve / 请求修改 / 纯评论的身份提交一次评审，可带 diff 行内评论"}
}

func (PrReview) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("PR 编号", ""),
		field.Enum("event", field.Opt("COMMENT", "只评论"), field.Opt("APPROVE", "通过"),
			field.Opt("REQUEST_CHANGES", "请求修改")).Label("结论").Default("COMMENT").Optional(),
		field.Text("body").Label("总评").
			Desc("REQUEST_CHANGES 与 COMMENT **必须有总评**，空的会 422").Optional(),
		field.Array("comments", []ReviewComment{}).Label("行内评论").
			Desc("针对 diff 某一行的评论。**行号是新版文件的行号**，且必须落在本次 diff 涉及的行上，" +
				"否则整个评审 422（不是那一条被忽略）").Optional(),
	}
}

func (PrReview) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("review_id").Label("评审 ID"),
		field.String("state").Label("状态"),
		field.String("url").Label("页面地址"),
	}
}

// ReviewComment 一条行内评论。
type ReviewComment struct {
	Path string `sokel:"path" label:"文件路径"`
	Line int    `sokel:"line" label:"行号" desc:"新版文件的行号；必须是本次 diff 动过的行"`
	Side string `sokel:"side" label:"哪一侧" desc:"RIGHT（新版，默认）或 LEFT（旧版，评论被删掉的行时用）"`
	Body string `sokel:"body" label:"内容"`
}

// PrRequestReviewers 请人评审。自动分配 reviewer 的机器人用它。
type PrRequestReviewers struct{}

func (PrRequestReviewers) Meta() contract.Meta {
	return contract.Meta{ID: "pr_request_reviewers", Label: "请人评审",
		Desc: "给 PR 加评审人或评审团队（自动分配 reviewer 的机器人用这个）"}
}

func (PrRequestReviewers) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("PR 编号", ""),
		field.Strings("reviewers").Label("评审人").Desc("用户名").Optional(),
		field.Strings("team_reviewers").Label("评审团队").Desc("团队 slug（组织仓库才有）").Optional(),
		field.Bool("remove").Label("改成移除").Desc("默认是加；打开则把列出的移除").Optional(),
	}
}

func (PrRequestReviewers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("reviewers").Label("现在的评审人").
			Desc("**PR 作者不能是自己的评审人**——传了会被 GitHub 静默丢掉，拿这个核对"),
	}
}

// —— 机器人回执面 ——

// CommitStatusCreate 回写提交状态。PR 页面上那一排 ✓/✗ 就是它。
type CommitStatusCreate struct{}

func (CommitStatusCreate) Meta() contract.Meta {
	return contract.Meta{ID: "commit_status_create", Label: "回写提交状态",
		Desc: "在某个提交上挂一条状态（PR 页面下方那排 ✓/✗），机器人把结论写回 GitHub 的最简方式"}
}

func (CommitStatusCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("sha").Label("提交 sha").
			Desc("**要用 PR 的 head_sha，不是合并提交**——写错地方的状态不会出现在 PR 页面上"),
		field.Enum("state", field.Opt("success", "通过"), field.Opt("failure", "失败"),
			field.Opt("error", "出错"), field.Opt("pending", "进行中")).Label("状态"),
		field.String("context").Label("检查名").
			Desc("这条状态的标识（如 sokel/lint）。**同名会覆盖**——进行中→完成就是靠同名覆盖做的").
			Default("sokel"),
		field.String("description").Label("一句话说明").Desc("最多 140 字，超了 GitHub 会截断").Optional(),
		field.String("target_url").Label("详情链接").Desc("点状态跳转到哪（如工作流运行记录）").Optional(),
	}
}

func (CommitStatusCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("status_id").Label("状态 ID"),
		field.String("context").Label("检查名"),
	}
}

// CheckRunCreate 建检查运行。比提交状态富：能带注解、markdown 摘要、结论。
type CheckRunCreate struct{}

func (CheckRunCreate) Meta() contract.Meta {
	return contract.Meta{ID: "check_run_create", Label: "建检查运行",
		Desc: "Checks API：带标题/markdown 摘要/行内注解的检查结果（比「提交状态」富，但要 App 或细粒度令牌）"}
}

func (CheckRunCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("sha").Label("提交 sha").Desc("同「回写提交状态」：用 PR 的 head_sha"),
		field.String("name").Label("检查名"),
		field.Enum("conclusion", field.Opt("success", "通过"), field.Opt("failure", "失败"),
			field.Opt("neutral", "中性"), field.Opt("cancelled", "已取消"),
			field.Opt("timed_out", "超时"), field.Opt("action_required", "需人工处理")).
			Label("结论").Desc("留空 = 标成「进行中」").Optional(),
		field.String("title").Label("标题").Optional(),
		field.Text("summary").Label("摘要").Desc("markdown").Optional(),
		field.Text("details").Label("详情").Desc("markdown，显示在摘要下方").Optional(),
		field.Array("annotations", []Annotation{}).Label("行内注解").
			Desc("**一次最多 50 条**，超出的部分 GitHub 直接丢掉不报错").Optional(),
		field.String("details_url").Label("详情链接").Optional(),
	}
}

func (CheckRunCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("check_run_id").Label("检查运行 ID"),
		field.String("url").Label("页面地址"),
		field.Int("annotations_sent").Label("实际提交的注解数").
			Desc("超过 50 条时这里是 50——差额就是被丢掉的"),
	}
}

// Annotation 一条行内注解。
type Annotation struct {
	Path      string `sokel:"path" label:"文件路径"`
	StartLine int    `sokel:"start_line" label:"起始行"`
	EndLine   int    `sokel:"end_line" label:"结束行" desc:"单行就与起始行相同"`
	Level     string `sokel:"level" label:"级别" desc:"notice / warning / failure"`
	Message   string `sokel:"message" label:"内容"`
	Title     string `sokel:"title" label:"标题" desc:"可留空"`
}

// ReactionAdd 加表情。ChatOps 机器人「收到了」的标准手势。
type ReactionAdd struct{}

func (ReactionAdd) Meta() contract.Meta {
	return contract.Meta{ID: "reaction_add", Label: "加表情",
		Desc: "给 Issue/PR 或某条评论加一个表情——ChatOps 机器人回执「收到」的标准做法"}
}

func (ReactionAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Enum("target", field.Opt("issue", "Issue 或 PR 本身"), field.Opt("comment", "某条评论")).
			Label("加在哪").Default("comment").Optional(),
		field.Int("id").Label("编号或评论 ID").
			Desc("target=issue 时填 Issue/PR 编号；target=comment 时填评论 ID（发评论的出参里有）"),
		field.Enum("content", field.Opt("+1", "👍"), field.Opt("-1", "👎"), field.Opt("laugh", "😄"),
			field.Opt("confused", "😕"), field.Opt("heart", "❤️"), field.Opt("hooray", "🎉"),
			field.Opt("rocket", "🚀"), field.Opt("eyes", "👀")).
			Label("表情").Desc("👀 = 已看到在处理，🚀 = 已执行，是 ChatOps 的通行约定").Default("eyes").Optional(),
	}
}

func (ReactionAdd) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("reaction_id").Label("表情 ID"),
		field.Bool("already").Label("之前就加过").Desc("GitHub 对重复加表情回 200，不是错"),
	}
}
