package schema

// Issue 域。GitHub 的 Issue 与 PR 共用编号空间与评论接口——这里的操作对 PR 同样有效，
// 但 IssuesList 默认剔掉 PR（见包顶注第一条）。
//
// 覆盖的机器人形态：triage/labeler（打标签、指派、进里程碑）、stale-bot（按时间筛 → 评论 → 关闭）、
// welcome-bot（按作者筛历史）。

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// IssuesList Issue 列表。
type IssuesList struct{}

func (IssuesList) Meta() contract.Meta {
	return contract.Meta{ID: "issues_list", Label: "Issue 列表",
		Desc: "按状态/标签/指派人/更新时间筛 Issue（stale-bot 那套筛选都在这）"}
}

func (IssuesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.Enum("state", field.Opt("open", "未关闭"), field.Opt("closed", "已关闭"),
			field.Opt("all", "全部")).Label("状态").Default("open").Optional(),
		field.Strings("labels").Label("标签").Desc("**同时**带上这些标签的才返回（是与不是或）").Optional(),
		field.String("assignee").Label("指派给").
			Desc("用户名；填 none = 没人认领的，填 * = 任何人认领的").Optional(),
		field.String("creator").Label("创建者").Desc("用户名").Optional(),
		field.String("milestone").Label("里程碑").Desc("里程碑编号或标题；none = 不在任何里程碑里").Optional(),
		field.String("since").Label("这个时间之后更新的").
			Desc("ISO8601。**stale-bot 反过来用**：要「很久没动的」就按 updated 升序取第一页").Optional(),
		field.Enum("sort", field.Opt("created", "创建时间"), field.Opt("updated", "更新时间"),
			field.Opt("comments", "评论数")).Label("排序").Default("created").Optional(),
		field.Enum("direction", field.Opt("desc", "倒序"), field.Opt("asc", "正序")).
			Label("方向").Default("desc").Optional(),
		field.Bool("include_prs").Label("把 PR 也算进来").
			Desc("GitHub 的 Issue 接口**本来就会返回 PR**；默认剔掉，打开这个开关才保留").Optional(),
		pageField(),
	}
}

func (IssuesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("issues", []Issue{}).Label("Issue 列表"),
		countField(), hasMoreField(),
		field.Int("dropped_prs").Label("剔掉的 PR 数").
			Desc("本页里因为是 PR 而被剔掉的条数（include_prs 打开时恒为 0）").Optional(),
	}
}

// Issue 一条 Issue（PR 也是这个形状）。
type Issue struct {
	Number    int      `sokel:"number" label:"编号"`
	Title     string   `sokel:"title" label:"标题"`
	Body      string   `sokel:"body" label:"正文" desc:"超长会截断"`
	State     string   `sokel:"state" label:"状态" desc:"open / closed"`
	IsPR      bool     `sokel:"is_pr" label:"其实是 PR" desc:"GitHub 把 PR 也当 Issue 返回——判据看这个，别看标题"`
	Author    string   `sokel:"author" label:"创建者"`
	Assignees []string `sokel:"assignees" label:"指派给"`
	Labels    []string `sokel:"labels" label:"标签"`
	Milestone string   `sokel:"milestone" label:"里程碑"`
	Comments  int      `sokel:"comments" label:"评论数"`
	URL       string   `sokel:"url" label:"页面地址"`
	CreatedAt string   `sokel:"created_at" label:"创建时间"`
	UpdatedAt string   `sokel:"updated_at" label:"更新时间"`
	ClosedAt  string   `sokel:"closed_at" label:"关闭时间" desc:"未关闭时为空"`
}

// IssueGet Issue 详情。
type IssueGet struct{}

func (IssueGet) Meta() contract.Meta {
	return contract.Meta{ID: "issue_get", Label: "Issue 详情",
		Desc: "取单条 Issue（或 PR）的完整信息，可带上评论"}
}

func (IssueGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("编号", "Issue 或 PR 的编号（两者共用编号空间）"),
		field.Bool("with_comments").Label("带上评论").Desc("默认只取正文").Optional(),
	}
}

func (IssueGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Json("issue", Issue{}).Label("Issue"),
		field.Array("comments", []Comment{}).Label("评论").Optional(),
	}
}

// Comment 一条评论。
type Comment struct {
	ID        int    `sokel:"id" label:"评论 ID" desc:"改/删评论、加表情都要它"`
	Author    string `sokel:"author" label:"作者"`
	Body      string `sokel:"body" label:"内容"`
	URL       string `sokel:"url" label:"页面地址"`
	CreatedAt string `sokel:"created_at" label:"时间"`
}

