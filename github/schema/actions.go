package schema

// GitHub Actions + releases + repo housekeeping (labels/milestones/collaborators/branch protection).
//
// Bot shapes covered: CI watchdog (open an Issue / notify on failure), release-drafter (cut a
// release), labeler's prerequisite (the label must exist first), access audits (who has write
// access, whether main branch protection got turned off).

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// WorkflowRunsList lists workflow runs.
type WorkflowRunsList struct{}

func (WorkflowRunsList) Meta() contract.Meta {
	return contract.Meta{ID: "workflow_runs_list", Label: "工作流运行记录",
		Desc: "列出 Actions 的运行记录（按工作流/分支/状态筛）"}
}

func (WorkflowRunsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("workflow").Label("工作流").
			Desc("文件名（如 ci.yml）或数字 ID；留空 = 全部工作流").Optional(),
		field.String("branch").Label("分支").Optional(),
		field.Enum("status", field.Opt("completed", "已完成"), field.Opt("in_progress", "进行中"),
			field.Opt("queued", "排队中"), field.Opt("failure", "失败"),
			field.Opt("success", "成功")).Label("状态").Optional(),
		field.String("actor").Label("触发者").Desc("用户名").Optional(),
		pageField(),
	}
}

func (WorkflowRunsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("runs", []WorkflowRun{}).Label("运行记录"),
		countField(), hasMoreField(),
	}
}

// WorkflowRun is a single workflow run.
type WorkflowRun struct {
	ID         int    `sokel:"id" label:"运行 ID" desc:"查作业与日志要它"`
	Name       string `sokel:"name" label:"工作流名"`
	Status     string `sokel:"status" label:"状态" desc:"queued / in_progress / completed"`
	Conclusion string `sokel:"conclusion" label:"结论" desc:"success / failure / cancelled…；**未完成时为空**，别拿空当成功"`
	Branch     string `sokel:"branch" label:"分支"`
	SHA        string `sokel:"sha" label:"提交"`
	Event      string `sokel:"event" label:"触发事件" desc:"push / pull_request / workflow_dispatch…"`
	Actor      string `sokel:"actor" label:"触发者"`
	RunNumber  int    `sokel:"run_number" label:"第几次运行"`
	Attempt    int    `sokel:"attempt" label:"第几次重试"`
	URL        string `sokel:"url" label:"页面地址"`
	CreatedAt  string `sokel:"created_at" label:"开始时间"`
	UpdatedAt  string `sokel:"updated_at" label:"更新时间"`
}

// WorkflowDispatch manually triggers a workflow.
type WorkflowDispatch struct{}

func (WorkflowDispatch) Meta() contract.Meta {
	return contract.Meta{ID: "workflow_dispatch", Label: "触发工作流",
		Desc: "手动跑一个 Actions 工作流（工作流得声明了 workflow_dispatch 才行）"}
}

func (WorkflowDispatch) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("workflow").Label("工作流").Desc("文件名（如 deploy.yml）或数字 ID"),
		field.String("ref").Label("分支或标签").Desc("留空 = 默认分支"),
		field.Json("inputs", map[string]string{}).Label("输入参数").
			Desc("对应工作流里的 inputs。**值必须是字符串**——GitHub 只收字符串，传数字会 422").Optional(),
	}
}

func (WorkflowDispatch) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("dispatched").Label("已发出"),
		field.String("note").Label("说明").
			Desc("**GitHub 这个接口不返回运行 ID**（204 空响应）——要拿到 run_id 得隔几秒去查运行记录"),
	}
}

// RunJobs lists the jobs in a single run.
type RunJobs struct{}

func (RunJobs) Meta() contract.Meta {
	return contract.Meta{ID: "run_jobs", Label: "运行的作业",
		Desc: "列出某次工作流运行里的作业与每一步的结果（定位是哪一步挂的）"}
}

func (RunJobs) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Int("run_id").Label("运行 ID").Desc("从「工作流运行记录」拿"),
	}
}

func (RunJobs) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("jobs", []Job{}).Label("作业列表"),
		countField(),
		field.Strings("failed_steps").Label("失败的步骤").
			Desc("跨所有作业汇总成 作业名/步骤名，直接可以发进通知——不用调用方自己再遍历一遍"),
	}
}

