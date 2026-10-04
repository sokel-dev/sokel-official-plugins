// Package schema 声明 claude-code 插件的操作与凭证契约。
//
// 定位：把**本机装的 Claude Code** 当成工作流里的一个执行体——给它一个 GitLab 项目
// 和一句任务，它在真实工作树里改代码，过程实时回传，结论与改动清单作为出参进下游节点。
//
// 为什么是本地 CC 而不是云端托管 agent：自建 GitLab 多在内网，云端沙箱够不着仓库。
// 插件跑在能访问内网、装了 claude 的机器上，这条约束是这个插件的前提（与 kubernetes 插件同类）。
//
// 四条约定，操作设计围着它们转：
//
//   - **默认不推代码**：改完只回 diff 与结论，push 是显式开关。让一个 agent 默认拥有
//     push 权限，是那种出事之后才会被发现的默认值。
//   - **成本必须有硬顶**：CC 的 --max-budget-usd 直接透出成契约字段。跑飞的 agent
//     不该由账单来告诉你。
//   - **过程是一等产出**：操作声明 Stream，边跑边把 CC 的文字推成部分产出；
//     跑完的 log 也照样在出参里。「跑了十分钟只有执行中三个字」是这类节点最难查的形态。
//   - **工作树按分支复用**：同一项目同一分支复用一份 git worktree（下次只 fetch 不重 clone），
//     不同分支各一份——两个任务共用一个工作树必然互相踩。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

func projectField() contract.FieldSpec {
	return field.String("project").Label("项目").
		Desc("GitLab 项目路径（group/name）或数字 ID——与 gitlab 插件同一形态")
}

// —— 任务 ——

// RunTask 跑一个任务。
type RunTask struct{}

func (RunTask) Meta() contract.Meta {
	return contract.Meta{ID: "run_task", Label: "执行任务", Stream: true, TimeoutSec: 3600,
		Desc: "在项目工作树里让 Claude Code 干一件事：改代码、查问题、写测试。过程实时回传，结论与改动清单进下游节点"}
}

func (RunTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		projectField(),
		field.Text("task").Label("任务").
			Desc("要它做什么，越具体越好。把验收标准也写进去（「跑通 go test ./internal/api」）比只说「修一下」有效得多"),
		field.String("base_branch").Label("基线分支").Desc("从哪个分支开工；留空 = 仓库默认分支").Optional(),
		field.String("branch").Label("工作分支").
			Desc("在这个分支上改。留空 = 自动生成 cc/<时间戳>。**同名分支复用同一个工作树**（增量更快）").Optional(),
		field.Bool("push").Label("推送分支").
			Desc("默认关：只回 diff 与结论，代码留在工作树里。打开才 git push——之后可以接 gitlab 插件的「建 MR」").Optional(),
		field.Number("max_budget_usd").Label("成本上限(美元)").
			Desc("超了就停（CC 原生支持）。留空 = 不限，**不建议**：agent 跑飞时这是唯一的刹车").Optional(),
		field.String("model").Label("模型").Desc("如 opus / sonnet，或完整模型名。留空用 CC 默认").Optional(),
		field.Enum("effort", field.Opt("low", "低"), field.Opt("medium", "中"), field.Opt("high", "高")).
			Label("思考力度").Desc("留空用 CC 默认").Optional(),
		field.Strings("allowed_tools").Label("只允许这些工具").
			Desc(`留空 = 用权限模式决定。形态如 Read、Edit、Bash(git:*)——注意是冒号不是空格`).Optional(),
		field.Strings("disallowed_tools").Label("禁用这些工具").Desc("同上形态，优先于允许清单").Optional(),
		field.Enum("permission_mode",
			field.Opt("acceptEdits", "自动接受改文件（默认）"),
			field.Opt("dontAsk", "只允许清单内的工具"),
			field.Opt("plan", "只出方案不动手"),
			field.Opt("bypassPermissions", "跳过全部权限检查（危险）"),
		).Label("权限模式").
			Desc("默认 acceptEdits：能改文件、常规文件命令放行。**bypassPermissions 等于把这台机器交出去**，只在隔离容器里用").Optional(),
		field.Text("system_prompt").Label("追加系统提示").Desc("附加在 CC 默认系统提示之后（团队规范、禁改目录…）").Optional(),
	}
}

func (RunTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Text("conclusion").Label("结论").Desc("CC 的最终答复——「它做了什么、结果如何」"),
		field.Text("log").Label("过程").Desc("整段执行过程（与流式推的是同一份内容）"),
		field.Strings("changed_files").Label("改动文件"),
		field.Text("diff").Label("改动内容").Desc("git diff（超长会截断，完整改动在工作树/分支里）"),
		field.String("branch").Label("分支"),
		field.Bool("pushed").Label("已推送"),
		field.Number("cost_usd").Label("花费(美元)"),
		field.Int("turns").Label("轮数"),
		field.String("session_id").Label("会话 ID").Desc("给「继续任务」用：同一会话追加指令，CC 记得上下文"),
		field.String("worktree").Label("工作树路径").
			Desc("代码改在这个目录里。**人要接管就 cd 到这儿**再 claude --resume <会话 ID>——" +
				"前提是同一台机器、同一个系统用户（会话文件是 0600）"),
		field.Strings("denied_tools").Label("被权限拦下的工具").
			Desc("非空说明它想做但没被允许的事——任务没完成时先看这里，多半是权限配窄了"),
	}
}

// ResumeTask 继续一个任务。
type ResumeTask struct{}

func (ResumeTask) Meta() contract.Meta {
	return contract.Meta{ID: "resume_task", Label: "继续任务", Stream: true, TimeoutSec: 3600,
		Desc: "在上一次的会话里追加一句指令（它记得之前做过什么），在同一个工作树上接着改"}
}

