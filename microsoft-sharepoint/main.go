package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).healthCheck(ctx, in)
	})
	OnSearchSites(h, func(ctx plugin.Ctx, in *SearchSitesIn) (*SearchSitesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).searchSites(ctx, in)
	})
	OnGetSite(h, func(ctx plugin.Ctx, in *GetSiteIn) (*GetSiteOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getSite(ctx, in)
	})
	OnListDrives(h, func(ctx plugin.Ctx, in *ListDrivesIn) (*ListDrivesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listDrives(ctx, in)
	})
	OnListLists(h, func(ctx plugin.Ctx, in *ListListsIn) (*ListListsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listLists(ctx, in)
	})
	OnListColumns(h, func(ctx plugin.Ctx, in *ListColumnsIn) (*ListColumnsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listColumns(ctx, in)
	})
	OnListItems(h, func(ctx plugin.Ctx, in *ListItemsIn) (*ListItemsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listItems(ctx, in)
	})
	OnCreateItem(h, func(ctx plugin.Ctx, in *CreateItemIn) (*CreateItemOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).createItem(ctx, in)
	})
	OnUpdateFields(h, func(ctx plugin.Ctx, in *UpdateFieldsIn) (*UpdateFieldsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).updateFields(ctx, in)
	})
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "microsoft-sharepoint"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
