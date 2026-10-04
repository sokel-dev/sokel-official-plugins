// gitlab -- a first-party Sokel plugin: GitLab automation (works with self-hosted CE and gitlab.com alike).
//
// 25 operations covering the repository/MR/Issue/CI domains, plus call as a catch-all. Design
// decisions are documented at the top of schema/schema.go.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./gitlab
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

	// Event source: polls the Events API + failed pipelines (only starts if the credential has watch_projects set).
	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "poll", Label: "GitLab 事件轮询"}, runEvents)
	// The platform receives the webhook on our behalf (zero-latency version; pick either this or the polling source, see docs)
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
