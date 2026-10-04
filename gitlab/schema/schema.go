// Package schema declares the gitlab plugin's operation and credential contracts.
//
// Scope: GitLab automation -- supports both self-hosted CE and gitlab.com (set the instance
// address in the credential). Covers four domains: repository (read/write files, branches,
// commits), MR (create/list/comment/merge), Issue, CI/CD (list pipelines/trigger/view failure
// logs), plus call as a catch-all for direct REST v4 access.
//
// Three GitLab-specific conventions that the operation design revolves around:
//
//   - project has two forms: a numeric id or a "group/name" path both work (the path gets
//     URL-encoded), and every operation's project field follows the same rule -- it's most
//     convenient to copy the path straight out of the repository address on the canvas.
//   - Writing a file goes through the commits endpoint rather than the files endpoint: a
//     single commit can contain multiple file actions and naturally carries a commit message;
//     simplified here to a single file (create/update is auto-detected), multi-file goes
//     through call.
//   - All lists are paginated (page/per_page): the first page defaults to 50 items + the total
//     header is passed through; bump page to page further.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

func projectField() contract.FieldSpec {
	return field.String("project").Label("项目").
		Desc("数字 ID 或路径（如 backend/server，即仓库地址里域名后那段）")
}

func pageField() contract.FieldSpec {
	return field.Int("page").Label("页码").Desc("默认 1；每页 50 条").Optional()
}

// -- Projects --

// ProjectsList lists projects.
type ProjectsList struct{}

func (ProjectsList) Meta() contract.Meta {
	return contract.Meta{ID: "projects_list", Label: "项目列表",
		Desc: "列出有权限的项目（可按名字搜）"}
}

func (ProjectsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("search").Label("搜索").Desc("按名字/路径模糊搜；留空列全部").Optional(),
		pageField(),
	}
}

func (ProjectsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("projects", []Project{}).Label("项目列表"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").Desc("GitLab 报的**全部**条数（不是本页）。0 = 上游没给这个头（超大结果集时会省略）").Optional(),
		field.Bool("has_more").Label("还有下一页").Desc("**翻页看它,别看本页条数**——正好 50 条并不代表翻完了").Optional(),
	}
}

// Project is a single project.
type Project struct {
	ID            int    `sokel:"id" label:"ID"`
	Path          string `sokel:"path" label:"路径" desc:"group/name 形态，其他操作的 project 字段用它"`
	Name          string `sokel:"name" label:"名称"`
	DefaultBranch string `sokel:"default_branch,optional" label:"默认分支"`
	WebURL        string `sokel:"web_url" label:"页面地址"`
	LastActivity  string `sokel:"last_activity,optional" label:"最近活动"`
}

// -- Repository --

// FileGet reads a file.
type FileGet struct{}

func (FileGet) Meta() contract.Meta {
	return contract.Meta{ID: "file_get", Label: "读文件",
		Desc: "读仓库里一个文件的内容（配置/清单/文档都常用）"}
}

func (FileGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("path").Label("文件路径").Desc("仓库内路径，如 config/app.yaml"),
		field.String("ref").Label("分支/tag/commit").Desc("留空用默认分支").Optional(),
	}
}

func (FileGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("content").Label("内容"),
		field.String("last_commit_id").Label("最后修改的 commit"),
	}
}

// FileWrite writes a file (as a commit).
type FileWrite struct{}

func (FileWrite) Meta() contract.Meta {
	return contract.Meta{ID: "file_write", Label: "写文件（提交）",
		Desc: "把内容提交到仓库（文件不存在则创建，存在则覆盖，自动判断）。" +
			"多文件一次提交走「通用调用」的 commits 接口"}
}

func (FileWrite) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("path").Label("文件路径"),
		field.Text("content").Label("内容"),
		field.String("branch").Label("提交到分支").Desc("留空用默认分支"),
		field.String("message").Label("提交信息"),
		field.String("start_branch").Label("从哪个分支切出").
			Desc("branch 不存在时以它为起点新建（提 MR 的常用流）；留空不新建").Optional(),
	}
}

func (FileWrite) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("commit_id").Label("提交 ID"),
		field.String("web_url").Label("提交页面"),
	}
}

// BranchesList lists branches.
type BranchesList struct{}

