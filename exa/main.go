package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/exa.md
var usageDoc string

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "exa"})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func register(p plugin.Host) {
	OnSearch(p, opSearch)
	OnGetContents(p, opGetContents)
	OnAnswer(p, opAnswer)
	OnAgentStart(p, opAgentStart)
	OnAgentStatus(p, opAgentStatus)
	OnHealthCheck(p, opHealthCheck)
}
