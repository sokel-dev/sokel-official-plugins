package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func apiOf(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx).AccessToken) }
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "google-drive"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	RegisterAuth(p)
	DeclareEvents(p)
	registerOperations(p, apiOf)
	sokel.RegisterSource(p, sokel.Source{ID: "changes", Label: "Poll for changes every minute"}, runSource)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnListFiles(h, func(ctx plugin.Ctx, in *ListFilesIn) (*ListFilesOut, error) { return getAPI(ctx).list(ctx, in) })
	OnUploadFile(h, func(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
		return uploadWith(ctx, getAPI(ctx), in)
	})
	OnDownloadFile(h, func(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
		return downloadWith(ctx, getAPI(ctx), in)
	})
	OnExportFile(h, func(ctx plugin.Ctx, in *ExportFileIn) (*ExportFileOut, error) {
		return exportWith(ctx, getAPI(ctx), in)
	})
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return getAPI(ctx).health(ctx) })
}
