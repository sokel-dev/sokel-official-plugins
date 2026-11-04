package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "confluence"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx)) })
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(ctx).health(ctx, in) })
	OnListSpaces(h, func(ctx plugin.Ctx, in *ListSpacesIn) (*ListSpacesOut, error) { return client(ctx).spaces(ctx, in) })
	OnGetSpace(h, func(ctx plugin.Ctx, in *GetSpaceIn) (*GetSpaceOut, error) { return client(ctx).getSpace(ctx, in) })
	OnListPages(h, func(ctx plugin.Ctx, in *ListPagesIn) (*ListPagesOut, error) { return client(ctx).pages(ctx, in) })
	OnGetPage(h, func(ctx plugin.Ctx, in *GetPageIn) (*GetPageOut, error) { return client(ctx).getPage(ctx, in) })
	OnCreatePage(h, func(ctx plugin.Ctx, in *CreatePageIn) (*CreatePageOut, error) { return client(ctx).createPage(ctx, in) })
	OnUpdatePage(h, func(ctx plugin.Ctx, in *UpdatePageIn) (*UpdatePageOut, error) { return client(ctx).updatePage(ctx, in) })
}
