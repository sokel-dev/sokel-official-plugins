package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.Env("BOOKSTACK_URL"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).healthCheck(ctx, in)
	})
	OnListEntities(h, func(ctx plugin.Ctx, in *ListEntitiesIn) (*ListEntitiesOut, error) {
		return clientFor(ctx).listEntities(ctx, in)
	})
	OnGetShelf(h, func(ctx plugin.Ctx, in *GetShelfIn) (*GetShelfOut, error) { return clientFor(ctx).getShelf(ctx, in) })
	OnGetBook(h, func(ctx plugin.Ctx, in *GetBookIn) (*GetBookOut, error) { return clientFor(ctx).getBook(ctx, in) })
	OnGetChapter(h, func(ctx plugin.Ctx, in *GetChapterIn) (*GetChapterOut, error) {
		return clientFor(ctx).getChapter(ctx, in)
	})
	OnGetPage(h, func(ctx plugin.Ctx, in *GetPageIn) (*GetPageOut, error) { return clientFor(ctx).getPage(ctx, in) })
	OnSearch(h, func(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) { return clientFor(ctx).search(ctx, in) })
	OnCreatePage(h, func(ctx plugin.Ctx, in *CreatePageIn) (*CreatePageOut, error) {
		return clientFor(ctx).createPage(ctx, in)
	})
	OnUpdatePage(h, func(ctx plugin.Ctx, in *UpdatePageIn) (*UpdatePageOut, error) {
		return clientFor(ctx).updatePage(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "bookstack"})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
