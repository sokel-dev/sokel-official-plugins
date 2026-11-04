package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.Env("TIKA_URL"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnExtractText(h, func(ctx plugin.Ctx, in *ExtractTextIn) (*ExtractTextOut, error) {
		return clientFor(ctx).extractText(ctx, in)
	})
	OnExtractMetadata(h, func(ctx plugin.Ctx, in *ExtractMetadataIn) (*ExtractMetadataOut, error) {
		return clientFor(ctx).extractMetadata(ctx, in)
	})
	OnExtractRecursive(h, func(ctx plugin.Ctx, in *ExtractRecursiveIn) (*ExtractRecursiveOut, error) {
		return clientFor(ctx).extractRecursive(ctx, in)
	})
	OnDetectType(h, func(ctx plugin.Ctx, in *DetectTypeIn) (*DetectTypeOut, error) {
		return clientFor(ctx).detectType(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "apache-tika"})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
