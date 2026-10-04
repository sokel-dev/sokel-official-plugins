package schema

// Package schema: Projects V2 (boards) + search + raw-call fallback + health check.
//
// **Projects V2 is GraphQL-only**: GitHub has retired the REST endpoints for classic Projects.
// These four operations go through GraphQL in client.go, and the distinction is invisible at
// the contract level (as it should be — callers don't care about the transport).
//
// Boards are the most commonly used half of "maintaining a project": a triage bot drops new
// Issues into the board's Triage column, someone claiming one moves it to In Progress, and
// merging moves it to Done. So besides create/delete/list, there must be a **field-update**
// operation (moving a card between columns is really just changing the single-select Status
// field to a different value).

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// ProjectsList lists boards.
type ProjectsList struct{}

func (ProjectsList) Meta() contract.Meta {
	return contract.Meta{ID: "projects_list", Label: "看板列表",
		Desc: "列出某个用户或组织的 Projects V2 看板"}
}

func (ProjectsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("owner").Label("用户或组织").Desc("看板挂在账号下，不是仓库下"),
		field.Bool("is_org").Label("是组织").
			Desc("**组织与个人是两个不同的 GraphQL 查询**，认错会返回空列表而不是报错。留空自动试两次").Optional(),
	}
}

func (ProjectsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("projects", []Project{}).Label("看板列表"),
		countField(),
	}
}

// Project is a single board.
type Project struct {
	Number int    `sokel:"number" label:"编号" desc:"看板地址末尾那个数字，其他看板操作用它"`
	ID     string `sokel:"id" label:"节点 ID" desc:"GraphQL 的全局 ID（PVT_ 开头）；加卡片要它"`
	Title  string `sokel:"title" label:"标题"`
	Closed bool   `sokel:"closed" label:"已关闭"`
	URL    string `sokel:"url" label:"页面地址"`
	Items  int    `sokel:"items" label:"卡片数"`
}

// ProjectItemsList lists a board's items (cards).
type ProjectItemsList struct{}

func (ProjectItemsList) Meta() contract.Meta {
	return contract.Meta{ID: "project_items_list", Label: "看板卡片",
		Desc: "列出看板里的卡片及其字段值（Status 在哪一列就在字段里）", TimeoutSec: 60}
}

func (ProjectItemsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("owner").Label("用户或组织"),
		field.Int("number").Label("看板编号").Desc("看板地址末尾那个数字"),
		field.Bool("is_org").Label("是组织").Desc("留空自动试").Optional(),
		field.Int("limit").Label("最多取多少").Desc("默认 50，上限 100（GraphQL 单页上限）").Optional(),
	}
}

func (ProjectItemsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []ProjectItem{}).Label("卡片列表"),
		countField(), hasMoreField(),
	}
}

// ProjectItem is a single card on a board.
type ProjectItem struct {
	ID     string   `sokel:"id" label:"卡片 ID" desc:"PVTI_ 开头；改字段要它"`
	Type   string   `sokel:"type" label:"类型" desc:"ISSUE / PULL_REQUEST / DRAFT_ISSUE"`
	Title  string   `sokel:"title" label:"标题"`
	Number int      `sokel:"number" label:"Issue/PR 编号" desc:"草稿卡片为 0"`
	Repo   string   `sokel:"repo" label:"所属仓库" desc:"草稿卡片为空"`
	State  string   `sokel:"state" label:"状态"`
	Status string   `sokel:"status" label:"所在列" desc:"Status 单选字段的值，也就是看板上的列名"`
	Fields []string `sokel:"fields" label:"所有字段" desc:"形如 Status=In Progress，含自定义字段"`
	URL    string   `sokel:"url" label:"页面地址"`
}

// ProjectItemAdd adds an Issue/PR to a board.
type ProjectItemAdd struct{}

func (ProjectItemAdd) Meta() contract.Meta {
	return contract.Meta{ID: "project_item_add", Label: "加进看板",
		Desc: "把一个 Issue 或 PR 加进看板（triage 机器人的第一步）"}
}

func (ProjectItemAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("owner").Label("用户或组织"),
		field.Int("project_number").Label("看板编号"),
		repoField(),
		numberField("Issue 或 PR 编号", ""),
		field.Bool("is_org").Label("是组织").Desc("留空自动试").Optional(),
	}
}

func (ProjectItemAdd) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("item_id").Label("卡片 ID").Desc("接着用它挪列"),
		field.Bool("already").Label("之前就在看板里").
			Desc("GitHub 对重复添加返回同一张卡片，不报错——机器人重跑安全"),
	}
}

