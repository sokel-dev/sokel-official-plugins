package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.Env("GITEA_URL"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).healthCheck(ctx, in)
	})
	OnListRepositories(h, func(ctx plugin.Ctx, in *ListRepositoriesIn) (*ListRepositoriesOut, error) {
		return clientFor(ctx).listRepositories(ctx, in)
	})
	OnGetRepository(h, func(ctx plugin.Ctx, in *GetRepositoryIn) (*GetRepositoryOut, error) {
		return clientFor(ctx).getRepository(ctx, in)
	})
	OnListIssues(h, func(ctx plugin.Ctx, in *ListIssuesIn) (*ListIssuesOut, error) {
		return clientFor(ctx).listIssues(ctx, in)
	})
	OnGetIssue(h, func(ctx plugin.Ctx, in *GetIssueIn) (*GetIssueOut, error) { return clientFor(ctx).getIssue(ctx, in) })
	OnCreateIssue(h, func(ctx plugin.Ctx, in *CreateIssueIn) (*CreateIssueOut, error) {
		return clientFor(ctx).createIssue(ctx, in)
	})
	OnAddComment(h, func(ctx plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) {
		return clientFor(ctx).addComment(ctx, in)
	})
	OnListPullRequests(h, func(ctx plugin.Ctx, in *ListPullRequestsIn) (*ListPullRequestsOut, error) {
		return clientFor(ctx).listPullRequests(ctx, in)
	})
	OnCreatePullRequest(h, func(ctx plugin.Ctx, in *CreatePullRequestIn) (*CreatePullRequestOut, error) {
		return clientFor(ctx).createPullRequest(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "gitea"})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
