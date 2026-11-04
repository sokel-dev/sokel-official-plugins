package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/prometheus.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnGetBuildInfo(h, func(ctx plugin.Ctx, in *GetBuildInfoIn) (*GetBuildInfoOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getBuildInfo(ctx, in)
	})
	OnQuery(h, func(ctx plugin.Ctx, in *QueryIn) (*QueryOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).query(ctx, in)
	})
	OnQueryRange(h, func(ctx plugin.Ctx, in *QueryRangeIn) (*QueryRangeOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).queryRange(ctx, in)
	})
	OnListLabelNames(h, func(ctx plugin.Ctx, in *ListLabelNamesIn) (*ListLabelNamesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listLabelNames(ctx, in)
	})
	OnListLabelValues(h, func(ctx plugin.Ctx, in *ListLabelValuesIn) (*ListLabelValuesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listLabelValues(ctx, in)
	})
	OnFindSeries(h, func(ctx plugin.Ctx, in *FindSeriesIn) (*FindSeriesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).findSeries(ctx, in)
	})
	OnListAlerts(h, func(ctx plugin.Ctx, in *ListAlertsIn) (*ListAlertsOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listAlerts(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "prometheus", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
