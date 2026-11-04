package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "jira"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx)) })
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(ctx).health(ctx, in) })
	OnSearchIssues(h, func(ctx plugin.Ctx, in *SearchIssuesIn) (*SearchIssuesOut, error) { return client(ctx).search(ctx, in) })
	OnGetIssue(h, func(ctx plugin.Ctx, in *GetIssueIn) (*GetIssueOut, error) { return client(ctx).getIssue(ctx, in) })
	OnCreateIssue(h, func(ctx plugin.Ctx, in *CreateIssueIn) (*CreateIssueOut, error) {
		return client(ctx).createIssue(ctx, in)
	})
	OnUpdateIssue(h, func(ctx plugin.Ctx, in *UpdateIssueIn) (*UpdateIssueOut, error) {
		return client(ctx).updateIssue(ctx, in)
	})
	OnListTransitions(h, func(ctx plugin.Ctx, in *ListTransitionsIn) (*ListTransitionsOut, error) {
		return client(ctx).transitions(ctx, in)
	})
	OnTransitionIssue(h, func(ctx plugin.Ctx, in *TransitionIssueIn) (*TransitionIssueOut, error) {
		return client(ctx).transition(ctx, in)
	})
	OnListComments(h, func(ctx plugin.Ctx, in *ListCommentsIn) (*ListCommentsOut, error) {
		return client(ctx).comments(ctx, in)
	})
	OnAddComment(h, func(ctx plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) { return client(ctx).addComment(ctx, in) })
}
