package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.Env("GRAFANA_URL"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).healthCheck(ctx, in)
	})
	OnListDataSources(h, func(ctx plugin.Ctx, in *ListDataSourcesIn) (*ListDataSourcesOut, error) {
		return clientFor(ctx).listDataSources(ctx, in)
	})
	OnGetDataSource(h, func(ctx plugin.Ctx, in *GetDataSourceIn) (*GetDataSourceOut, error) {
		return clientFor(ctx).getDataSource(ctx, in)
	})
	OnSearchDashboards(h, func(ctx plugin.Ctx, in *SearchDashboardsIn) (*SearchDashboardsOut, error) {
		return clientFor(ctx).searchDashboards(ctx, in)
	})
	OnGetDashboard(h, func(ctx plugin.Ctx, in *GetDashboardIn) (*GetDashboardOut, error) {
		return clientFor(ctx).getDashboard(ctx, in)
	})
	OnListAnnotations(h, func(ctx plugin.Ctx, in *ListAnnotationsIn) (*ListAnnotationsOut, error) {
		return clientFor(ctx).listAnnotations(ctx, in)
	})
	OnCreateAnnotation(h, func(ctx plugin.Ctx, in *CreateAnnotationIn) (*CreateAnnotationOut, error) {
		return clientFor(ctx).createAnnotation(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "grafana"})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
