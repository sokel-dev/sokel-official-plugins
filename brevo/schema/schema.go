package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type PageOptions struct {
	Limit  *int64 `json:"limit" sokel:"limit,optional" label:"Page size"`
	Offset *int64 `json:"offset" sokel:"offset,optional" label:"Offset"`
}
type Contact struct {
	ID               string         `json:"id" sokel:"id" label:"Contact ID"`
	Email            string         `json:"email" sokel:"email" label:"Email address"`
	EmailBlacklisted bool           `json:"email_blacklisted" sokel:"email_blacklisted" label:"Email blocked"`
	SmsBlacklisted   bool           `json:"sms_blacklisted" sokel:"sms_blacklisted" label:"SMS blocked"`
	ListIDs          []string       `json:"list_ids" sokel:"list_ids" label:"List IDs"`
	Attributes       map[string]any `json:"attributes" sokel:"attributes" label:"Contact attributes" opaque:"Account-defined attribute values"`
	CreatedAt        string         `json:"created_at" sokel:"created_at" label:"Created at"`
	ModifiedAt       string         `json:"modified_at" sokel:"modified_at" label:"Modified at"`
}
type UpdateContactOptions struct {
	EmailBlacklisted *bool `json:"email_blacklisted" sokel:"email_blacklisted,optional" label:"Block email" desc:"Omit to preserve the current block status; false explicitly allows email"`
}
type ContactList struct {
	ID                string `json:"id" sokel:"id" label:"List ID"`
	Name              string `json:"name" sokel:"name" label:"List name"`
	FolderID          string `json:"folder_id" sokel:"folder_id" label:"Folder ID"`
	UniqueSubscribers int64  `json:"unique_subscribers" sokel:"unique_subscribers" label:"Unique contacts"`
}
type Recipient struct {
	Email string `json:"email" sokel:"email" label:"Email address"`
	Name  string `json:"name" sokel:"name,optional" label:"Display name"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", Desc: "Check credentials", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type ListContacts struct{}

func (ListContacts) Meta() contract.Meta {
	return contract.Meta{ID: "list_contacts", Label: "List contacts", Desc: "List contacts", TimeoutSec: 60}
}
func (ListContacts) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("modified_since").Label("Modified since").Desc("RFC 3339 timestamp including a timezone").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListContacts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("contacts", []Contact{}).Label("Contacts"), field.Int("total").Label("Total count"), field.Bool("has_more").Label("More results"), field.Int("next_offset").Label("Next offset")}
}

type GetContact struct{}

func (GetContact) Meta() contract.Meta {
	return contract.Meta{ID: "get_contact", Label: "Get contact", Desc: "Get contact", TimeoutSec: 60}
}
func (GetContact) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("contact_id").Label("Contact ID")}
}
func (GetContact) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("contact", Contact{}).Label("Contact")}
}

type CreateContact struct{}

func (CreateContact) Meta() contract.Meta {
	return contract.Meta{ID: "create_contact", Label: "Create contact", Desc: "Create contact", TimeoutSec: 60}
}
func (CreateContact) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email").Label("Email address"), field.Enum("email_status", contract.Option{Value: "blocked"}, contract.Option{Value: "allowed"}).Label("New contact email status").Desc("Explicitly choose whether the new contact is blocked from email"), field.Object("attributes", "Account-defined uppercase attribute names; values must match account types. EMAIL updates are not supported").Label("Contact attributes").Optional(), field.Strings("list_ids").Label("List IDs").Desc("Positive decimal IDs as strings; adds membership to these existing lists").Optional()}
}
func (CreateContact) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("contact_id").Label("Contact ID")}
}

type UpdateContact struct{}

func (UpdateContact) Meta() contract.Meta {
	return contract.Meta{ID: "update_contact", Label: "Update contact", Desc: "Update contact", TimeoutSec: 60}
}
func (UpdateContact) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("contact_id").Label("Contact ID"), field.Object("attributes", "Account-defined uppercase attribute names; values must match account types. EMAIL updates are not supported").Label("Contact attributes").Optional(), field.Strings("list_ids").Label("List IDs").Desc("Positive decimal IDs as strings; adds membership to these existing lists").Optional(), field.Json("options", UpdateContactOptions{}).Label("Optional settings").Optional()}
}
func (UpdateContact) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("accepted").Label("Update accepted")}
}

type ListLists struct{}

func (ListLists) Meta() contract.Meta {
	return contract.Meta{ID: "list_lists", Label: "List contact lists", Desc: "List contact lists", TimeoutSec: 60}
}
func (ListLists) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListLists) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("lists", []ContactList{}).Label("Contact lists"), field.Int("total").Label("Total count"), field.Bool("has_more").Label("More results"), field.Int("next_offset").Label("Next offset")}
}

type GetList struct{}

func (GetList) Meta() contract.Meta {
	return contract.Meta{ID: "get_list", Label: "Get contact list", Desc: "Get contact list", TimeoutSec: 60}
}
func (GetList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("List ID")}
}
func (GetList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("list", ContactList{}).Label("Contact list")}
}

type SendEmail struct{}

func (SendEmail) Meta() contract.Meta {
	return contract.Meta{ID: "send_email", Label: "Send transactional email", Desc: "Explicitly send one transactional message to up to 99 recipients; acceptance does not prove delivery", TimeoutSec: 60}
}
func (SendEmail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("sender_email").Label("Sender email"), field.String("sender_name").Label("Sender name").Optional(), field.Array("to", []Recipient{}).Label("Recipients"), field.String("subject").Label("Subject"), field.Enum("content_type", contract.Option{Value: "text"}, contract.Option{Value: "html"}).Label("Body format"), field.String("body").Label("Email body")}
}
func (SendEmail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("message_id").Label("Message ID"), field.Bool("accepted").Label("Sending request accepted")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Brevo API v3 key; SMTP passwords are not API keys")}
}
