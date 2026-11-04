package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnVerifyCredential(h, func(c plugin.Ctx, in *VerifyCredentialIn) (*VerifyCredentialOut, error) {
		return getAPI(c).VerifyCredential(c, in)
	})
	OnWebSearch(h, func(c plugin.Ctx, in *WebSearchIn) (*WebSearchOut, error) { return getAPI(c).WebSearch(c, in) })
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "bocha-search"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c).AccessToken) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
