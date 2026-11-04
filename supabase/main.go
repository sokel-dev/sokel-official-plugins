package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
	"os"
)

func apiOf(ctx plugin.Ctx) *api {
	c := sokel.CredentialAs[Cred](ctx)
	return newAPI(os.Getenv("SUPABASE_URL"), c.APIKey, c.AccessToken)
}
func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnSelectRows(h, func(c plugin.Ctx, in *SelectRowsIn) (*SelectRowsOut, error) { return getAPI(c).selectRows(c, in) })
	OnInsertRows(h, func(c plugin.Ctx, in *InsertRowsIn) (*InsertRowsOut, error) { return getAPI(c).insertRows(c, in) })
	OnUpsertRows(h, func(c plugin.Ctx, in *UpsertRowsIn) (*UpsertRowsOut, error) { return getAPI(c).upsertRows(c, in) })
	OnUpdateRows(h, func(c plugin.Ctx, in *UpdateRowsIn) (*UpdateRowsOut, error) { return getAPI(c).updateRows(c, in) })
	OnDeleteRows(h, func(c plugin.Ctx, in *DeleteRowsIn) (*DeleteRowsOut, error) { return getAPI(c).deleteRows(c, in) })
	OnCallFunction(h, func(c plugin.Ctx, in *CallFunctionIn) (*CallFunctionOut, error) { return getAPI(c).callFunction(c, in) })
	OnListObjects(h, func(c plugin.Ctx, in *ListObjectsIn) (*ListObjectsOut, error) { return getAPI(c).listObjects(c, in) })
	OnCreateSignedURL(h, func(c plugin.Ctx, in *CreateSignedURLIn) (*CreateSignedURLOut, error) {
		return getAPI(c).signedURL(c, in)
	})
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return getAPI(c).health(c, in) })
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "supabase"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, apiOf)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
