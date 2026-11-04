package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/*.md
var docs embed.FS

func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/intercom.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListContacts(h, func(ctx plugin.Ctx, in *ListContactsIn) (*ListContactsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_contacts(ctx, in)
	})
	OnGetContact(h, func(ctx plugin.Ctx, in *GetContactIn) (*GetContactOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_contact(ctx, in)
	})
	OnCreateContact(h, func(ctx plugin.Ctx, in *CreateContactIn) (*CreateContactOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_contact(ctx, in)
	})
	OnUpdateContact(h, func(ctx plugin.Ctx, in *UpdateContactIn) (*UpdateContactOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).update_contact(ctx, in)
	})
	OnSearchContacts(h, func(ctx plugin.Ctx, in *SearchContactsIn) (*SearchContactsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).search_contacts(ctx, in)
	})
	OnListConversations(h, func(ctx plugin.Ctx, in *ListConversationsIn) (*ListConversationsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_conversations(ctx, in)
	})
	OnGetConversation(h, func(ctx plugin.Ctx, in *GetConversationIn) (*GetConversationOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_conversation(ctx, in)
	})
	OnReplyConversation(h, func(ctx plugin.Ctx, in *ReplyConversationIn) (*ReplyConversationOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).reply_conversation(ctx, in)
	})
}

func main() {
	p := sokel.New(sokel.Config{Name: "intercom", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
