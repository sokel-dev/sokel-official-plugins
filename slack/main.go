package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate -lang go .

import (
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client { return newClient(sokel.CredentialAs[Cred](ctx).Token) }
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	DeclareEvents(p)
	registerOperations(p)
	sokel.RegisterSource(p, sokel.Source{ID: "messages", Label: "New-message polling"}, runSource)
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "slack"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func healthCheck(ctx plugin.Ctx) (*HealthCheckOut, error) {
	out, err := clientFor(ctx).health(ctx)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	out.Message = "Slack access token is valid."
	return out, nil
}

func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return healthCheck(ctx) })
	OnSlackHistory(h, func(ctx plugin.Ctx, in *SlackHistoryIn) (*SlackHistoryOut, error) {
		return clientFor(ctx).history(ctx, in)
	})
	OnSlackReplies(h, func(ctx plugin.Ctx, in *SlackRepliesIn) (*SlackRepliesOut, error) {
		return clientFor(ctx).replies(ctx, in)
	})
	OnSlackSend(h, func(ctx plugin.Ctx, in *SlackSendIn) (*SlackSendOut, error) { return clientFor(ctx).send(ctx, in) })
	OnSlackUpload(h, func(ctx plugin.Ctx, in *SlackUploadIn) (*SlackUploadOut, error) {
		if err := validateUpload(in); err != nil {
			return nil, err
		}
		data, err := in.File.Blob(ctx)
		if err != nil {
			return nil, errors.New("Failed to read the platform file. Check file access and try again.")
		}
		return clientFor(ctx).upload(ctx, in, data)
	})
}
