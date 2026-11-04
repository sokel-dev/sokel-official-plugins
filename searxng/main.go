package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/searxng.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnHealth(h, func(ctx plugin.Ctx, in *HealthIn) (*HealthOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).health(ctx, in)
	})
	OnSearch(h, func(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).search(ctx, in)
	})
	OnGetConfig(h, func(ctx plugin.Ctx, in *GetConfigIn) (*GetConfigOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getConfig(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "searxng", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