// Job is a single job.
type Job struct {
	ID         int      `sokel:"id" label:"作业 ID" desc:"取日志要它"`
	Name       string   `sokel:"name" label:"作业名"`
	Status     string   `sokel:"status" label:"状态"`
	Conclusion string   `sokel:"conclusion" label:"结论"`
	Steps      []string `sokel:"steps" label:"步骤" desc:"步骤名 → 结论，形如 build=success"`
	StartedAt  string   `sokel:"started_at" label:"开始时间"`
	URL        string   `sokel:"url" label:"页面地址"`
}

// JobLog is a job's log.
type JobLog struct{}

func (JobLog) Meta() contract.Meta {
	return contract.Meta{ID: "job_log", Label: "作业日志",
		Desc: "取某个作业的日志正文（默认只给尾部，够定位失败原因）", TimeoutSec: 60}
}

func (JobLog) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Int("job_id").Label("作业 ID").Desc("从「运行的作业」拿"),
		field.Int("tail").Label("只要末尾多少行").
			Desc("默认 200。日志动辄几 MB，整篇塞进下游会把上下文吃满；**要全文填 -1**（不是 0——0 与「没填」在线上分不开）").Optional(),
	}
}

func (JobLog) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("log").Label("日志"),
		field.Int("lines").Label("返回行数"),
		field.Bool("truncated").Label("被截断了").Desc("true = 前面还有内容"),
	}
}

// —— Releases ——

// ReleasesList lists releases.
type ReleasesList struct{}

func (ReleasesList) Meta() contract.Meta {
	return contract.Meta{ID: "releases_list", Label: "发布列表", Desc: "列出仓库的 Release"}
}

func (ReleasesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{repoField(), pageField()}
}

func (ReleasesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("releases", []Release{}).Label("发布列表"),
		countField(), hasMoreField(),
	}
}

// Release is a single release.
type Release struct {
	ID          int    `sokel:"id" label:"发布 ID"`
	TagName     string `sokel:"tag_name" label:"标签"`
	Name        string `sokel:"name" label:"标题"`
	Body        string `sokel:"body" label:"发布说明"`
	Draft       bool   `sokel:"draft" label:"草稿"`
	Prerelease  bool   `sokel:"prerelease" label:"预发布"`
	Author      string `sokel:"author" label:"发布者"`
	URL         string `sokel:"url" label:"页面地址"`
	CreatedAt   string `sokel:"created_at" label:"创建时间"`
	PublishedAt string `sokel:"published_at" label:"发布时间" desc:"草稿为空"`
}

// ReleaseCreate cuts a release.
type ReleaseCreate struct{}

func (ReleaseCreate) Meta() contract.Meta {
	return contract.Meta{ID: "release_create", Label: "发版",
		Desc: "创建一个 Release（可让 GitHub 自动生成发布说明）"}
}

func (ReleaseCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("tag").Label("标签名").Desc("如 v1.2.0。标签不存在会**按 target 自动创建**"),
		field.String("target").Label("基于").Desc("分支名或提交 sha；标签已存在时忽略；留空 = 默认分支").Optional(),
		field.String("name").Label("标题").Desc("留空 = 用标签名").Optional(),
		field.Text("body").Label("发布说明").Desc("留空且打开了自动生成，就用 GitHub 生成的").Optional(),
		field.Bool("generate_notes").Label("自动生成发布说明").
			Desc("GitHub 按上个标签以来的 PR 生成。**与手写正文可以并存**：生成的会接在正文后面").Optional(),
		field.Bool("draft").Label("存为草稿").Optional(),
		field.Bool("prerelease").Label("标为预发布").Optional(),
	}
}

func (ReleaseCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("release_id").Label("发布 ID"),
		field.String("tag").Label("标签"),
		field.String("url").Label("页面地址"),
		field.Text("body").Label("最终的发布说明").Desc("自动生成时这里是 GitHub 生成的全文"),
	}
}

// —— Repo housekeeping ——

// LabelsList lists repo labels.
type LabelsList struct{}

func (LabelsList) Meta() contract.Meta {
	return contract.Meta{ID: "labels_list", Label: "标签列表",
		Desc: "列出仓库定义的标签（打标签前先确认它存在）"}
}

func (LabelsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{repoField(), pageField()}
}

func (LabelsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("labels", []Label{}).Label("标签列表"),
		countField(), hasMoreField(),
	}
}

// Label is a single label.
type Label struct {
	Name        string `sokel:"name" label:"名字"`
	Color       string `sokel:"color" label:"颜色" desc:"六位十六进制，不带 #"`
	Description string `sokel:"description" label:"说明"`
}

