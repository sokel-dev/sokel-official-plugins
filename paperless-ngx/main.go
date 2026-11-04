package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.Env("PAPERLESS_URL"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).healthCheck(ctx, in)
	})
	OnListDocuments(h, func(ctx plugin.Ctx, in *ListDocumentsIn) (*ListDocumentsOut, error) {
		return clientFor(ctx).listDocuments(ctx, in)
	})
	OnGetDocument(h, func(ctx plugin.Ctx, in *GetDocumentIn) (*GetDocumentOut, error) {
		return clientFor(ctx).getDocument(ctx, in)
	})
	OnUploadDocument(h, func(ctx plugin.Ctx, in *UploadDocumentIn) (*UploadDocumentOut, error) {
		return clientFor(ctx).uploadDocument(ctx, in)
	})
	OnGetIngestionTask(h, func(ctx plugin.Ctx, in *GetIngestionTaskIn) (*GetIngestionTaskOut, error) {
		return clientFor(ctx).getIngestionTask(ctx, in)
	})
	OnDownloadDocument(h, func(ctx plugin.Ctx, in *DownloadDocumentIn) (*DownloadDocumentOut, error) {
		return clientFor(ctx).downloadDocument(ctx, in)
	})
	OnUpdateDocument(h, func(ctx plugin.Ctx, in *UpdateDocumentIn) (*UpdateDocumentOut, error) {
		return clientFor(ctx).updateDocument(ctx, in)
	})
	OnListTags(h, func(ctx plugin.Ctx, in *ListTagsIn) (*ListTagsOut, error) { return clientFor(ctx).listTags(ctx, in) })
	OnCreateTag(h, func(ctx plugin.Ctx, in *CreateTagIn) (*CreateTagOut, error) { return clientFor(ctx).createTag(ctx, in) })
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "paperless-ngx"})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