// ProjectItemFieldSet changes a card's field value (i.e. moves it between columns).
type ProjectItemFieldSet struct{}

func (ProjectItemFieldSet) Meta() contract.Meta {
	return contract.Meta{ID: "project_item_field_set", Label: "改看板字段",
		Desc: "改卡片的某个字段值；把 Status 改成别的值就是「挪到另一列」"}
}

func (ProjectItemFieldSet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("owner").Label("用户或组织"),
		field.Int("project_number").Label("看板编号"),
		field.String("item_id").Label("卡片 ID").Desc("PVTI_ 开头，从「看板卡片」或「加进看板」拿"),
		field.String("field").Label("字段名").Desc("如 Status、Priority；大小写要与看板上一致").Default("Status"),
		field.String("value").Label("目标值").
			Desc("单选字段填选项名（如 In Progress）；文本/数字字段填字面值。" +
				"**选项名必须与看板上的完全一致**——对不上会明确报错并列出可选项，不会静默不动"),
		field.Bool("is_org").Label("是组织").Desc("留空自动试").Optional(),
	}
}

func (ProjectItemFieldSet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("updated").Label("已更新"),
		field.String("field").Label("字段名"),
		field.String("value").Label("现在的值"),
	}
}

// —— Search and fallback ——

// Search runs a search.
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "搜索",
		Desc: "搜 Issue/PR、代码或仓库（GitHub 搜索语法原样传）", TimeoutSec: 60}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("type", field.Opt("issues", "Issue 与 PR"), field.Opt("code", "代码"),
			field.Opt("repositories", "仓库"), field.Opt("commits", "提交")).
			Label("搜什么").Default("issues").Optional(),
		field.String("query").Label("搜索式").
			Desc("GitHub 搜索语法，如 `repo:owner/name is:open label:bug`、`org:foo is:pr review:required`。" +
				"**限定符必须写在这里**——没有 repo: 的话是全站搜"),
		field.Int("limit").Label("最多返回").Desc("默认 30，上限 100").Optional(),
	}
}

func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("results", []SearchHit{}).Label("命中"),
		countField(),
		field.Int("total").Label("总命中数").
			Desc("**搜索接口是少数会给总数的**（上限 1000，超过按 1000 算）"),
		field.Bool("incomplete").Label("结果不完整").
			Desc("true = GitHub 搜索超时了只返回了部分——**别把它当成「就这么多」**"),
	}
}

// SearchHit is a single search hit.
type SearchHit struct {
	Title   string `sokel:"title" label:"标题"`
	Repo    string `sokel:"repo" label:"仓库"`
	Number  int    `sokel:"number" label:"编号" desc:"仓库/代码搜索时为 0"`
	State   string `sokel:"state" label:"状态"`
	Author  string `sokel:"author" label:"作者"`
	Path    string `sokel:"path" label:"文件路径" desc:"只有代码搜索有"`
	Snippet string `sokel:"snippet" label:"片段" desc:"截断过"`
	URL     string `sokel:"url" label:"页面地址"`
}

// Call is the raw-call fallback.
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "通用调用",
		Desc: "直调任意 GitHub REST 接口——上面没覆盖到的都走这里", TimeoutSec: 60}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("method", field.Opt("GET"), field.Opt("POST"), field.Opt("PATCH"),
			field.Opt("PUT"), field.Opt("DELETE")).Label("方法").Default("GET").Optional(),
		field.String("path").Label("接口路径").
			Desc("以 / 开头、**不带** https://api.github.com 前缀，如 /repos/owner/name/topics"),
		field.Any("body", "任意 GitHub 接口的请求体，形状由目标接口决定").Label("请求体").Optional(),
		field.Json("query", map[string]string{}).Label("查询参数").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("status").Label("HTTP 状态码"),
		field.Any("data", "上游原样返回，形状由目标接口决定").Label("响应体"),
		field.Bool("has_more").Label("还有下一页").Desc("从 Link 头判断").Optional(),
	}
}

// HealthCheck is the health check. This is what the "Test" button on the platform's
// credentials page calls.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "健康检查",
		Desc: "验证令牌可用，并回报速率余量与令牌权限范围", Internal: true}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("是否可用"),
		field.String("message").Label("说明"),
		field.String("login").Label("令牌属于谁"),
		field.Strings("scopes").Label("令牌权限范围").
			Desc("经典令牌才有；细粒度令牌这里是空的（GitHub 不在头里回报），不代表没权限").Optional(),
		field.Int("rate_remaining").Label("本小时剩余调用次数").Optional(),
	}
}
