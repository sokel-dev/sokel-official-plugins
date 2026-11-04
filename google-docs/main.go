package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func apiOf(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx).AccessToken) }
func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnGetDocument(h, func(c plugin.Ctx, in *GetDocumentIn) (*GetDocumentOut, error) { return getAPI(c).get(c, in) })
	OnCreateDocument(h, func(c plugin.Ctx, in *CreateDocumentIn) (*CreateDocumentOut, error) { return getAPI(c).create(c, in) })
	OnInsertText(h, func(c plugin.Ctx, in *InsertTextIn) (*InsertTextOut, error) { return getAPI(c).insert(c, in) })
	OnReplaceText(h, func(c plugin.Ctx, in *ReplaceTextIn) (*ReplaceTextOut, error) { return getAPI(c).replace(c, in) })
	OnDeleteText(h, func(c plugin.Ctx, in *DeleteTextIn) (*DeleteTextOut, error) { return getAPI(c).delete(c, in) })
	OnHealthCheck(h, func(c plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return getAPI(c).health(c) })
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "google-docs"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	RegisterAuth(p)
	registerOperations(p, apiOf)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