func (BranchesList) Meta() contract.Meta {
	return contract.Meta{ID: "branches_list", Label: "分支列表"}
}

func (BranchesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("search").Label("搜索").Optional(),
		pageField(),
	}
}

func (BranchesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("branches", []Branch{}).Label("分支列表"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").Desc("GitLab 报的**全部**条数（不是本页）。0 = 上游没给这个头（超大结果集时会省略）").Optional(),
		field.Bool("has_more").Label("还有下一页").Desc("**翻页看它,别看本页条数**——正好 50 条并不代表翻完了").Optional(),
	}
}

// Branch is a single branch.
type Branch struct {
	Name      string `sokel:"name" label:"名称"`
	Default   bool   `sokel:"default" label:"是否默认分支"`
	Protected bool   `sokel:"protected" label:"是否保护分支"`
	CommitID  string `sokel:"commit_id,optional" label:"最新提交"`
}

// CommitsList lists commits.
type CommitsList struct{}

func (CommitsList) Meta() contract.Meta {
	return contract.Meta{ID: "commits_list", Label: "提交列表",
		Desc: "某分支最近的提交（变更监控：定时查 → 有新提交触发下游）"}
}

func (CommitsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("ref").Label("分支").Desc("留空用默认分支").Optional(),
		field.String("since").Label("起始时间").Desc("RFC3339；留空不限").Optional(),
		pageField(),
	}
}

func (CommitsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("commits", []Commit{}).Label("提交列表"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").Desc("GitLab 报的**全部**条数（不是本页）。0 = 上游没给这个头（超大结果集时会省略）").Optional(),
		field.Bool("has_more").Label("还有下一页").Desc("**翻页看它,别看本页条数**——正好 50 条并不代表翻完了").Optional(),
	}
}

// Commit is a single commit.
type Commit struct {
	ID        string `sokel:"id" label:"提交 ID"`
	ShortID   string `sokel:"short_id" label:"短 ID"`
	Title     string `sokel:"title" label:"标题"`
	Author    string `sokel:"author" label:"作者"`
	CreatedAt string `sokel:"created_at" label:"时间"`
	WebURL    string `sokel:"web_url,optional" label:"页面地址"`
}

// -- MR --

// MrList lists MRs.
type MrList struct{}

func (MrList) Meta() contract.Meta {
	return contract.Meta{ID: "mr_list", Label: "MR 列表"}
}

func (MrList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Enum("state", field.Opt("opened", "开着的"), field.Opt("merged", "已合并"),
			field.Opt("closed", "已关闭"), field.Opt("all", "全部")).
			Label("状态").Default("opened"),
		field.String("target_branch").Label("目标分支过滤").Optional(),
		pageField(),
	}
}

func (MrList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("mrs", []Mr{}).Label("MR 列表"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").
			Desc("GitLab 报的**全部**条数（不是本页）。0 = 上游没给这个头（超大结果集时会省略）").Optional(),
		field.Bool("has_more").Label("还有下一页").
			Desc("**翻页看它，别看本页条数**——正好 50 条并不代表翻完了").Optional(),
	}
}

// Mr is a single MR.
type Mr struct {
	IID          int    `sokel:"iid" label:"MR 编号" desc:"项目内编号（!42 的 42），其他 MR 操作认它"`
	Title        string `sokel:"title" label:"标题"`
	State        string `sokel:"state" label:"状态"`
	SourceBranch string `sokel:"source_branch" label:"源分支"`
	TargetBranch string `sokel:"target_branch" label:"目标分支"`
	Author       string `sokel:"author,optional" label:"作者"`
	WebURL       string `sokel:"web_url" label:"页面地址"`
	MergeStatus  string `sokel:"merge_status,optional" label:"可合并性" desc:"can_be_merged / cannot_be_merged…"`
}

// MrCreate creates an MR.
type MrCreate struct{}

func (MrCreate) Meta() contract.Meta {
	return contract.Meta{ID: "mr_create", Label: "建 MR"}
}

func (MrCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("source_branch").Label("源分支"),
		field.String("target_branch").Label("目标分支").Desc("留空用默认分支"),
		field.String("title").Label("标题"),
		field.Text("description").Label("描述").Optional(),
		field.Bool("remove_source_branch").Label("合并后删源分支").Default(true),
	}
}

