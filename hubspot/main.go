package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate -lang go .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client { return newClient(sokel.CredentialAs[Cred](ctx)) }
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return clientFor(ctx).health(ctx) })
	OnListRecords(h, func(ctx plugin.Ctx, in *ListRecordsIn) (*ListRecordsOut, error) {
		return clientFor(ctx).listRecords(ctx, in)
	})
	OnSearchRecords(h, func(ctx plugin.Ctx, in *SearchRecordsIn) (*SearchRecordsOut, error) {
		return clientFor(ctx).searchRecords(ctx, in)
	})
	OnGetRecord(h, func(ctx plugin.Ctx, in *GetRecordIn) (*GetRecordOut, error) { return clientFor(ctx).getRecord(ctx, in) })
	OnCreateRecord(h, func(ctx plugin.Ctx, in *CreateRecordIn) (*CreateRecordOut, error) {
		return clientFor(ctx).createRecord(ctx, in)
	})
	OnUpdateRecord(h, func(ctx plugin.Ctx, in *UpdateRecordIn) (*UpdateRecordOut, error) {
		return clientFor(ctx).updateRecord(ctx, in)
	})
	OnListProperties(h, func(ctx plugin.Ctx, in *ListPropertiesIn) (*ListPropertiesOut, error) {
		return clientFor(ctx).listProperties(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "hubspot"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
