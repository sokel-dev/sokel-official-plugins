package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.EnvOr("API_BASE_URL", "https://docs.qq.com"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	OnFilesList(p, func(ctx plugin.Ctx, in *FilesListIn) (*FilesListOut, error) { return clientFor(ctx).FilesList(ctx, in) })
	OnFilesSearch(p, func(ctx plugin.Ctx, in *FilesSearchIn) (*FilesSearchOut, error) {
		return clientFor(ctx).FilesSearch(ctx, in)
	})
	OnFileGet(p, func(ctx plugin.Ctx, in *FileGetIn) (*FileGetOut, error) { return clientFor(ctx).FileGet(ctx, in) })
	OnFileCreate(p, func(ctx plugin.Ctx, in *FileCreateIn) (*FileCreateOut, error) {
		return clientFor(ctx).FileCreate(ctx, in)
	})
	OnSheetsList(p, func(ctx plugin.Ctx, in *SheetsListIn) (*SheetsListOut, error) {
		return clientFor(ctx).SheetsList(ctx, in)
	})
	OnSheetRangeGet(p, func(ctx plugin.Ctx, in *SheetRangeGetIn) (*SheetRangeGetOut, error) {
		return clientFor(ctx).SheetRangeGet(ctx, in)
	})
	OnSheetTextWrite(p, func(ctx plugin.Ctx, in *SheetTextWriteIn) (*SheetTextWriteOut, error) {
		return clientFor(ctx).SheetTextWrite(ctx, in)
	})
	OnDocumentGet(p, func(ctx plugin.Ctx, in *DocumentGetIn) (*DocumentGetOut, error) {
		return clientFor(ctx).DocumentGet(ctx, in)
	})
	OnDocumentInsertText(p, func(ctx plugin.Ctx, in *DocumentInsertTextIn) (*DocumentInsertTextOut, error) {
		return clientFor(ctx).DocumentInsertText(ctx, in)
	})
	OnHealthCheck(p, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).HealthCheck(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "tencent-docs"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
