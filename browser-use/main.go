package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).healthCheck(ctx, in)
	})
	OnCreateRun(h, func(ctx plugin.Ctx, in *CreateRunIn) (*CreateRunOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).createRun(ctx, in)
	})
	OnGetRun(h, func(ctx plugin.Ctx, in *GetRunIn) (*GetRunOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getRun(ctx, in)
	})
	OnGetRunStatus(h, func(ctx plugin.Ctx, in *GetRunStatusIn) (*GetRunStatusOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getRunStatus(ctx, in)
	})
	OnListRuns(h, func(ctx plugin.Ctx, in *ListRunsIn) (*ListRunsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listRuns(ctx, in)
	})
	OnCancelRun(h, func(ctx plugin.Ctx, in *CancelRunIn) (*CancelRunOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).cancelRun(ctx, in)
	})
	OnListRunEvents(h, func(ctx plugin.Ctx, in *ListRunEventsIn) (*ListRunEventsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listRunEvents(ctx, in)
	})
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "browser-use"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
