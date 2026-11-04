package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnGetCompany(h, func(c plugin.Ctx, in *GetCompanyIn) (*GetCompanyOut, error) { return getAPI(c).GetCompany(c, in) })
	OnListCompanyChanges(h, func(c plugin.Ctx, in *ListCompanyChangesIn) (*ListCompanyChangesOut, error) {
		return getAPI(c).ListCompanyChanges(c, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "tianyancha"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c)) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