func (MrCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("iid").Label("MR 编号"),
		field.String("web_url").Label("页面地址"),
	}
}

// MrMerge merges an MR.
type MrMerge struct{}

func (MrMerge) Meta() contract.Meta {
	return contract.Meta{ID: "mr_merge", Label: "合并 MR",
		Desc: "合并一个 MR（未过 CI / 有冲突时 GitLab 会拒绝，报错里说明原因）"}
}

func (MrMerge) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("MR 编号"),
		field.Bool("squash").Label("squash 合并").Default(false),
		field.Bool("when_pipeline_succeeds").Label("等流水线过了再合").Default(false),
	}
}

func (MrMerge) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("state").Label("合并后状态"),
		field.String("merge_commit_id").Label("合并提交"),
	}
}

// MrNote comments on an MR.
type MrNote struct{}

func (MrNote) Meta() contract.Meta {
	return contract.Meta{ID: "mr_note", Label: "评论 MR",
		Desc: "在 MR 下留一条评论（机器人报告审查结果/检查清单的落点）"}
}

func (MrNote) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("MR 编号"),
		field.Text("body").Label("评论内容").Desc("支持 GitLab Markdown"),
	}
}

func (MrNote) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("note_id").Label("评论 ID")}
}

// -- Issue --

// MrChange is one file's changes.
type MrChange struct {
	Path    string `sokel:"path" label:"文件路径"`
	OldPath string `sokel:"old_path,optional" label:"原路径" desc:"重命名时才不同"`
	New     bool   `sokel:"new_file" label:"新增"`
	Deleted bool   `sokel:"deleted_file" label:"删除"`
	Diff    string `sokel:"diff" label:"改动内容" desc:"统一 diff 片段"`
}

// MrChanges is an MR's changes.
type MrChanges struct{}

func (MrChanges) Meta() contract.Meta {
	return contract.Meta{ID: "mr_changes", Label: "MR 改动", TimeoutSec: 120,
		Desc: "取一个 MR 改了哪些文件、每个文件的 diff。**让 agent review MR 用的就是它**"}
}

func (MrChanges) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("MR 编号"),
		field.Int("max_diff_chars").Label("单文件 diff 上限(字符)").
			Desc("超出截断。默认 20000——一个几万行的迁移 diff 会把下游的上下文占满").Optional(),
	}
}

func (MrChanges) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("changes", []MrChange{}).Label("改动文件"),
		field.Int("count").Label("文件数"),
		field.String("title").Label("MR 标题"),
		field.String("source_branch").Label("源分支"),
		field.String("target_branch").Label("目标分支"),
		field.Bool("truncated").Label("有 diff 被截断").
			Desc("true = 至少一个文件的 diff 没给全，别据此下「改动很小」的结论"),
	}
}

// MrApprove approves an MR.
type MrApprove struct{}

func (MrApprove) Meta() contract.Meta {
	return contract.Meta{ID: "mr_approve", Label: "批准 MR",
		Desc: "给 MR 投一个批准票。**注意这是以凭证那个账号的身份批的**——别让它替人做决定"}
}

func (MrApprove) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{projectField(), field.Int("iid").Label("MR 编号")}
}

func (MrApprove) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("approvals_left").Label("还差几票").Optional(),
		field.Strings("approved_by").Label("已批准的人").Optional(),
	}
}

// MrUpdate edits an MR.
type MrUpdate struct{}

func (MrUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "mr_update", Label: "改 MR",
		Desc: "改标题/正文、加减标签、关闭或重开、转草稿。只填要改的，其余不动"}
}

func (MrUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("MR 编号"),
		field.Enum("state_event", field.Opt("close", "关闭"), field.Opt("reopen", "重开")).
			Label("开关状态").Optional(),
		field.Strings("add_labels").Label("加标签").Optional(),
		field.Strings("remove_labels").Label("去标签").Optional(),
		field.String("title").Label("标题").Optional(),
		field.Text("description").Label("正文").Optional(),
	}
}

func (MrUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("state").Label("当前状态"),
		field.Strings("labels").Label("当前标签"),
		field.String("url").Label("链接"),
	}
}

// IssuesList lists Issues.
type IssuesList struct{}

func (IssuesList) Meta() contract.Meta {
	return contract.Meta{ID: "issues_list", Label: "Issue 列表"}
}

