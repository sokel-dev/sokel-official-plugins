package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate -lang go .

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client { return newClient(sokel.CredentialAs[Cred](ctx).Token) }
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	DeclareEvents(p)
	registerOperations(p)
	sokel.RegisterSource(p, sokel.Source{ID: "records", Label: "Record-change polling"}, runSource)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "airtable"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func healthCheck(ctx plugin.Ctx) (*HealthCheckOut, error) {
	out, err := clientFor(ctx).health(ctx)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	out.Message = "Airtable access token is valid."
	return out, nil
}

func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return healthCheck(ctx) })
	OnAirtableBases(h, func(ctx plugin.Ctx, in *AirtableBasesIn) (*AirtableBasesOut, error) {
		return clientFor(ctx).bases(ctx, in)
	})
	OnAirtableTables(h, func(ctx plugin.Ctx, in *AirtableTablesIn) (*AirtableTablesOut, error) {
		return clientFor(ctx).tables(ctx, in)
	})
	OnAirtableRecords(h, func(ctx plugin.Ctx, in *AirtableRecordsIn) (*AirtableRecordsOut, error) {
		return clientFor(ctx).records(ctx, in)
	})
	OnAirtableGet(h, func(ctx plugin.Ctx, in *AirtableGetIn) (*AirtableGetOut, error) { return clientFor(ctx).get(ctx, in) })
	OnAirtableCreate(h, func(ctx plugin.Ctx, in *AirtableCreateIn) (*AirtableCreateOut, error) {
		return clientFor(ctx).create(ctx, in)
	})
	OnAirtableUpdate(h, func(ctx plugin.Ctx, in *AirtableUpdateIn) (*AirtableUpdateOut, error) {
		return clientFor(ctx).update(ctx, in)
	})
	OnAirtableDelete(h, func(ctx plugin.Ctx, in *AirtableDeleteIn) (*AirtableDeleteOut, error) {
		return clientFor(ctx).remove(ctx, in)
	})
}
