package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	cred := sokel.CredentialAs[Cred](ctx)
	return newClient(&cred)
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).health_check(ctx, in)
	})
	OnListContacts(h, func(ctx plugin.Ctx, in *ListContactsIn) (*ListContactsOut, error) {
		return clientFor(ctx).list_contacts(ctx, in)
	})
	OnGetContact(h, func(ctx plugin.Ctx, in *GetContactIn) (*GetContactOut, error) {
		return clientFor(ctx).get_contact(ctx, in)
	})
	OnCreateContact(h, func(ctx plugin.Ctx, in *CreateContactIn) (*CreateContactOut, error) {
		return clientFor(ctx).create_contact(ctx, in)
	})
	OnUpdateContact(h, func(ctx plugin.Ctx, in *UpdateContactIn) (*UpdateContactOut, error) {
		return clientFor(ctx).update_contact(ctx, in)
	})
	OnListLists(h, func(ctx plugin.Ctx, in *ListListsIn) (*ListListsOut, error) {
		return clientFor(ctx).list_lists(ctx, in)
	})
	OnGetList(h, func(ctx plugin.Ctx, in *GetListIn) (*GetListOut, error) { return clientFor(ctx).get_list(ctx, in) })
	OnSendEmail(h, func(ctx plugin.Ctx, in *SendEmailIn) (*SendEmailOut, error) {
		return clientFor(ctx).send_email(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "brevo"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
