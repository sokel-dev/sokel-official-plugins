// github — a Sokel first-party plugin: GitHub project maintenance automation (works with both
// github.com and GitHub Enterprise Server).
//
// 43 operations cover six domains — repo/Issue/PR/Actions/releases/boards — plus a bot feedback
// surface (commit status, check runs, reactions) and a call fallback. Events have two sources:
// webhook and polling. See the top-of-file notes in schema/schema.go and README.md for design
// decisions.
//
// Run with: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./github
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

	// —— repo ——
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

	// —— bot feedback surface ——
	OnCommitStatusCreate(p, opCommitStatusCreate)
	OnCheckRunCreate(p, opCheckRunCreate)
	OnReactionAdd(p, opReactionAdd)

	// —— Actions ——
	OnWorkflowRunsList(p, opWorkflowRunsList)
	OnWorkflowDispatch(p, opWorkflowDispatch)
	OnRunJobs(p, opRunJobs)
	OnJobLog(p, opJobLog)

	// —— releases and repo housekeeping ——
	OnReleasesList(p, opReleasesList)
	OnReleaseCreate(p, opReleaseCreate)
	OnLabelsList(p, opLabelsList)
	OnLabelCreate(p, opLabelCreate)
	OnMilestonesList(p, opMilestonesList)
	OnCollaboratorsList(p, opCollaboratorsList)
	OnBranchProtectionGet(p, opBranchProtectionGet)

	// —— boards (GraphQL) ——
	OnProjectsList(p, opProjectsList)
	OnProjectItemsList(p, opProjectItemsList)
	OnProjectItemAdd(p, opProjectItemAdd)
	OnProjectItemFieldSet(p, opProjectItemFieldSet)

	// —— search / fallback / health check ——
	OnSearch(p, opSearch)
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	// Events have two sources; the docs say pick one:
	//   webhook — zero latency, requires configuring a webhook on the GitHub repo
	//   poll source — no webhook needed, about 1 minute of latency (only starts once watch_repos
	//                 is set on the credential)
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
