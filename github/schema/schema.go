// Package schema declares the GitHub plugin's operation, event and credential contracts.
//
// Positioning: GitHub project-maintenance automation — **works with both github.com and GitHub
// Enterprise Server** (the instance address goes in the credential). The operation surface
// **targets the various GitHub tools/bots** (the Probot family, Renovate, stale-bot, reviewdog,
// Mergify, release-drafter, ChatOps) rather than wrapping the REST endpoints one by one: whether
// this set of operations can rebuild those bots is the acceptance bar for this contract.
//
// Five GitHub-specific quirks that the operation design revolves around (each one maps to a
// real pitfall):
//
//   - **Issues and PRs share the same number space, and /issues returns PRs along with them.**
//     This is GitHub API's most classic trap: carrying over a GitLab mental model when listing
//     Issues gets you a pile of PRs mixed in, with no error at all. So IssuesList **excludes
//     PRs by default** (include_prs must be turned on explicitly), and the output exposes is_pr
//     so it's visible.
//   - **The comment endpoint is shared**: posting a regular comment on a PR goes through the
//     Issue comment endpoint. So issue_comment works just as well on a PR, and pr_comment is
//     just an alias for the same thing — we don't open a second one, to avoid the two operations'
//     behavior drifting apart. What's genuinely different is the **inline comment** (on a
//     specific diff line), which is the Review endpoint, in pr_review.
//   - **Writing a file requires the blob sha**: the Contents API returns 422 when updating an
//     existing file without a sha, and the error text won't tell you "you forgot the sha".
//     FileWrite does get-then-put internally so the caller doesn't need to know this.
//   - **Pagination has no total count**: GitHub doesn't return an X-Total-style header, only
//     rel="next" in the Link header. So the output gives has_more instead of total — **check
//     that for paging**, since exactly 30 results doesn't mean you're done.
//   - **Projects V2 is GraphQL-only**: the REST endpoints for classic Projects have been retired.
//     The four board-related operations go through GraphQL, the rest go through REST; that line
//     lives in client.go and is invisible at the contract level (as it should be).
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— Shared fields ——

// repoField identifies a repository. GitHub uses owner/repo throughout; there's no GitLab-style
// numeric-ID alternative.
func repoField() contract.FieldSpec {
	return field.String("repo").Label("仓库").
		Desc("owner/repo 形态（如 sokel-dev/sokel-plugin-sdk，即仓库地址里域名后那段）")
}

func pageField() contract.FieldSpec {
	return field.Int("page").Label("页码").Desc("默认 1；每页 50 条").Optional()
}

func numberField(label, desc string) contract.FieldSpec {
	return field.Int("number").Label(label).Desc(desc)
}

// hasMoreField is the paging indicator. GitHub doesn't give a total count, so this is the only
// reliable way to know whether there's more.
func hasMoreField() contract.FieldSpec {
	return field.Bool("has_more").Label("还有下一页").
		Desc("**翻页看它**——GitHub 不回总数，正好 50 条并不代表翻完了").Optional()
}

func countField() contract.FieldSpec {
	return field.Int("count").Label("本页条数")
}

// —— Repositories ——

// ReposList lists repositories.
type ReposList struct{}

func (ReposList) Meta() contract.Meta {
	return contract.Meta{ID: "repos_list", Label: "仓库列表",
		Desc: "列出某个用户或组织的仓库；留空则列当前令牌账号有权限的仓库"}
}

func (ReposList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("owner").Label("用户或组织").
			Desc("留空 = 当前令牌自己的仓库（含私有）").Optional(),
		field.Enum("type", field.Opt("all", "全部"), field.Opt("owner", "自己拥有的"),
			field.Opt("member", "作为成员参与的"), field.Opt("public", "公开的"),
			field.Opt("private", "私有的")).Label("范围").Default("all").Optional(),
		field.Enum("sort", field.Opt("updated", "最近更新"), field.Opt("created", "创建时间"),
			field.Opt("pushed", "最近推送"), field.Opt("full_name", "名字")).
			Label("排序").Default("updated").Optional(),
		pageField(),
	}
}

func (ReposList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("repos", []Repo{}).Label("仓库列表"),
		countField(), hasMoreField(),
	}
}

// Repo is a single repository.
type Repo struct {
	FullName      string `sokel:"full_name" label:"全名" desc:"owner/repo 形态，其他操作的 repo 字段用它"`
	Name          string `sokel:"name" label:"名字"`
	Owner         string `sokel:"owner" label:"归属"`
	Description   string `sokel:"description" label:"简介"`
	Private       bool   `sokel:"private" label:"私有"`
	Fork          bool   `sokel:"fork" label:"是 fork"`
	Archived      bool   `sokel:"archived" label:"已归档" desc:"归档仓库只读，写操作会 403"`
	DefaultBranch string `sokel:"default_branch" label:"默认分支"`
	Stars         int    `sokel:"stars" label:"星标数"`
	OpenIssues    int    `sokel:"open_issues" label:"未关闭 Issue 数" desc:"**含 PR**——GitHub 这个计数把 PR 也算进去"`
	URL           string `sokel:"url" label:"页面地址"`
	UpdatedAt     string `sokel:"updated_at" label:"更新时间"`
}

