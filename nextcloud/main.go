package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/nextcloud.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnGetInfo(h, func(ctx plugin.Ctx, in *GetInfoIn) (*GetInfoOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getInfo(ctx, in)
	})
	OnListDirectory(h, func(ctx plugin.Ctx, in *ListDirectoryIn) (*ListDirectoryOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listDirectory(ctx, in)
	})
	OnCreateDirectory(h, func(ctx plugin.Ctx, in *CreateDirectoryIn) (*CreateDirectoryOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).createDirectory(ctx, in)
	})
	OnUploadFile(h, func(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).uploadFile(ctx, in)
	})
	OnDownloadFile(h, func(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).downloadFile(ctx, in)
	})
	OnMoveResource(h, func(ctx plugin.Ctx, in *MoveResourceIn) (*MoveResourceOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).moveResource(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "nextcloud", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
