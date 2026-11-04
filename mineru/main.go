package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/mineru.md
var usageDoc string

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "mineru"})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
func register(p plugin.Host) {
	OnSubmit(p, opSubmit)
	OnStatus(p, opStatus)
	OnBatchSubmit(p, opBatchSubmit)
	OnBatchStatus(p, opBatchStatus)
	OnHealthCheck(p, opHealthCheck)
}
