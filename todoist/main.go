package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "todoist"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx)) })
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func registerOperations(h plugin.Host, c func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return c(ctx).health(ctx, in) })
	OnListProjects(h, func(ctx plugin.Ctx, in *ListProjectsIn) (*ListProjectsOut, error) {
		return c(ctx).listProjects(ctx, in)
	})
	OnGetProject(h, func(ctx plugin.Ctx, in *GetProjectIn) (*GetProjectOut, error) { return c(ctx).getProject(ctx, in) })
	OnCreateProject(h, func(ctx plugin.Ctx, in *CreateProjectIn) (*CreateProjectOut, error) {
		return c(ctx).createProject(ctx, in)
	})
	OnListTasks(h, func(ctx plugin.Ctx, in *ListTasksIn) (*ListTasksOut, error) { return c(ctx).listTasks(ctx, in) })
	OnGetTask(h, func(ctx plugin.Ctx, in *GetTaskIn) (*GetTaskOut, error) { return c(ctx).getTask(ctx, in) })
	OnCreateTask(h, func(ctx plugin.Ctx, in *CreateTaskIn) (*CreateTaskOut, error) { return c(ctx).createTask(ctx, in) })
	OnUpdateTask(h, func(ctx plugin.Ctx, in *UpdateTaskIn) (*UpdateTaskOut, error) { return c(ctx).updateTask(ctx, in) })
	OnCloseTask(h, func(ctx plugin.Ctx, in *CloseTaskIn) (*CloseTaskOut, error) { return c(ctx).closeTask(ctx, in) })
}
