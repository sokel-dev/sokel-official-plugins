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
	OnListFiles(h, func(ctx plugin.Ctx, in *ListFilesIn) (*ListFilesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).listFiles(ctx, in)
	})
	OnGetFile(h, func(ctx plugin.Ctx, in *GetFileIn) (*GetFileOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getFile(ctx, in)
	})
	OnCreateFolder(h, func(ctx plugin.Ctx, in *CreateFolderIn) (*CreateFolderOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).createFolder(ctx, in)
	})
	OnRenameFile(h, func(ctx plugin.Ctx, in *RenameFileIn) (*RenameFileOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).renameFile(ctx, in)
	})
	OnGetFilePath(h, func(ctx plugin.Ctx, in *GetFilePathIn) (*GetFilePathOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).getFilePath(ctx, in)
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
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "wps"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
