package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	return newClient(sokel.EnvOr("API_BASE_URL", "https://api.dingtalk.com"), sokel.CredentialAs[Cred](ctx))
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	DeclareEvents(p)
	OnGroupMessageSend(p, func(ctx plugin.Ctx, in *GroupMessageSendIn) (*GroupMessageSendOut, error) {
		return clientFor(ctx).GroupMessageSend(ctx, in)
	})
	OnDirectMessageSend(p, func(ctx plugin.Ctx, in *DirectMessageSendIn) (*DirectMessageSendOut, error) {
		return clientFor(ctx).DirectMessageSend(ctx, in)
	})
	OnMessageFileURL(p, func(ctx plugin.Ctx, in *MessageFileURLIn) (*MessageFileURLOut, error) {
		return clientFor(ctx).MessageFileURL(ctx, in)
	})
	OnHealthCheck(p, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).HealthCheck(ctx, in)
	})
	sokel.RegisterWebhook(p, webhook)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "dingtalk"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
