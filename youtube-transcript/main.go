// youtube-transcript — a first-party plugin for fetching YouTube transcripts (Go implementation).
//
// The fetching approach is modeled on the Python jdepoix/youtube-transcript-api: it uses the same
// undocumented API the YouTube web client itself relies on, needing no API key and no browser. See the
// top of schema/schema.go for the contract and design tradeoffs.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./youtube-transcript
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "youtube-transcript",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnTranscriptFetch(p, opFetch)
	OnTranscriptList(p, opList)
	OnHealthCheck(p, opHealthCheck)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
