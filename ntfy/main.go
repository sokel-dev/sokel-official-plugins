package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/ntfy.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnHealth(h, func(ctx plugin.Ctx, in *HealthIn) (*HealthOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).health(ctx, in)
	})
	OnPublish(h, func(ctx plugin.Ctx, in *PublishIn) (*PublishOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).publish(ctx, in)
	})
	OnPoll(h, func(ctx plugin.Ctx, in *PollIn) (*PollOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).poll(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "ntfy", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
