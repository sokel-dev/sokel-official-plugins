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
	b, _ := docs.ReadFile("docs/typeform.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListWorkspaces(h, func(ctx plugin.Ctx, in *ListWorkspacesIn) (*ListWorkspacesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_workspaces(ctx, in)
	})
	OnGetWorkspace(h, func(ctx plugin.Ctx, in *GetWorkspaceIn) (*GetWorkspaceOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_workspace(ctx, in)
	})
	OnListForms(h, func(ctx plugin.Ctx, in *ListFormsIn) (*ListFormsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_forms(ctx, in)
	})
	OnGetForm(h, func(ctx plugin.Ctx, in *GetFormIn) (*GetFormOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_form(ctx, in)
	})
	OnListResponses(h, func(ctx plugin.Ctx, in *ListResponsesIn) (*ListResponsesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_responses(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "typeform", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
