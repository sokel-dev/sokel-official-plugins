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
	b, _ := docs.ReadFile("docs/dropbox.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListFolder(h, func(ctx plugin.Ctx, in *ListFolderIn) (*ListFolderOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_folder(ctx, in)
	})
	OnContinueList(h, func(ctx plugin.Ctx, in *ContinueListIn) (*ContinueListOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).continue_list(ctx, in)
	})
	OnGetMetadata(h, func(ctx plugin.Ctx, in *GetMetadataIn) (*GetMetadataOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_metadata(ctx, in)
	})
	OnSearch(h, func(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).search(ctx, in)
	})
	OnContinueSearch(h, func(ctx plugin.Ctx, in *ContinueSearchIn) (*ContinueSearchOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).continue_search(ctx, in)
	})
	OnUploadFile(h, func(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).upload_file(ctx, in)
	})
	OnDownloadFile(h, func(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).download_file(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "dropbox", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
