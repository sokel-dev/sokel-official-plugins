package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Ticket struct {
	ID          string   `json:"id" sokel:"id" label:"Ticket ID"`
	Subject     string   `json:"subject" sokel:"subject" label:"Subject"`
	Description string   `json:"description" sokel:"description" label:"Description"`
	Status      string   `json:"status" sokel:"status" label:"Status"`
	Priority    string   `json:"priority" sokel:"priority" label:"Priority"`
	RequesterID string   `json:"requester_id" sokel:"requester_id" label:"Requester ID"`
	AssigneeID  string   `json:"assignee_id" sokel:"assignee_id" label:"Assignee ID"`
	Tags        []string `json:"tags" sokel:"tags" label:"Tags"`
	CreatedAt   string   `json:"created_at" sokel:"created_at" label:"Created at"`
	UpdatedAt   string   `json:"updated_at" sokel:"updated_at" label:"Updated at"`
}
type Comment struct {
	ID        string `json:"id" sokel:"id" label:"Comment ID"`
	AuthorID  string `json:"author_id" sokel:"author_id" label:"Author ID"`
	Body      string `json:"body" sokel:"body" label:"Comment body"`
	Public    bool   `json:"public" sokel:"public" label:"Public reply"`
	CreatedAt string `json:"created_at" sokel:"created_at" label:"Created at"`
}
type TicketChanges struct {
	Subject    *string   `json:"subject,omitempty" sokel:"subject,optional" label:"Subject"`
	Status     *string   `json:"status,omitempty" sokel:"status,optional" label:"Status"`
	Priority   *string   `json:"priority,omitempty" sokel:"priority,optional" label:"Priority (empty clears)"`
	AssigneeID *string   `json:"assignee_id,omitempty" sokel:"assignee_id,optional" label:"Assignee ID (empty clears)"`
	Tags       *[]string `json:"tags,omitempty" sokel:"tags,optional" label:"Tags (replaces all)"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type ListTickets struct{}

func (ListTickets) Meta() contract.Meta {
	return contract.Meta{ID: "list_tickets", Label: "List tickets", TimeoutSec: 60}
}
func (ListTickets) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional()}
}
func (ListTickets) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tickets", []Ticket{}).Label("Tickets"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type SearchTickets struct{}

func (SearchTickets) Meta() contract.Meta {
	return contract.Meta{ID: "search_tickets", Label: "Search tickets", TimeoutSec: 60}
}
func (SearchTickets) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Ticket search query"), field.Int("page").Label("Page number").Optional()}
}
func (SearchTickets) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tickets", []Ticket{}).Label("Tickets"), field.Int("next_page").Label("Next page number"), field.Bool("has_more").Label("More results"), field.Int("total_count").Label("Total matches"), field.Bool("truncated").Label("More than 1000 matches")}
}

type GetTicket struct{}

func (GetTicket) Meta() contract.Meta {
	return contract.Meta{ID: "get_ticket", Label: "Get ticket", TimeoutSec: 60}
}
func (GetTicket) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("ticket_id").Label("Ticket ID")}
}
func (GetTicket) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("ticket", Ticket{}).Label("Ticket")}
}

type CreateTicket struct{}

func (CreateTicket) Meta() contract.Meta {
	return contract.Meta{ID: "create_ticket", Label: "Create ticket", TimeoutSec: 60}
}
func (CreateTicket) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("subject").Label("Subject"), field.String("body").Label("Initial comment"), field.String("requester_id").Label("Requester ID"), field.Bool("public").Label("Public reply"), field.Json("details", TicketChanges{}).Label("Optional ticket fields").Optional()}
}
func (CreateTicket) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("ticket", Ticket{}).Label("Ticket")}
}

type UpdateTicket struct{}

func (UpdateTicket) Meta() contract.Meta {
	return contract.Meta{ID: "update_ticket", Label: "Update ticket", TimeoutSec: 60}
}
func (UpdateTicket) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("ticket_id").Label("Ticket ID"), field.Json("changes", TicketChanges{}).Label("Changed fields"), field.String("updated_stamp").Label("Expected updated_at timestamp").Optional()}
}
func (UpdateTicket) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("ticket", Ticket{}).Label("Ticket")}
}

type ListComments struct{}

func (ListComments) Meta() contract.Meta {
	return contract.Meta{ID: "list_comments", Label: "List ticket comments", TimeoutSec: 60}
}
func (ListComments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("ticket_id").Label("Ticket ID"), field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional()}
}
func (ListComments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("comments", []Comment{}).Label("Comments"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type AddComment struct{}

func (AddComment) Meta() contract.Meta {
	return contract.Meta{ID: "add_comment", Label: "Add ticket comment", TimeoutSec: 60}
}
func (AddComment) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("ticket_id").Label("Ticket ID"), field.String("body").Label("Comment body"), field.Bool("public").Label("Public reply"), field.String("updated_stamp").Label("Expected updated_at timestamp").Optional()}
}
func (AddComment) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("ticket", Ticket{}).Label("Updated ticket")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("subdomain").Label("Zendesk subdomain").Desc("Use the account name before .zendesk.com, without a URL"), field.Text("email").Label("Agent email").Desc("Email of an agent or administrator with access to the target tickets"), field.Secret("api_token").Label("API token").Desc("Create an API token in Zendesk Admin Center; API token access must be enabled")}
}
