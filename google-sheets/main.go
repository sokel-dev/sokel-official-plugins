package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func apiOf(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx).AccessToken) }
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "google-sheets"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	RegisterAuth(p)
	DeclareEvents(p)
	registerOperations(p, apiOf)
	sokel.RegisterSource(p, sokel.Source{ID: "changes", Label: "Poll for changes every minute"}, runSource)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnGetSpreadsheet(h, func(ctx plugin.Ctx, in *GetSpreadsheetIn) (*GetSpreadsheetOut, error) {
		return getAPI(ctx).metadata(ctx, in.SpreadsheetID)
	})
	OnReadValues(h, func(ctx plugin.Ctx, in *ReadValuesIn) (*ReadValuesOut, error) { return getAPI(ctx).read(ctx, in) })
	OnUpdateValues(h, func(ctx plugin.Ctx, in *UpdateValuesIn) (*UpdateValuesOut, error) {
		return getAPI(ctx).write(ctx, in.SpreadsheetID, in.Range, in.Values, in.ValueInputOption, false)
	})
	OnAppendValues(h, func(ctx plugin.Ctx, in *AppendValuesIn) (*AppendValuesOut, error) {
		out, e := getAPI(ctx).write(ctx, in.SpreadsheetID, in.Range, in.Values, in.ValueInputOption, true)
		if e != nil {
			return nil, e
		}
		return &AppendValuesOut{UpdatedRange: out.UpdatedRange, UpdatedRows: out.UpdatedRows, UpdatedCells: out.UpdatedCells}, nil
	})
	OnBatchUpdateValues(h, func(ctx plugin.Ctx, in *BatchUpdateValuesIn) (*BatchUpdateValuesOut, error) {
		return getAPI(ctx).batch(ctx, in)
	})
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return getAPI(ctx).health(ctx) })
}