func (IssuesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Enum("state", field.Opt("opened", "开着的"), field.Opt("closed", "已关闭"),
			field.Opt("all", "全部")).Label("状态").Default("opened"),
		field.String("labels").Label("标签过滤").Desc("逗号分隔").Optional(),
		pageField(),
	}
}

func (IssuesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("issues", []Issue{}).Label("Issue 列表"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").Desc("GitLab 报的**全部**条数（不是本页）。0 = 上游没给这个头（超大结果集时会省略）").Optional(),
		field.Bool("has_more").Label("还有下一页").Desc("**翻页看它,别看本页条数**——正好 50 条并不代表翻完了").Optional(),
	}
}

// Issue is a single Issue.
type Issue struct {
	IID       int      `sokel:"iid" label:"编号"`
	Title     string   `sokel:"title" label:"标题"`
	State     string   `sokel:"state" label:"状态"`
	Labels    []string `sokel:"labels,optional" label:"标签"`
	Author    string   `sokel:"author,optional" label:"作者"`
	WebURL    string   `sokel:"web_url" label:"页面地址"`
	CreatedAt string   `sokel:"created_at,optional" label:"创建时间"`
}

// IssueCreate creates an Issue.
type IssueCreate struct{}

func (IssueCreate) Meta() contract.Meta {
	return contract.Meta{ID: "issue_create", Label: "建 Issue",
		Desc: "创建 Issue（告警落单：监控发现问题 → 自动开单跟踪）"}
}

func (IssueCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("title").Label("标题"),
		field.Text("description").Label("描述").Desc("支持 GitLab Markdown").Optional(),
		field.String("labels").Label("标签").Desc("逗号分隔").Optional(),
		field.String("assignee").Label("指派给").Desc("用户名（不带 @）").Optional(),
	}
}

func (IssueCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("iid").Label("编号"),
		field.String("web_url").Label("页面地址"),
	}
}

// IssueNote comments on an Issue.
type IssueNote struct{}

func (IssueNote) Meta() contract.Meta {
	return contract.Meta{ID: "issue_note", Label: "评论 Issue"}
}

func (IssueNote) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("Issue 编号"),
		field.Text("body").Label("评论内容"),
	}
}

func (IssueNote) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("note_id").Label("评论 ID")}
}

// -- CI/CD --

// IssueUpdate edits an Issue.
//
// The closing-the-loop step: once an agent has handled it, it needs to be able to close the
// Issue and swap its labels. Removing the triggering label is the cleanest way to prevent
// repeat triggering -- far more reliable than having the workflow itself track "I already
// handled this Issue."
type IssueUpdate struct{}

func (IssueUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "issue_update", Label: "改 Issue",
		Desc: "改标题/正文、加减标签、指派、关闭或重开。只填要改的，其余不动"}
}

func (IssueUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("Issue 编号"),
		field.Enum("state_event", field.Opt("close", "关闭"), field.Opt("reopen", "重开")).
			Label("开关状态").Desc("留空 = 不改").Optional(),
		field.Strings("add_labels").Label("加标签").Optional(),
		field.Strings("remove_labels").Label("去标签").
			Desc("**处理完摘掉触发标签**，就不会被同一个 Issue 反复触发").Optional(),
		field.String("title").Label("标题").Desc("留空 = 不改").Optional(),
		field.Text("description").Label("正文").Desc("留空 = 不改").Optional(),
		field.String("assignee").Label("指派给").Desc("用户名；留空 = 不改").Optional(),
	}
}

func (IssueUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("state").Label("当前状态").Desc("opened / closed"),
		field.Strings("labels").Label("当前标签"),
		field.String("url").Label("链接"),
	}
}

// NoteUpdate edits a comment.
type NoteUpdate struct{}

func (NoteUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "note_update", Label: "改评论",
		Desc: "更新已有的一条评论。**长任务应该先回一条「处理中…」、跑完更新同一条**——" +
			"既不刷屏，也少一次「评论触发」的成环机会"}
}

func (NoteUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Enum("target", field.Opt("issue", "Issue"), field.Opt("merge_request", "MR")).
			Label("挂在哪").Optional(),
		field.Int("iid").Label("Issue / MR 编号"),
		field.Int("note_id").Label("评论 ID").Desc("「评论 Issue」的出参 note_id"),
		field.Text("body").Label("新内容"),
	}
}