func (ResumeTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("session_id").Label("会话 ID").
			Desc("要接管的会话。可以是本插件「执行任务」的出参，**也可以是人在这台机器上自己跑出来的**——" +
				"CC 按 id 在整机范围内找，不限目录"),
		field.Text("task").Label("追加指令").Desc("如「测试没跑通，修一下」「把日志加上」"),
		field.String("worktree").Label("直接指定工作目录").
			Desc("绝对路径。**接管插件之外的会话时填这里**（如人在 /home/me/repo 里起的），" +
				"填了就不再按项目/分支找工作树。需要凭证里先开「允许外部目录」").Optional(),
		field.String("project").Label("项目").Desc("走插件自己的工作树时要给；填了「直接指定工作目录」就不用").Optional(),
		field.String("branch").Label("工作分支").Desc("同上——上一次那个分支，工作树按它找").Optional(),
		field.Bool("push").Label("推送分支").Optional(),
		field.Number("max_budget_usd").Label("成本上限(美元)").Optional(),
	}
}

func (ResumeTask) Outputs() []contract.FieldSpec { return RunTask{}.Outputs() }

// WorktreeInfo 一个工作树的现状。
type WorktreeInfo struct {
	Project     string `sokel:"project" label:"项目"`
	Branch      string `sokel:"branch" label:"分支"`
	Path        string `sokel:"path" label:"路径" desc:"人要接管就 cd 到这儿"`
	SizeMB      int    `sokel:"size_mb" label:"占用(MB)"`
	LastActive  string `sokel:"last_active" label:"最后活动"`
	IdleDays    int    `sokel:"idle_days" label:"闲置天数"`
	Dirty       bool   `sokel:"dirty" label:"有未提交改动" desc:"true = 删掉就没了,先确认"`
	Sessions    int    `sokel:"sessions" label:"会话数"`
	LastSession string `sokel:"last_session" label:"最近会话 ID" desc:"claude --resume 用它"`
}

// ListWorktrees 列出工作树。
type ListWorktrees struct{}

func (ListWorktrees) Meta() contract.Meta {
	return contract.Meta{ID: "list_worktrees", Label: "列出工作树", TimeoutSec: 120,
		Desc: "这台机器上有哪些工作树:占多大、多久没动、有没有未提交的改动、能接管的会话是哪个。清理之前先看这个"}
}

func (ListWorktrees) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("project").Label("只看这个项目").Desc("留空 = 全部").Optional(),
	}
}

func (ListWorktrees) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []WorktreeInfo{}).Label("工作树"),
		field.Int("count").Label("个数"),
		field.Int("total_mb").Label("合计占用(MB)"),
		field.String("workspace").Label("工作区目录"),
	}
}

// Cleanup 清理工作树。
type Cleanup struct{}

func (Cleanup) Meta() contract.Meta {
	return contract.Meta{ID: "cleanup", Label: "清理工作树", TimeoutSec: 300,
		Desc: "删工作树回收磁盘。可以点名一个分支，也可以按闲置天数批量清。" +
			"仓库缓存、已推送的分支、CC 的会话记录都不受影响"}
}

func (Cleanup) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("project").Label("项目").Desc("留空 = 所有项目（配合闲置天数用）").Optional(),
		field.String("branch").Label("工作分支").Desc("点名删这一个；留空则按闲置天数批量").Optional(),
		field.Int("idle_days").Label("只删闲置超过几天的").
			Desc("批量清理用。**没给分支时必须给它**——否则等于要删光所有工作树，插件会拒绝").Optional(),
		field.Bool("include_dirty").Label("连有未提交改动的一起删").
			Desc("默认关 = **保护有改动的工作树**（删掉就没了）。" +
				"刻意写成「连脏的一起删」而不是「跳过脏的」：布尔留空就是 false，" +
				"这样「没填」天然等于「保护」，不必去猜作者有没有填过").Optional(),
		field.Bool("dry_run").Label("只看不删").Desc("先跑一次看会删掉哪些，再决定").Optional(),
	}
}

func (Cleanup) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("removed").Label("删除个数"),
		field.Array("items", []WorktreeInfo{}).Label("删掉的（dry_run 时是「将要删的」）"),
		field.Int("freed_mb").Label("回收(MB)"),
		field.Int("skipped_dirty").Label("因有改动跳过的个数"),
	}
}

// HealthCheck 平台约定的凭证体检。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 60,
		Desc: "查 claude 装没装、版本、工作区可写、GitLab 通不通"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("claude_version").Label("Claude Code 版本"),
		field.String("workspace_dir").Label("工作区"),
		field.Bool("gitlab_ok").Label("GitLab 可达"),
		field.String("message").Label("说明"),
	}
}

// —— 凭证 ——

type Credential struct{}

// CredentialFields 凭证只回答一个问题：**这次调用用谁的额度**。
//
// 曾经这里还有 GitLab 地址/令牌、工作区目录、claude 路径、代理、外部目录开关——
// 那全是「这台机器能干什么」，是**部署属性**，不是凭证。混在一起有三个实害：
// 同一个 GitLab 令牌要在 gitlab 插件和这里各配一份（轮换时必漏一处）；
// 换一台机器部署要去改凭证；以及最要命的——凭证是工作流作者在画布上选的，
// 而机器能访问哪些目录、走哪个代理，该由运维定，不该由编排的人选。
//
// 现在它们全部走**插件进程的环境变量**，见 env.go。
func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("api_key").Label("Anthropic API Key").
			Desc("**留空也能跑**：那样就用插件那台机器上 CC 自己的登录态（订阅制常见）。" +
				"填了则这次调用走这把 key 的额度——多个空间/团队各自分账、要审计时用它。" +
				"两种都是合法部署，插件不替你选").Optional(),
	}
}
