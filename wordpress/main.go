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
	OnListPosts(h, func(ctx plugin.Ctx, in *ListPostsIn) (*ListPostsOut, error) { return clientFor(ctx).listPosts(ctx, in) })
	OnGetPost(h, func(ctx plugin.Ctx, in *GetPostIn) (*GetPostOut, error) { return clientFor(ctx).getPost(ctx, in) })
	OnCreateDraft(h, func(ctx plugin.Ctx, in *CreateDraftIn) (*CreateDraftOut, error) {
		return clientFor(ctx).createDraft(ctx, in)
	})
	OnUpdatePost(h, func(ctx plugin.Ctx, in *UpdatePostIn) (*UpdatePostOut, error) {
		return clientFor(ctx).updatePost(ctx, in)
	})
	OnListCategories(h, func(ctx plugin.Ctx, in *ListCategoriesIn) (*ListCategoriesOut, error) {
		return clientFor(ctx).listCategories(ctx, in)
	})
	OnListMedia(h, func(ctx plugin.Ctx, in *ListMediaIn) (*ListMediaOut, error) { return clientFor(ctx).listMedia(ctx, in) })
	OnUploadMedia(h, func(ctx plugin.Ctx, in *UploadMediaIn) (*UploadMediaOut, error) {
		return clientFor(ctx).uploadMedia(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "wordpress"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