func (NoteUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("note_id").Label("评论 ID")}
}

// PipelinesList lists pipelines.
type PipelinesList struct{}

func (PipelinesList) Meta() contract.Meta {
	return contract.Meta{ID: "pipelines_list", Label: "流水线列表",
		Desc: "最近的流水线与状态（CI 监控：定时查 → failed 触发告警）"}
}

func (PipelinesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Enum("status", field.Opt("running", "运行中"),
			field.Opt("success", "成功"), field.Opt("failed", "失败"),
			field.Opt("pending", "排队")).Label("状态过滤").Desc("留空 = 全部").Optional(),
		field.String("ref").Label("分支过滤").Optional(),
		pageField(),
	}
}

func (PipelinesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("pipelines", []Pipeline{}).Label("流水线列表"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").Desc("GitLab 报的**全部**条数（不是本页）。0 = 上游没给这个头（超大结果集时会省略）").Optional(),
		field.Bool("has_more").Label("还有下一页").Desc("**翻页看它,别看本页条数**——正好 50 条并不代表翻完了").Optional(),
	}
}

// Pipeline is a single pipeline.
type Pipeline struct {
	ID        int    `sokel:"id" label:"ID"`
	Status    string `sokel:"status" label:"状态"`
	Ref       string `sokel:"ref" label:"分支"`
	SHA       string `sokel:"sha,optional" label:"提交"`
	WebURL    string `sokel:"web_url" label:"页面地址"`
	CreatedAt string `sokel:"created_at,optional" label:"创建时间"`
}

// PipelineTrigger triggers a pipeline.
type PipelineTrigger struct{}

func (PipelineTrigger) Meta() contract.Meta {
	return contract.Meta{ID: "pipeline_trigger", Label: "触发流水线",
		Desc: "在指定分支起一条流水线，可带 CI 变量（定时构建/发布流程的起点）"}
}

func (PipelineTrigger) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.String("ref").Label("分支/tag"),
		field.Object("variables", "CI 变量键值对（如 {\"DEPLOY_ENV\":\"staging\"}），键名由 .gitlab-ci.yml 约定").
			Label("CI 变量").Optional(),
	}
}

func (PipelineTrigger) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("pipeline_id").Label("流水线 ID"),
		field.String("web_url").Label("页面地址"),
		field.String("status").Label("初始状态"),
	}
}

// PipelineJobs lists a pipeline's jobs.
type PipelineJobs struct{}

func (PipelineJobs) Meta() contract.Meta {
	return contract.Meta{ID: "pipeline_jobs", Label: "流水线 Job 列表",
		Desc: "看一条流水线里各 job 的状态（找出挂了哪个）"}
}

func (PipelineJobs) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("pipeline_id").Label("流水线 ID"),
	}
}

func (PipelineJobs) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("jobs", []Job{}).Label("Job 列表"),
		field.Int("count").Label("个数"),
	}
}

// Job is a single CI job.
type Job struct {
	ID       int     `sokel:"id" label:"ID" desc:"看日志认它"`
	Name     string  `sokel:"name" label:"名称"`
	Stage    string  `sokel:"stage" label:"阶段"`
	Status   string  `sokel:"status" label:"状态"`
	Duration float64 `sokel:"duration,optional" label:"耗时(秒)"`
	WebURL   string  `sokel:"web_url,optional" label:"页面地址"`
}

// JobLog views a job's log.
type JobLog struct{}

func (JobLog) Meta() contract.Meta {
	return contract.Meta{ID: "job_log", Label: "Job 日志", TimeoutSec: 60,
		Desc: "取一个 job 的日志尾部（失败排查：拿去给 LLM 总结失败原因再发群）"}
}

func (JobLog) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("job_id").Label("Job ID"),
		field.Int("tail_lines").Label("最后几行").Desc("默认 200，上限 2000").Optional(),
	}
}

func (JobLog) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("log").Label("日志"),
		field.Int("lines").Label("行数"),
	}
}

// -- Catch-all / health check --

