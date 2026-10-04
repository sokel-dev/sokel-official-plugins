// github —— Sokel 第一方插件：GitHub 项目维护自动化（github.com / GitHub Enterprise Server 通吃）。
//
// 43 个操作覆盖 仓库/Issue/PR/Actions/发布/看板 六个域，外加机器人回执面
// （提交状态、检查运行、表情）与 call 保底。事件有 webhook 与轮询两条来路。
// 设计判断见 schema/schema.go 顶注与 README.md。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./github
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN（接入组「接入命令」里复制）;" +
			"随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "github",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	// —— 仓库 ——
	OnReposList(p, opReposList)
	OnRepoGet(p, opRepoGet)
	OnFileGet(p, opFileGet)
	OnFileWrite(p, opFileWrite)
	OnBranchCreate(p, opBranchCreate)
	OnBranchesList(p, opBranchesList)
	OnCommitsList(p, opCommitsList)

	// —— Issue ——
	OnIssuesList(p, opIssuesList)
	OnIssueGet(p, opIssueGet)
	OnIssueCreate(p, opIssueCreate)
	OnIssueUpdate(p, opIssueUpdate)
	OnIssueComment(p, opIssueComment)
	OnIssueLabel(p, opIssueLabel)
	OnIssueAssign(p, opIssueAssign)

	// —— PR ——
	OnPrList(p, opPrList)
	OnPrGet(p, opPrGet)
	OnPrCreate(p, opPrCreate)
	OnPrUpdate(p, opPrUpdate)
	OnPrMerge(p, opPrMerge)
	OnPrFiles(p, opPrFiles)
	OnPrReview(p, opPrReview)
	OnPrRequestReviewers(p, opPrRequestReviewers)

	// —— 机器人回执面 ——
	OnCommitStatusCreate(p, opCommitStatusCreate)
	OnCheckRunCreate(p, opCheckRunCreate)
	OnReactionAdd(p, opReactionAdd)

	// —— Actions ——
	OnWorkflowRunsList(p, opWorkflowRunsList)
	OnWorkflowDispatch(p, opWorkflowDispatch)
	OnRunJobs(p, opRunJobs)
	OnJobLog(p, opJobLog)

	// —— 发布与仓库家务 ——
	OnReleasesList(p, opReleasesList)
	OnReleaseCreate(p, opReleaseCreate)
	OnLabelsList(p, opLabelsList)
	OnLabelCreate(p, opLabelCreate)
	OnMilestonesList(p, opMilestonesList)
	OnCollaboratorsList(p, opCollaboratorsList)
	OnBranchProtectionGet(p, opBranchProtectionGet)

	// —— 看板（GraphQL）——
	OnProjectsList(p, opProjectsList)
	OnProjectItemsList(p, opProjectItemsList)
	OnProjectItemAdd(p, opProjectItemAdd)
	OnProjectItemFieldSet(p, opProjectItemFieldSet)

	// —— 搜索 / 保底 / 健康检查 ——
	OnSearch(p, opSearch)
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	// 事件两条来路，文档写明二选一：
	//   webhook —— 零延迟，要在 GitHub 仓库上配一条 webhook
	//   轮询源  —— 不用配 webhook，约 1 分钟延迟（凭证填了 watch_repos 才启动）
	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "poll", Label: "GitHub 事件轮询"}, runEvents)
	sokel.RegisterWebhook(p, handleWebhook)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
