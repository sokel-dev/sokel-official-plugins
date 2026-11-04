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
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/baserow.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListTables(h, func(ctx plugin.Ctx, in *ListTablesIn) (*ListTablesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_tables(ctx, in)
	})
	OnListFields(h, func(ctx plugin.Ctx, in *ListFieldsIn) (*ListFieldsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_fields(ctx, in)
	})
	OnListRows(h, func(ctx plugin.Ctx, in *ListRowsIn) (*ListRowsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_rows(ctx, in)
	})
	OnGetRow(h, func(ctx plugin.Ctx, in *GetRowIn) (*GetRowOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_row(ctx, in)
	})
	OnCreateRow(h, func(ctx plugin.Ctx, in *CreateRowIn) (*CreateRowOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_row(ctx, in)
	})
	OnUpdateRow(h, func(ctx plugin.Ctx, in *UpdateRowIn) (*UpdateRowOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).update_row(ctx, in)
	})
	OnDeleteRow(h, func(ctx plugin.Ctx, in *DeleteRowIn) (*DeleteRowOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).delete_row(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "baserow", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