// LabelCreate creates a label. A prerequisite for labeler bots — applying a label that doesn't
// exist yet returns 422.
type LabelCreate struct{}

func (LabelCreate) Meta() contract.Meta {
	return contract.Meta{ID: "label_create", Label: "建标签",
		Desc: "新建或更新一个仓库标签（已存在则改颜色与说明，不报错）"}
}

func (LabelCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("name").Label("名字"),
		field.String("color").Label("颜色").Desc("六位十六进制，不带 #（如 d73a4a）；留空 = 随机").Optional(),
		field.String("description").Label("说明").Desc("最多 100 字").Optional(),
	}
}

func (LabelCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("name").Label("名字"),
		field.Bool("created").Label("是新建").Desc("false = 已存在，做了更新"),
	}
}

// MilestonesList lists milestones.
type MilestonesList struct{}

func (MilestonesList) Meta() contract.Meta {
	return contract.Meta{ID: "milestones_list", Label: "里程碑列表",
		Desc: "列出里程碑与完成进度"}
}

func (MilestonesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Enum("state", field.Opt("open", "进行中"), field.Opt("closed", "已关闭"),
			field.Opt("all", "全部")).Label("状态").Default("open").Optional(),
	}
}

func (MilestonesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("milestones", []Milestone{}).Label("里程碑列表"),
		countField(),
	}
}

// Milestone is a single milestone.
type Milestone struct {
	Number       int    `sokel:"number" label:"编号" desc:"给 Issue 设里程碑时可以用它，也可以用标题"`
	Title        string `sokel:"title" label:"标题"`
	Description  string `sokel:"description" label:"说明"`
	State        string `sokel:"state" label:"状态"`
	OpenIssues   int    `sokel:"open_issues" label:"未完成"`
	ClosedIssues int    `sokel:"closed_issues" label:"已完成"`
	DueOn        string `sokel:"due_on" label:"截止时间"`
	URL          string `sokel:"url" label:"页面地址"`
}

// CollaboratorsList lists collaborators. Used for access audits.
type CollaboratorsList struct{}

func (CollaboratorsList) Meta() contract.Meta {
	return contract.Meta{ID: "collaborators_list", Label: "协作者列表",
		Desc: "列出对仓库有权限的人及其权限级别（准入审计；也用来判断某人能不能被指派）"}
}

func (CollaboratorsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Enum("permission", field.Opt("push", "有写权限的"),
			field.Opt("admin", "管理员"), field.Opt("pull", "只读"),
			field.Opt("triage", "可分类"), field.Opt("maintain", "可维护")).
			Label("只看某个权限").Desc("留空 = 全部").Optional(),
		pageField(),
	}
}

func (CollaboratorsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("collaborators", []Collaborator{}).Label("协作者"),
		countField(), hasMoreField(),
	}
}

// Collaborator is a single collaborator.
type Collaborator struct {
	Login      string `sokel:"login" label:"用户名"`
	Permission string `sokel:"permission" label:"权限" desc:"admin / maintain / push / triage / pull"`
	Type       string `sokel:"type" label:"类型" desc:"User 或 Bot"`
	URL        string `sokel:"url" label:"主页"`
}

// BranchProtectionGet looks up branch protection.
type BranchProtectionGet struct{}

func (BranchProtectionGet) Meta() contract.Meta {
	return contract.Meta{ID: "branch_protection_get", Label: "查分支保护",
		Desc: "读取某个分支的保护规则（合规巡检：主分支的保护有没有被人关掉）"}
}

func (BranchProtectionGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("branch").Label("分支").Desc("留空 = 默认分支").Optional(),
	}
}

func (BranchProtectionGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("protected").Label("受保护").
			Desc("**false 时下面各项都是零值**——没有保护规则时 GitHub 回 404，这里翻译成 protected=false 而不是报错"),
		field.Int("required_approvals").Label("需要几个人批准"),
		field.Bool("dismiss_stale_reviews").Label("新提交作废旧批准"),
		field.Bool("require_code_owner_review").Label("需要 code owner 批准"),
		field.Strings("required_checks").Label("必须通过的检查").
			Desc("检查名，与「回写提交状态」的 context 是同一个命名空间"),
		field.Bool("enforce_admins").Label("管理员也受约束"),
		field.Bool("allow_force_push").Label("允许强推"),
		field.Bool("required_linear_history").Label("要求线性历史"),
	}
}
