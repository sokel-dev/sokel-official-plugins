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
	b, _ := docs.ReadFile("docs/pocketbase.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListRecords(h, func(ctx plugin.Ctx, in *ListRecordsIn) (*ListRecordsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_records(ctx, in)
	})
	OnFindRecord(h, func(ctx plugin.Ctx, in *FindRecordIn) (*FindRecordOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).find_record(ctx, in)
	})
	OnGetRecord(h, func(ctx plugin.Ctx, in *GetRecordIn) (*GetRecordOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_record(ctx, in)
	})
	OnCreateRecord(h, func(ctx plugin.Ctx, in *CreateRecordIn) (*CreateRecordOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_record(ctx, in)
	})
	OnUpdateRecord(h, func(ctx plugin.Ctx, in *UpdateRecordIn) (*UpdateRecordOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).update_record(ctx, in)
	})
	OnDeleteRecord(h, func(ctx plugin.Ctx, in *DeleteRecordIn) (*DeleteRecordOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).delete_record(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "pocketbase", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
