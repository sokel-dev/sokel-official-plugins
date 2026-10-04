// gitlab —— Sokel 第一方插件：GitLab 自动化（自建 CE / gitlab.com 通吃）。
//
// 25 个操作覆盖 仓库/MR/Issue/CI 四个域 + call 保底。设计判断见 schema/schema.go 顶注。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./gitlab
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
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "gitlab",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnProjectsList(p, opProjectsList)
	OnFileGet(p, opFileGet)
	OnFileWrite(p, opFileWrite)
	OnBranchesList(p, opBranchesList)
	OnCommitsList(p, opCommitsList)
	OnMrList(p, opMrList)
	OnMrCreate(p, opMrCreate)
	OnMrMerge(p, opMrMerge)
	OnMrNote(p, opMrNote)
	OnMrChanges(p, opMrChanges)
	OnMrApprove(p, opMrApprove)
	OnMrUpdate(p, opMrUpdate)
	OnIssuesList(p, opIssuesList)
	OnIssueCreate(p, opIssueCreate)
	OnIssueNote(p, opIssueNote)
	OnIssueUpdate(p, opIssueUpdate)
	OnNoteUpdate(p, opNoteUpdate)
	OnPipelinesList(p, opPipelinesList)
	OnPipelineTrigger(p, opPipelineTrigger)
	OnPipelineJobs(p, opPipelineJobs)
	OnJobLog(p, opJobLog)
	OnSearch(p, opSearch)
	OnMrDiscussion(p, opMrDiscussion)
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	// 事件源：轮询 Events API + 失败流水线（凭证填了 watch_projects 才启动）。
	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "poll", Label: "GitLab 事件轮询"}, runEvents)
	// 平台代收 webhook(零延迟版;与轮询源二选一,见 docs)
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
