package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/linkding.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).healthCheck(ctx, in)
	})
	OnListBookmarks(h, func(ctx plugin.Ctx, in *ListBookmarksIn) (*ListBookmarksOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listBookmarks(ctx, in)
	})
	OnGetBookmark(h, func(ctx plugin.Ctx, in *GetBookmarkIn) (*GetBookmarkOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getBookmark(ctx, in)
	})
	OnSaveBookmark(h, func(ctx plugin.Ctx, in *SaveBookmarkIn) (*SaveBookmarkOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).saveBookmark(ctx, in)
	})
	OnUpdateBookmark(h, func(ctx plugin.Ctx, in *UpdateBookmarkIn) (*UpdateBookmarkOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).updateBookmark(ctx, in)
	})
	OnArchiveBookmark(h, func(ctx plugin.Ctx, in *ArchiveBookmarkIn) (*ArchiveBookmarkOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).archiveBookmark(ctx, in)
	})
	OnUnarchiveBookmark(h, func(ctx plugin.Ctx, in *UnarchiveBookmarkIn) (*UnarchiveBookmarkOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).unarchiveBookmark(ctx, in)
	})
	OnDeleteBookmark(h, func(ctx plugin.Ctx, in *DeleteBookmarkIn) (*DeleteBookmarkOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).deleteBookmark(ctx, in)
	})
	OnListTags(h, func(ctx plugin.Ctx, in *ListTagsIn) (*ListTagsOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listTags(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "linkding", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
