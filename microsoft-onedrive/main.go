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
	OnGetDrive(h, func(ctx plugin.Ctx, in *GetDriveIn) (*GetDriveOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getDrive(ctx, in)
	})
	OnListItems(h, func(ctx plugin.Ctx, in *ListItemsIn) (*ListItemsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listItems(ctx, in)
	})
	OnGetItem(h, func(ctx plugin.Ctx, in *GetItemIn) (*GetItemOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getItem(ctx, in)
	})
	OnSearchItems(h, func(ctx plugin.Ctx, in *SearchItemsIn) (*SearchItemsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).searchItems(ctx, in)
	})
	OnUploadFile(h, func(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).uploadFile(ctx, in)
	})
	OnDelta(h, func(ctx plugin.Ctx, in *DeltaIn) (*DeltaOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).delta(ctx, in)
	})
	OnGetDownloadInfo(h, func(ctx plugin.Ctx, in *GetDownloadInfoIn) (*GetDownloadInfoOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getDownloadInfo(ctx, in)
	})
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "microsoft-onedrive"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
