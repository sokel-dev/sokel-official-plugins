package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/resend.md
var usageDoc string

func registerOperations(h plugin.Host) { registerWithClient(h, newClient) }
func registerWithClient(h plugin.Host, makeClient func(Cred) *client) {
	OnSendEmail(h, func(ctx plugin.Ctx, in *SendEmailIn) (*SendEmailOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).sendEmail(ctx, in)
	})
	OnGetEmail(h, func(ctx plugin.Ctx, in *GetEmailIn) (*GetEmailOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getEmail(ctx, in)
	})
	OnCancelEmail(h, func(ctx plugin.Ctx, in *CancelEmailIn) (*CancelEmailOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).cancelEmail(ctx, in)
	})
	OnGetReceivedEmail(h, func(ctx plugin.Ctx, in *GetReceivedEmailIn) (*GetReceivedEmailOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getReceivedEmail(ctx, in)
	})
	OnListEmails(h, func(ctx plugin.Ctx, in *ListEmailsIn) (*ListEmailsOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listEmails(ctx, in)
	})
	OnListReceivedEmails(h, func(ctx plugin.Ctx, in *ListReceivedEmailsIn) (*ListReceivedEmailsOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listReceivedEmails(ctx, in)
	})
	OnGetDomain(h, func(ctx plugin.Ctx, in *GetDomainIn) (*GetDomainOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).getDomain(ctx, in)
	})
	OnListDomains(h, func(ctx plugin.Ctx, in *ListDomainsIn) (*ListDomainsOut, error) {
		return makeClient(sokel.CredentialAs[Cred](ctx)).listDomains(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "resend", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")
	registerOperations(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
