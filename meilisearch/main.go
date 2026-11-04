package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	"embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/*.md
var docs embed.FS

func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/meilisearch.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListIndexes(h, func(ctx plugin.Ctx, in *ListIndexesIn) (*ListIndexesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_indexes(ctx, in)
	})
	OnGetIndex(h, func(ctx plugin.Ctx, in *GetIndexIn) (*GetIndexOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_index(ctx, in)
	})
	OnCreateIndex(h, func(ctx plugin.Ctx, in *CreateIndexIn) (*CreateIndexOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_index(ctx, in)
	})
	OnSearch(h, func(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).search(ctx, in)
	})
	OnGetDocument(h, func(ctx plugin.Ctx, in *GetDocumentIn) (*GetDocumentOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_document(ctx, in)
	})
	OnAddDocuments(h, func(ctx plugin.Ctx, in *AddDocumentsIn) (*AddDocumentsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).add_documents(ctx, in)
	})
	OnDeleteDocument(h, func(ctx plugin.Ctx, in *DeleteDocumentIn) (*DeleteDocumentOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).delete_document(ctx, in)
	})
	OnGetTask(h, func(ctx plugin.Ctx, in *GetTaskIn) (*GetTaskOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_task(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "meilisearch", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