// Call is the generic catch-all call.
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "通用调用", TimeoutSec: 60,
		Desc: "直调任意 GitLab REST v4 接口（token 由插件带上）。typed 没覆盖的能力走这里，" +
			"路径照官方 API 文档，如 /projects/:id/releases"}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("method", field.Opt("GET", "GET"), field.Opt("POST", "POST"),
			field.Opt("PUT", "PUT"), field.Opt("DELETE", "DELETE")).
			Label("HTTP 方法").Default("GET"),
		field.String("path").Label("接口路径").
			Desc("以 / 开头、不带 /api/v4 前缀，如 /projects/42/releases；:id 位置自己填实值"),
		field.Object("body", "请求体 JSON，键名照 GitLab API 文档；GET 时作为 query 参数").
			Label("参数").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Any("data", "GitLab 应答 JSON，形状随接口而变，由官方 API 文档定义").Label("应答"),
	}
}

// SearchHit is a single search result. Fields are filled in depending on scope -- a code hit
// gives a path and snippet, an Issue/MR hit gives an id and title.
type SearchHit struct {
	Title   string `sokel:"title,optional" label:"标题" desc:"Issue/MR 的标题；代码命中时是文件名"`
	Path    string `sokel:"path,optional" label:"文件路径" desc:"仅代码命中"`
	Line    int    `sokel:"line,optional" label:"起始行" desc:"仅代码命中"`
	Snippet string `sokel:"snippet,optional" label:"片段" desc:"代码命中的上下文，或 Issue/MR 的正文开头"`
	Iid     int    `sokel:"iid,optional" label:"编号" desc:"仅 Issue/MR 命中"`
	Ref     string `sokel:"ref,optional" label:"分支" desc:"仅代码命中"`
	URL     string `sokel:"url,optional" label:"链接"`
	Project string `sokel:"project,optional" label:"项目" desc:"跨项目搜索时用它分辨命中在哪个仓库"`
}

// Search performs a search.
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "搜索", TimeoutSec: 60,
		Desc: "搜代码 / Issue / MR / 提交。留空项目 = 全实例搜（agent 找「这个函数在哪儿用过」最直接的入口）"}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("query").Label("搜什么").Desc("关键词；代码搜索支持 GitLab 的高级语法（如 filename:*.go）"),
		field.Enum("scope",
			field.Opt("blobs", "代码"), field.Opt("issues", "Issue"),
			field.Opt("merge_requests", "MR"), field.Opt("commits", "提交"),
		).Label("搜哪一类").Default("blobs"),
		field.String("project").Label("限定项目").
			Desc("留空 = 全实例搜。**大实例上全局搜代码可能很慢**，能限定就限定").Optional(),
		pageField(),
	}
}

func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("results", []SearchHit{}).Label("命中"),
		field.Int("count").Label("本页条数"),
		field.Int("total").Label("总数").Optional(),
		field.Bool("has_more").Label("还有下一页").Optional(),
	}
}

// MrDiscussion comments on a specific line of an MR.
type MrDiscussion struct{}

func (MrDiscussion) Meta() contract.Meta {
	return contract.Meta{ID: "mr_discussion", Label: "MR 行级评论", TimeoutSec: 60,
		Desc: "在 MR 的某个文件某一行上开一条评论线程——review 意见要落到具体那一行才有用"}
}

func (MrDiscussion) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Int("iid").Label("MR 编号"),
		field.String("path").Label("文件路径").Desc("MR 改动里的路径，用「MR 改动」出参的 path"),
		field.Int("line").Label("行号").
			Desc("**新文件里的行号**（diff 右侧）。评论被删掉的行请用「原文件行号」"),
		field.Int("old_line").Label("原文件行号").Desc("评论被删除的行时填这个，同时把「行号」留空").Optional(),
		field.Text("body").Label("评论内容"),
	}
}

func (MrDiscussion) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("discussion_id").Label("线程 ID"),
		field.Int("note_id").Label("评论 ID"),
	}
}

// HealthCheck is the platform's standard credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "调 /user 验证 token 有效，并显示这个 token 是谁的"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("username").Label("用户名"),
		field.String("message").Label("说明"),
	}
}

// -- Events (polling event source, starts workflows) --
//
// GitLab has no long-polling/streaming API, so the event source polls: each poll cycle fetches
// the Events API + failed pipelines once. Latency = poll interval; for zero latency, use the
// platform's Webhook trigger (pointed at from the GitLab project's Settings -> Webhooks) -- the
// two approaches complement each other.
// Polling scope must be narrowed to the projects named in the credential -- watching the whole
// instance has unbounded volume.