// IssueCreate 开 Issue。
type IssueCreate struct{}

func (IssueCreate) Meta() contract.Meta {
	return contract.Meta{ID: "issue_create", Label: "开 Issue", Desc: "新建一条 Issue"}
}

func (IssueCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("title").Label("标题"),
		field.Text("body").Label("正文").Desc("markdown").Optional(),
		field.Strings("labels").Label("标签").
			Desc("**标签必须已存在**，否则 422；不确定就先用「建标签」").Optional(),
		field.Strings("assignees").Label("指派给").Desc("用户名；对仓库没写权限的人会被静默忽略").Optional(),
		field.String("milestone").Label("里程碑").Desc("编号或标题").Optional(),
	}
}

func (IssueCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("number").Label("编号"),
		field.String("url").Label("页面地址"),
	}
}

// IssueUpdate 改 Issue（含开关）。
type IssueUpdate struct{}

func (IssueUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "issue_update", Label: "改 Issue",
		Desc: "改标题/正文/状态/里程碑；关闭与重开也走这里（stale-bot 的最后一步）"}
}

func (IssueUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("编号", "Issue 或 PR 编号"),
		field.String("title").Label("标题").Desc("留空 = 不改").Optional(),
		field.Text("body").Label("正文").Desc("留空 = 不改").Optional(),
		field.Enum("state", field.Opt("open", "重开"), field.Opt("closed", "关闭")).
			Label("状态").Desc("留空 = 不改").Optional(),
		field.Enum("state_reason", field.Opt("completed", "已完成"), field.Opt("not_planned", "不做了"),
			field.Opt("reopened", "重开")).Label("关闭原因").
			Desc("**stale-bot 关闭时应给 not_planned**——不给的话默认算「已完成」，统计会失真").Optional(),
		field.String("milestone").Label("里程碑").Desc("编号或标题；填 none 移出里程碑").Optional(),
	}
}

func (IssueUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("number").Label("编号"),
		field.String("state").Label("当前状态"),
		field.String("url").Label("页面地址"),
	}
}

// IssueComment 发评论。对 PR 同样有效（GitHub 共用评论接口）。
type IssueComment struct{}

func (IssueComment) Meta() contract.Meta {
	return contract.Meta{ID: "issue_comment", Label: "发评论",
		Desc: "在 Issue 或 PR 下发一条评论（GitHub 两者共用评论接口，PR 也用这个）"}
}

func (IssueComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("编号", "Issue 或 PR 编号"),
		field.Text("body").Label("内容").Desc("markdown"),
	}
}

func (IssueComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("comment_id").Label("评论 ID").Desc("要给它加表情或事后编辑就留着这个"),
		field.String("url").Label("页面地址"),
	}
}

// IssueLabel 打/摘标签。labeler 机器人的主操作。
type IssueLabel struct{}

func (IssueLabel) Meta() contract.Meta {
	return contract.Meta{ID: "issue_label", Label: "打标签",
		Desc: "给 Issue 或 PR 加、去、或整体设定标签"}
}

func (IssueLabel) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("编号", "Issue 或 PR 编号"),
		field.Enum("mode", field.Opt("add", "追加"), field.Opt("remove", "移除"),
			field.Opt("set", "整体替换")).Label("方式").Default("add").Optional(),
		field.Strings("labels").Label("标签").
			Desc("**追加时标签必须已存在**（GitHub 会自动建？不会，422）；set 会把没列出的都摘掉"),
	}
}

func (IssueLabel) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("labels").Label("现在的标签"),
	}
}

// IssueAssign 指派。
type IssueAssign struct{}

func (IssueAssign) Meta() contract.Meta {
	return contract.Meta{ID: "issue_assign", Label: "指派",
		Desc: "给 Issue 或 PR 加/去指派人"}
}

func (IssueAssign) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		numberField("编号", "Issue 或 PR 编号"),
		field.Enum("mode", field.Opt("add", "追加"), field.Opt("remove", "移除")).
			Label("方式").Default("add").Optional(),
		field.Strings("assignees").Label("用户名").
			Desc("**对仓库没有写权限的人会被静默忽略**——加完请看出参核对，别假设成功"),
	}
}

func (IssueAssign) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("assignees").Label("现在的指派人").
			Desc("拿它和你传的比对——少了谁就是那个人没有仓库权限"),
	}
}
