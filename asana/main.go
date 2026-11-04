package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnAddComment(h, func(c plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) { return getAPI(c).AddComment(c, in) })
	OnCreateTask(h, func(c plugin.Ctx, in *CreateTaskIn) (*CreateTaskOut, error) { return getAPI(c).CreateTask(c, in) })
	OnGetTask(h, func(c plugin.Ctx, in *GetTaskIn) (*GetTaskOut, error) { return getAPI(c).GetTask(c, in) })
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return getAPI(c).HealthCheck(c, in) })
	OnListProjects(h, func(c plugin.Ctx, in *ListProjectsIn) (*ListProjectsOut, error) { return getAPI(c).ListProjects(c, in) })
	OnListTasks(h, func(c plugin.Ctx, in *ListTasksIn) (*ListTasksOut, error) { return getAPI(c).ListTasks(c, in) })
	OnListWorkspaces(h, func(c plugin.Ctx, in *ListWorkspacesIn) (*ListWorkspacesOut, error) {
		return getAPI(c).ListWorkspaces(c, in)
	})
	OnUpdateTask(h, func(c plugin.Ctx, in *UpdateTaskIn) (*UpdateTaskOut, error) { return getAPI(c).UpdateTask(c, in) })
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "asana"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c).AccessToken) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
