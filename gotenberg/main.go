package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.Env("GOTENBERG_URL"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).healthCheck(ctx, in)
	})
	OnHTMLToPdf(h, func(ctx plugin.Ctx, in *HTMLToPdfIn) (*HTMLToPdfOut, error) { return clientFor(ctx).htmlToPDF(ctx, in) })
	OnOfficeToPdf(h, func(ctx plugin.Ctx, in *OfficeToPdfIn) (*OfficeToPdfOut, error) {
		return clientFor(ctx).officeToPDF(ctx, in)
	})
	OnMergePdfs(h, func(ctx plugin.Ctx, in *MergePdfsIn) (*MergePdfsOut, error) { return clientFor(ctx).mergePDFs(ctx, in) })
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "gotenberg"})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
