package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/miniflux.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnGetCurrentUser(h, func(ctx plugin.Ctx, in *GetCurrentUserIn) (*GetCurrentUserOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getCurrentUser(ctx, in)
	})
	OnListCategories(h, func(ctx plugin.Ctx, in *ListCategoriesIn) (*ListCategoriesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listCategories(ctx, in)
	})
	OnCreateCategory(h, func(ctx plugin.Ctx, in *CreateCategoryIn) (*CreateCategoryOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).createCategory(ctx, in)
	})
	OnListFeeds(h, func(ctx plugin.Ctx, in *ListFeedsIn) (*ListFeedsOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listFeeds(ctx, in)
	})
	OnCreateFeed(h, func(ctx plugin.Ctx, in *CreateFeedIn) (*CreateFeedOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).createFeed(ctx, in)
	})
	OnRefreshFeed(h, func(ctx plugin.Ctx, in *RefreshFeedIn) (*RefreshFeedOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).refreshFeed(ctx, in)
	})
	OnListEntries(h, func(ctx plugin.Ctx, in *ListEntriesIn) (*ListEntriesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listEntries(ctx, in)
	})
	OnGetEntry(h, func(ctx plugin.Ctx, in *GetEntryIn) (*GetEntryOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getEntry(ctx, in)
	})
	OnUpdateEntries(h, func(ctx plugin.Ctx, in *UpdateEntriesIn) (*UpdateEntriesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).updateEntries(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "miniflux", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
