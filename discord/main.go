// discord —— Sokel first-party publisher plugin: sends messages to a channel.
//
// Same publish contract as bluesky / mastodon, but a different purpose: **community distribution,
// not a public discovery channel**. Use it to push a report to the research team's channel once it's
// out; use the other two for public exposure.
//
// The three design decisions are at the top of schema/schema.go: webhook instead of bot, embed card
// as the primary form, and the receipt must include the message id.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./discord
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
		Name:     "discord",
	})

	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnDiscordMessageSend(p, opMessageSend)
	OnDiscordMessageDelete(p, opMessageDelete)
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