// RepoGet gets repository details.
type RepoGet struct{}

func (RepoGet) Meta() contract.Meta {
	return contract.Meta{ID: "repo_get", Label: "仓库详情", Desc: "取单个仓库的元信息"}
}

func (RepoGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{repoField()}
}

func (RepoGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Json("repo", Repo{}).Label("仓库"),
		field.Strings("topics").Label("主题标签").Optional(),
	}
}

// FileGet reads a file.
type FileGet struct{}

func (FileGet) Meta() contract.Meta {
	return contract.Meta{ID: "file_get", Label: "读文件", Desc: "读取仓库里某个文件的内容"}
}

func (FileGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("path").Label("文件路径").Desc("仓库内相对路径，如 README.md 或 src/main.go"),
		field.String("ref").Label("分支或提交").Desc("留空 = 默认分支").Optional(),
	}
}

func (FileGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("content").Label("内容"),
		field.String("sha").Label("blob sha").
			Desc("**改这个文件时要把它传给「写文件」**（虽然写文件也会自己去取一次）"),
		field.Int("size").Label("字节数"),
		field.String("url").Label("页面地址"),
	}
}

// FileWrite writes a file.
type FileWrite struct{}

func (FileWrite) Meta() contract.Meta {
	return contract.Meta{ID: "file_write", Label: "写文件",
		Desc: "新建或修改一个文件并提交（自动判断新建/更新）", TimeoutSec: 60}
}

func (FileWrite) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("path").Label("文件路径"),
		field.Text("content").Label("内容").Desc("完整内容（不是补丁）——这个接口是整文件覆盖"),
		field.String("message").Label("提交信息"),
		field.String("branch").Label("分支").Desc("留空 = 默认分支；**分支必须已存在**，要新分支先用「建分支」").Optional(),
	}
}

func (FileWrite) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("commit_sha").Label("提交 sha"),
		field.String("sha").Label("新的 blob sha"),
		field.Bool("created").Label("是新建").Desc("false = 覆盖了已有文件"),
		field.String("url").Label("页面地址"),
	}
}

// BranchCreate creates a branch. This is step one for "PR-opening bots" like Renovate / backport
// tools.
type BranchCreate struct{}

func (BranchCreate) Meta() contract.Meta {
	return contract.Meta{ID: "branch_create", Label: "建分支",
		Desc: "从某个分支或提交拉一个新分支（机器人开 PR 前的第一步）"}
}

func (BranchCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("branch").Label("新分支名").Desc("不带 refs/heads/ 前缀"),
		field.String("from").Label("基于").
			Desc("分支名或提交 sha；留空 = 默认分支的最新提交").Optional(),
	}
}

func (BranchCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("branch").Label("分支名"),
		field.String("sha").Label("指向的提交"),
		field.Bool("existed").Label("已存在").
			Desc("true = 分支本来就有，没有新建（**不报错**：机器人重跑一次不该炸）"),
	}
}

// BranchesList lists branches.
type BranchesList struct{}

func (BranchesList) Meta() contract.Meta {
	return contract.Meta{ID: "branches_list", Label: "分支列表", Desc: "列出仓库的分支"}
}

func (BranchesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{repoField(), pageField()}
}

func (BranchesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("branches", []Branch{}).Label("分支列表"),
		countField(), hasMoreField(),
	}
}

// Branch is a single branch.
type Branch struct {
	Name      string `sokel:"name" label:"名字"`
	SHA       string `sokel:"sha" label:"最新提交"`
	Protected bool   `sokel:"protected" label:"受保护" desc:"受保护分支上直接 push 会被拒，要走 PR"`
}

// CommitsList lists commits.
type CommitsList struct{}

func (CommitsList) Meta() contract.Meta {
	return contract.Meta{ID: "commits_list", Label: "提交列表", Desc: "列出某个分支上的提交"}
}

func (CommitsList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		repoField(),
		field.String("branch").Label("分支").Desc("留空 = 默认分支").Optional(),
		field.String("path").Label("只看某个文件").Desc("只返回改动了该路径的提交").Optional(),
		field.String("since").Label("起始时间").Desc("ISO8601（如 2026-08-01T00:00:00Z）").Optional(),
		pageField(),
	}
}

func (CommitsList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("commits", []Commit{}).Label("提交列表"),
		countField(), hasMoreField(),
	}
}

// Commit is a single commit.
type Commit struct {
	SHA     string `sokel:"sha" label:"sha"`
	Message string `sokel:"message" label:"提交信息"`
	Author  string `sokel:"author" label:"作者"`
	Date    string `sokel:"date" label:"时间"`
	URL     string `sokel:"url" label:"页面地址"`
}
