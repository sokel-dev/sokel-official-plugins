package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).healthCheck(ctx, in)
	})
	OnCreateSession(h, func(ctx plugin.Ctx, in *CreateSessionIn) (*CreateSessionOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).createSession(ctx, in)
	})
	OnCloseSession(h, func(ctx plugin.Ctx, in *CloseSessionIn) (*CloseSessionOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).closeSession(ctx, in)
	})
	OnListWorksheets(h, func(ctx plugin.Ctx, in *ListWorksheetsIn) (*ListWorksheetsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listWorksheets(ctx, in)
	})
	OnListTables(h, func(ctx plugin.Ctx, in *ListTablesIn) (*ListTablesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listTables(ctx, in)
	})
	OnGetRange(h, func(ctx plugin.Ctx, in *GetRangeIn) (*GetRangeOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getRange(ctx, in)
	})
	OnUpdateRange(h, func(ctx plugin.Ctx, in *UpdateRangeIn) (*UpdateRangeOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).updateRange(ctx, in)
	})
	OnAppendRows(h, func(ctx plugin.Ctx, in *AppendRowsIn) (*AppendRowsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).appendRows(ctx, in)
	})
	OnListRows(h, func(ctx plugin.Ctx, in *ListRowsIn) (*ListRowsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listRows(ctx, in)
	})
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "microsoft-excel"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
