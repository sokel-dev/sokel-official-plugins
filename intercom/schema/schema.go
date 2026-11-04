package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Page struct {
	Limit  *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Cursor *string `json:"cursor,omitempty" sokel:"cursor,optional" label:"Pagination cursor"`
}
type Contact struct {
	ID           string  `json:"id" sokel:"id" label:"Contact ID"`
	Role         string  `json:"role" sokel:"role" label:"Contact role"`
	Email        *string `json:"email,omitempty" sokel:"email,optional" label:"Email"`
	ExternalID   *string `json:"external_id,omitempty" sokel:"external_id,optional" label:"External ID"`
	Name         *string `json:"name,omitempty" sokel:"name,optional" label:"Name"`
	Phone        *string `json:"phone,omitempty" sokel:"phone,optional" label:"Phone"`
	Unsubscribed bool    `json:"unsubscribed_from_emails" sokel:"unsubscribed_from_emails" label:"Unsubscribed from emails"`
}
type ContactFields struct {
	Name         *string `json:"name,omitempty" sokel:"name,optional" label:"Name"`
	Phone        *string `json:"phone,omitempty" sokel:"phone,optional" label:"Phone"`
	Unsubscribed *bool   `json:"unsubscribed_from_emails,omitempty" sokel:"unsubscribed_from_emails,optional" label:"Unsubscribed from emails"`
}
type Conversation struct {
	ID        string  `json:"id" sokel:"id" label:"Conversation ID"`
	Title     *string `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	State     string  `json:"state" sokel:"state" label:"State"`
	Open      bool    `json:"open" sokel:"open" label:"Open"`
	Read      bool    `json:"read" sokel:"read" label:"Read"`
	CreatedAt int64   `json:"created_at" sokel:"created_at" label:"Created at in Unix seconds"`
	UpdatedAt int64   `json:"updated_at" sokel:"updated_at" label:"Updated at in Unix seconds"`
}
type Part struct {
	ID        string  `json:"id" sokel:"id" label:"Part ID"`
	Type      string  `json:"part_type" sokel:"part_type" label:"Part type"`
	Body      *string `json:"body,omitempty" sokel:"body,optional" label:"Part body"`
	CreatedAt int64   `json:"created_at" sokel:"created_at" label:"Created at in Unix seconds"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListContacts struct{}

func (ListContacts) Meta() contract.Meta {
	return contract.Meta{ID: "list_contacts", Label: "List contacts", TimeoutSec: 60}
}
func (ListContacts) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListContacts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("contacts", []Contact{}).Label("Contacts"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetContact struct{}

func (GetContact) Meta() contract.Meta {
	return contract.Meta{ID: "get_contact", Label: "Get contact", TimeoutSec: 60}
}
func (GetContact) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("contact_id").Label("Contact ID")}
}
func (GetContact) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("contact", Contact{}).Label("Contact")}
}

type CreateContact struct{}

func (CreateContact) Meta() contract.Meta {
	return contract.Meta{ID: "create_contact", Label: "Create contact", TimeoutSec: 60}
}
func (CreateContact) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email").Label("Email"), field.String("role").Label("Contact role").Optional(), field.Json("fields", ContactFields{}).Label("Contact fields").Optional()}
}
func (CreateContact) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("contact", Contact{}).Label("Created contact")}
}

type UpdateContact struct{}

func (UpdateContact) Meta() contract.Meta {
	return contract.Meta{ID: "update_contact", Label: "Update contact", TimeoutSec: 60}
}
func (UpdateContact) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("contact_id").Label("Contact ID"), field.Json("fields", ContactFields{}).Label("Contact fields")}
}
func (UpdateContact) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("contact", Contact{}).Label("Updated contact")}
}

type SearchContacts struct{}

func (SearchContacts) Meta() contract.Meta {
	return contract.Meta{ID: "search_contacts", Label: "Search contacts by email", TimeoutSec: 60}
}
func (SearchContacts) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email").Label("Email"), field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (SearchContacts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("contacts", []Contact{}).Label("Contacts"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type ListConversations struct{}

func (ListConversations) Meta() contract.Meta {
	return contract.Meta{ID: "list_conversations", Label: "List conversations", TimeoutSec: 60}
}
func (ListConversations) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListConversations) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("conversations", []Conversation{}).Label("Conversations"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetConversation struct{}

func (GetConversation) Meta() contract.Meta {
	return contract.Meta{ID: "get_conversation", Label: "Get conversation", TimeoutSec: 60}
}
func (GetConversation) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("conversation_id").Label("Conversation ID")}
}
func (GetConversation) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("conversation", Conversation{}).Label("Conversation"), field.Json("source", MessageSource{}).Label("Opening message"), field.Array("parts", []Part{}).Label("Conversation parts"), field.Int("parts_total").Label("Reported part count"), field.Bool("parts_may_be_truncated").Label("Parts may be truncated")}
}

type ReplyConversation struct{}

func (ReplyConversation) Meta() contract.Meta {
	return contract.Meta{ID: "reply_conversation", Label: "Reply to conversation", TimeoutSec: 60}
}
func (ReplyConversation) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("conversation_id").Label("Conversation ID"), field.String("admin_id").Label("Replying admin ID"), field.String("body").Label("Reply body"), field.String("message_type").Label("Reply type: comment or note").Optional()}
}
func (ReplyConversation) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("conversation", Conversation{}).Label("Conversation")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Use a private app access token with the required contact and conversation permissions."), field.Text("region").Label("Workspace region: us, eu or au").Optional().Desc("Leave blank for US. Select the region hosting your Intercom workspace; no custom URL is accepted.")}
}

type MessageSource struct {
	Body *string `json:"body,omitempty" sokel:"body,optional" label:"Opening message body"`
}
