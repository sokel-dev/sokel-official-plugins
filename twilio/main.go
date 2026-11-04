package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/twilio.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnGetAccount(h, func(ctx plugin.Ctx, in *GetAccountIn) (*GetAccountOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getAccount(ctx, in)
	})
	OnSendMessage(h, func(ctx plugin.Ctx, in *SendMessageIn) (*SendMessageOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).sendMessage(ctx, in)
	})
	OnGetMessage(h, func(ctx plugin.Ctx, in *GetMessageIn) (*GetMessageOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getMessage(ctx, in)
	})
	OnListMessages(h, func(ctx plugin.Ctx, in *ListMessagesIn) (*ListMessagesOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listMessages(ctx, in)
	})
	OnGetIncomingNumber(h, func(ctx plugin.Ctx, in *GetIncomingNumberIn) (*GetIncomingNumberOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getIncomingNumber(ctx, in)
	})
	OnListIncomingNumbers(h, func(ctx plugin.Ctx, in *ListIncomingNumbersIn) (*ListIncomingNumbersOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listIncomingNumbers(ctx, in)
	})
	OnListMessageMedia(h, func(ctx plugin.Ctx, in *ListMessageMediaIn) (*ListMessageMediaOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listMessageMedia(ctx, in)
	})
}

func main() {
	p := sokel.New(sokel.Config{Name: "twilio", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
