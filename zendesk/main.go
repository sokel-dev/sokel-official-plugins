package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "zendesk"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c)) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(c).health_check(c, in) })
	OnListTickets(h, func(c plugin.Ctx, in *ListTicketsIn) (*ListTicketsOut, error) { return client(c).list_tickets(c, in) })
	OnSearchTickets(h, func(c plugin.Ctx, in *SearchTicketsIn) (*SearchTicketsOut, error) {
		return client(c).search_tickets(c, in)
	})
	OnGetTicket(h, func(c plugin.Ctx, in *GetTicketIn) (*GetTicketOut, error) { return client(c).get_ticket(c, in) })
	OnCreateTicket(h, func(c plugin.Ctx, in *CreateTicketIn) (*CreateTicketOut, error) {
		return client(c).create_ticket(c, in)
	})
	OnUpdateTicket(h, func(c plugin.Ctx, in *UpdateTicketIn) (*UpdateTicketOut, error) {
		return client(c).update_ticket(c, in)
	})
	OnListComments(h, func(c plugin.Ctx, in *ListCommentsIn) (*ListCommentsOut, error) {
		return client(c).list_comments(c, in)
	})
	OnAddComment(h, func(c plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) { return client(c).add_comment(c, in) })
}