func eventProjectField() contract.FieldSpec {
	return field.String("project").Label("项目").Desc("path 形态（group/name）")
}

// CommitPushed fires when there's a new commit.
type CommitPushed struct{}

func (CommitPushed) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "commit_pushed", Label: "有新提交"}
}

func (CommitPushed) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.String("ref").Label("分支"),
		field.String("commit_id").Label("提交 ID"),
		field.String("commit_title").Label("提交标题").Optional(),
		field.Int("commit_count").Label("提交数"),
		field.String("author").Label("推送人"),
		field.String("source").Label("来自哪条路").
			Desc("poll = 轮询 Events API / webhook = GitLab 推送。**raw 的形状由它决定**"),
		field.Any("raw", "上游原始载荷。**两条路的形状不一样**：source=poll 时是 Events API 的事件对象，"+
			"source=webhook 时是 Hook 的请求体。要下钻 raw 就必须先看 source，"+
			"否则换一种部署方式就会取到空——优先用上面那些已归一的字段").Label("原始事件"),
	}
}

// MrOpened fires when a new MR is opened.
type MrOpened struct{}

func (MrOpened) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "mr_opened", Label: "新 MR"}
}

func (MrOpened) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("iid").Label("MR 编号"),
		field.String("title").Label("标题"),
		field.String("author").Label("作者"),
		field.String("source").Label("来自哪条路").
			Desc("poll = 轮询 Events API / webhook = GitLab 推送。**raw 的形状由它决定**"),
		field.Any("raw", "上游原始载荷。**两条路的形状不一样**：source=poll 时是 Events API 的事件对象，"+
			"source=webhook 时是 Hook 的请求体。要下钻 raw 就必须先看 source，"+
			"否则换一种部署方式就会取到空——优先用上面那些已归一的字段").Label("原始事件"),
	}
}

// MrMerged fires when an MR is merged.
type MrMerged struct{}

func (MrMerged) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "mr_merged", Label: "MR 被合并"}
}

func (MrMerged) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("iid").Label("MR 编号"),
		field.String("title").Label("标题"),
		field.String("author").Label("合并人"),
		field.String("source").Label("来自哪条路").
			Desc("poll = 轮询 Events API / webhook = GitLab 推送。**raw 的形状由它决定**"),
		field.Any("raw", "上游原始载荷。**两条路的形状不一样**：source=poll 时是 Events API 的事件对象，"+
			"source=webhook 时是 Hook 的请求体。要下钻 raw 就必须先看 source，"+
			"否则换一种部署方式就会取到空——优先用上面那些已归一的字段").Label("原始事件"),
	}
}

// IssueOpened fires when a new Issue is created.
type IssueOpened struct{}

func (IssueOpened) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "issue_opened", Label: "新 Issue"}
}

func (IssueOpened) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("iid").Label("Issue 编号"),
		field.String("title").Label("标题"),
		field.Text("description").Label("正文").
			Desc("Issue 的正文。**要把 Issue 内容交给下游（如让 agent 照着做）时用的就是它**——" +
				"标题往往只是一句话").Optional(),
		field.Strings("labels").Label("标签").
			Desc("拿它当闸：只处理打了某个标签的 Issue，比「谁建的都跑」安全得多").Optional(),
		field.String("url").Label("Issue 链接").Optional(),
		field.String("author").Label("作者"),
		field.String("source").Label("来自哪条路").
			Desc("poll = 轮询 Events API / webhook = GitLab 推送。**raw 的形状由它决定**"),
		field.Any("raw", "上游原始载荷。**两条路的形状不一样**：source=poll 时是 Events API 的事件对象，"+
			"source=webhook 时是 Hook 的请求体。要下钻 raw 就必须先看 source，"+
			"否则换一种部署方式就会取到空——优先用上面那些已归一的字段").Label("原始事件"),
	}
}

// IssueLabeled fires when an Issue is labeled.
//
// A separate event rather than folded into issue_opened: the real-world usage pattern is
// "create the Issue first, take a look, then decide whether to dispatch work". But adding a
// label is an update on GitLab's side, not an open, so issue_opened will never fire again --
// which means the most natural path of "label it after creating it" is entirely dead (reported
// by an actual user).
type IssueLabeled struct{}

