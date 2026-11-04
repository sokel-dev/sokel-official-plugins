package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/*.md
var docs embed.FS

func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/qdrant.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListCollections(h, func(ctx plugin.Ctx, in *ListCollectionsIn) (*ListCollectionsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_collections(ctx, in)
	})
	OnGetCollection(h, func(ctx plugin.Ctx, in *GetCollectionIn) (*GetCollectionOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_collection(ctx, in)
	})
	OnCreateCollection(h, func(ctx plugin.Ctx, in *CreateCollectionIn) (*CreateCollectionOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_collection(ctx, in)
	})
	OnQueryPoints(h, func(ctx plugin.Ctx, in *QueryPointsIn) (*QueryPointsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).query_points(ctx, in)
	})
	OnUpsertPoints(h, func(ctx plugin.Ctx, in *UpsertPointsIn) (*UpsertPointsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).upsert_points(ctx, in)
	})
	OnRetrievePoints(h, func(ctx plugin.Ctx, in *RetrievePointsIn) (*RetrievePointsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).retrieve_points(ctx, in)
	})
	OnScrollPoints(h, func(ctx plugin.Ctx, in *ScrollPointsIn) (*ScrollPointsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).scroll_points(ctx, in)
	})
	OnDeletePoints(h, func(ctx plugin.Ctx, in *DeletePointsIn) (*DeletePointsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).delete_points(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "qdrant", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
