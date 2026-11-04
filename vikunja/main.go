package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	"embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/*.md
var docs embed.FS

func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/vikunja.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListProjects(h, func(ctx plugin.Ctx, in *ListProjectsIn) (*ListProjectsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_projects(ctx, in)
	})
	OnGetProject(h, func(ctx plugin.Ctx, in *GetProjectIn) (*GetProjectOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_project(ctx, in)
	})
	OnCreateProject(h, func(ctx plugin.Ctx, in *CreateProjectIn) (*CreateProjectOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_project(ctx, in)
	})
	OnListTasks(h, func(ctx plugin.Ctx, in *ListTasksIn) (*ListTasksOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_tasks(ctx, in)
	})
	OnGetTask(h, func(ctx plugin.Ctx, in *GetTaskIn) (*GetTaskOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_task(ctx, in)
	})
	OnCreateTask(h, func(ctx plugin.Ctx, in *CreateTaskIn) (*CreateTaskOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_task(ctx, in)
	})
	OnUpdateTask(h, func(ctx plugin.Ctx, in *UpdateTaskIn) (*UpdateTaskOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).update_task(ctx, in)
	})
	OnAddComment(h, func(ctx plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).add_comment(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "vikunja", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