func (IssueLabeled) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "issue_labeled", Label: "Issue 被打标签"}
}

func (IssueLabeled) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("iid").Label("Issue 编号"),
		field.String("title").Label("标题"),
		field.Text("description").Label("正文").Optional(),
		field.Strings("added_labels").Label("本次新增的标签").
			Desc("**判「该不该派活」用它**：labels 是当前全部标签，加一个无关标签也会让它含 claude"),
		field.Strings("labels").Label("当前全部标签").Optional(),
		field.String("url").Label("Issue 链接").Optional(),
		field.String("author").Label("操作人").Desc("打标签的人（轮询路径取不到，为空）").Optional(),
	}
}

// IssueCommented fires when there's a new comment on an Issue.
//
// This is the most natural shape for "dispatching work": say something under the Issue and it
// continues, and it can carry on a multi-round back-and-forth from the previous conversation.
//
// It naturally forms a loop: a bot replying to the Issue is also a comment, and without a
// guard that's an infinite loop -- one comment triggers one run, and the run produces another
// comment. So the event must carry the original author and comment text, so the workflow can
// filter out the bot's own replies (recommended extra layer: only recognize comments starting
// with /cc).
type IssueCommented struct{}

func (IssueCommented) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "issue_commented", Label: "Issue 有新评论"}
}

func (IssueCommented) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("iid").Label("Issue 编号"),
		field.String("title").Label("Issue 标题").Optional(),
		field.Text("comment").Label("评论内容").Desc("这次说了什么——派活时把它当指令"),
		field.String("author").Label("评论人").
			Desc("**防自触发靠它**：机器人回复也是评论，工作流里务必把机器人账号滤掉，否则无限循环"),
		field.String("url").Label("评论链接").Optional(),
	}
}

// MrCommented fires when there's a new comment on an MR.
//
// Kept separate from issue_commented: the review use case wants the MR's id and branches, and
// on GitLab's side both go through the same Note event, distinguished by noteable_type.
type MrCommented struct{}

func (MrCommented) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "mr_commented", Label: "MR 有新评论"}
}

func (MrCommented) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("iid").Label("MR 编号"),
		field.String("title").Label("MR 标题").Optional(),
		field.Text("comment").Label("评论内容"),
		field.String("author").Label("评论人").Desc("**防自触发靠它**，机器人回复也是评论"),
		field.String("url").Label("评论链接").Optional(),
	}
}

// PipelineFailed fires when a pipeline fails.
type PipelineFailed struct{}

func (PipelineFailed) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pipeline_failed", Label: "流水线失败"}
}

func (PipelineFailed) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventProjectField(),
		field.Int("pipeline_id").Label("流水线 ID").Desc("接「Job 列表」「Job 日志」排障"),
		field.String("ref").Label("分支"),
		field.String("sha").Label("提交"),
		field.String("web_url").Label("页面地址"),
	}
}

// Events declares the common fields.
type Events struct{}

func (Events) CommonFields() []string { return []string{"project"} }

// Credential: instance address + PAT. The same scheme covers both self-hosted CE and gitlab.com.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("base_url").Label("GitLab 地址").
			Desc("自建实例填根地址（如 https://git.example.com）；留空 = gitlab.com").Optional(),
		field.Secret("token").Label("访问令牌").
			Desc("个人访问令牌（用户头像 → Preferences → Access Tokens），scope 至少 api；" +
				"建议专建一个机器人账号的 token 并按需给 scope"),
		field.Secret("webhook_secret").Label("Webhook Secret").
			Desc("平台代收 webhook 用：GitLab 项目 Settings→Webhooks 里填的 Secret token，" +
				"两边一致才收（防伪造）。不用 webhook 可留空").Optional(),
		field.Text("watch_projects").Label("事件盯哪些项目").
			Desc("path 形态逗号分隔（如 backend/server,infra/deploy）。**填了才启动事件源**：" +
				"新提交/新 MR/合并/新 Issue/流水线失败 会触发工作流（轮询，约 1 分钟延迟；" +
				"要零延迟用 GitLab Webhook 指平台的 Webhook 触发器）").Optional(),
	}
}
