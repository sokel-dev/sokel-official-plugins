package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "linear"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx)) })
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(ctx).health(ctx, in) })
	OnListTeams(h, func(ctx plugin.Ctx, in *ListTeamsIn) (*ListTeamsOut, error) { return client(ctx).teams(ctx, in) })
	OnListIssues(h, func(ctx plugin.Ctx, in *ListIssuesIn) (*ListIssuesOut, error) { return client(ctx).issues(ctx, in) })
	OnGetIssue(h, func(ctx plugin.Ctx, in *GetIssueIn) (*GetIssueOut, error) { return client(ctx).getIssue(ctx, in) })
	OnListProjects(h, func(ctx plugin.Ctx, in *ListProjectsIn) (*ListProjectsOut, error) {
		return client(ctx).projects(ctx, in)
	})
	OnCreateIssue(h, func(ctx plugin.Ctx, in *CreateIssueIn) (*CreateIssueOut, error) {
		return client(ctx).createIssue(ctx, in)
	})
	OnUpdateIssue(h, func(ctx plugin.Ctx, in *UpdateIssueIn) (*UpdateIssueOut, error) {
		return client(ctx).updateIssue(ctx, in)
	})
}
