package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("account_sid").Label("Account SID").Desc("Twilio account or subaccount SID (AC followed by 32 hexadecimal characters)."), field.Secret("auth_token").Label("Auth token").Desc("Use the Auth Token belonging to this account. API-key SIDs and OAuth tokens are not supported in this version.")}
}

type PageOptions struct {
	Size        *int    `json:"size,omitempty" sokel:"size,optional" label:"Page size"`
	NextPageURI *string `json:"next_page_uri,omitempty" sokel:"next_page_uri,optional" label:"Next page URI"`
}
type MessageFilters struct {
	To         *string `json:"to,omitempty" sokel:"to,optional" label:"Recipient filter"`
	From       *string `json:"from,omitempty" sokel:"from,optional" label:"Sender filter"`
	DateSent   *string `json:"date_sent,omitempty" sokel:"date_sent,optional" label:"Sent date (UTC)"`
	SentAfter  *string `json:"sent_after,omitempty" sokel:"sent_after,optional" label:"Sent after date (UTC)"`
	SentBefore *string `json:"sent_before,omitempty" sokel:"sent_before,optional" label:"Sent before date (UTC)"`
}
type SendOptions struct {
	To                  string   `json:"to" sokel:"to" label:"Recipient phone number"`
	From                *string  `json:"from,omitempty" sokel:"from,optional" label:"Sender phone number or ID"`
	MessagingServiceSID *string  `json:"messaging_service_sid,omitempty" sokel:"messaging_service_sid,optional" label:"Messaging Service SID"`
	Body                *string  `json:"body,omitempty" sokel:"body,optional" label:"Message body"`
	MediaURLs           []string `json:"media_urls,omitempty" sokel:"media_urls,optional" label:"Media URLs"`
	SmartEncoded        *bool    `json:"smart_encoded,omitempty" sokel:"smart_encoded,optional" label:"Use Smart Encoding"`
	ValidityPeriod      *int     `json:"validity_period,omitempty" sokel:"validity_period,optional" label:"Queue validity in seconds"`
}
type Account struct {
	SID          string `json:"sid" sokel:"sid" label:"Account SID"`
	FriendlyName string `json:"friendly_name" sokel:"friendly_name" label:"Friendly name"`
	Status       string `json:"status" sokel:"status" label:"Account status"`
	Type         string `json:"type" sokel:"type" label:"Account type"`
	DateCreated  string `json:"date_created" sokel:"date_created" label:"Created at"`
}
type Message struct {
	SID                 string  `json:"sid" sokel:"sid" label:"Message SID"`
	AccountSID          string  `json:"account_sid" sokel:"account_sid" label:"Account SID"`
	From                *string `json:"from,omitempty" sokel:"from,optional" label:"Sender"`
	To                  string  `json:"to" sokel:"to" label:"Recipient"`
	Body                string  `json:"body" sokel:"body" label:"Message body"`
	Status              string  `json:"status" sokel:"status" label:"Message status"`
	Direction           string  `json:"direction" sokel:"direction" label:"Direction"`
	DateCreated         string  `json:"date_created" sokel:"date_created" label:"Created at"`
	DateSent            *string `json:"date_sent,omitempty" sokel:"date_sent,optional" label:"Sent at"`
	DateUpdated         string  `json:"date_updated" sokel:"date_updated" label:"Updated at"`
	NumSegments         string  `json:"num_segments" sokel:"num_segments" label:"Segment count"`
	NumMedia            string  `json:"num_media" sokel:"num_media" label:"Media count"`
	Price               *string `json:"price,omitempty" sokel:"price,optional" label:"Price"`
	PriceUnit           *string `json:"price_unit,omitempty" sokel:"price_unit,optional" label:"Price currency"`
	ErrorCode           *int    `json:"error_code,omitempty" sokel:"error_code,optional" label:"Delivery error code"`
	ErrorMessage        *string `json:"error_message,omitempty" sokel:"error_message,optional" label:"Delivery error message"`
	MessagingServiceSID *string `json:"messaging_service_sid,omitempty" sokel:"messaging_service_sid,optional" label:"Messaging Service SID"`
}
type NumberCapabilities struct {
	SMS   bool `json:"sms" sokel:"sms" label:"SMS supported"`
	MMS   bool `json:"mms" sokel:"mms" label:"MMS supported"`
	Voice bool `json:"voice" sokel:"voice" label:"Voice supported"`
}
type IncomingNumber struct {
	SID          string             `json:"sid" sokel:"sid" label:"Phone number SID"`
	AccountSID   string             `json:"account_sid" sokel:"account_sid" label:"Account SID"`
	PhoneNumber  string             `json:"phone_number" sokel:"phone_number" label:"Phone number"`
	FriendlyName string             `json:"friendly_name" sokel:"friendly_name" label:"Friendly name"`
	Capabilities NumberCapabilities `json:"capabilities" sokel:"capabilities" label:"Capabilities"`
	Status       string             `json:"status" sokel:"status" label:"Number status"`
	DateCreated  string             `json:"date_created" sokel:"date_created" label:"Created at"`
}
type Media struct {
	SID         string `json:"sid" sokel:"sid" label:"Media SID"`
	ParentSID   string `json:"parent_sid" sokel:"parent_sid" label:"Message SID"`
	AccountSID  string `json:"account_sid" sokel:"account_sid" label:"Account SID"`
	ContentType string `json:"content_type" sokel:"content_type" label:"Content type"`
	DateCreated string `json:"date_created" sokel:"date_created" label:"Created at"`
	URI         string `json:"uri" sokel:"uri" label:"Media metadata URI"`
}
type GetAccount struct{}

func (GetAccount) Meta() contract.Meta {
	return contract.Meta{ID: "get_account", Label: "Get account", Desc: "Read account identity without returning its Auth Token.", TimeoutSec: 60}
}
func (GetAccount) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (GetAccount) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("account", Account{}).Label("Account")}
}

type SendMessage struct{}

func (SendMessage) Meta() contract.Meta {
	return contract.Meta{ID: "send_message", Label: "Send SMS or MMS", Desc: "Send one message. This may incur charges. The returned status does not guarantee delivery; requests are never automatically retried.", TimeoutSec: 60}
}
func (SendMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("message", SendOptions{}).Label("Message")}
}
func (SendMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("message", Message{}).Label("Message")}
}

type GetMessage struct{}

func (GetMessage) Meta() contract.Meta {
	return contract.Meta{ID: "get_message", Label: "Get message", TimeoutSec: 60}
}
func (GetMessage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("message_sid").Label("Message SID")}
}
func (GetMessage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("message", Message{}).Label("Message")}
}

type ListMessages struct{}

func (ListMessages) Meta() contract.Meta {
	return contract.Meta{ID: "list_messages", Label: "List messages", Desc: "Read one page of message history. Reuse next_page_uri without changing filters.", TimeoutSec: 60}
}
func (ListMessages) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional(), field.Json("filters", MessageFilters{}).Label("Message filters").Optional()}
}
func (ListMessages) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("messages", []Message{}).Label("Messages"), field.String("next_page_uri").Label("Next page URI"), field.Bool("has_more").Label("Has more")}
}

type ListIncomingNumbers struct{}

func (ListIncomingNumbers) Meta() contract.Meta {
	return contract.Meta{ID: "list_incoming_numbers", Label: "List account phone numbers", TimeoutSec: 60}
}
func (ListIncomingNumbers) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListIncomingNumbers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("numbers", []IncomingNumber{}).Label("Phone numbers"), field.String("next_page_uri").Label("Next page URI"), field.Bool("has_more").Label("Has more")}
}

type GetIncomingNumber struct{}

func (GetIncomingNumber) Meta() contract.Meta {
	return contract.Meta{ID: "get_incoming_number", Label: "Get account phone number", TimeoutSec: 60}
}
func (GetIncomingNumber) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("number_sid").Label("Phone number SID")}
}
func (GetIncomingNumber) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("number", IncomingNumber{}).Label("Phone number")}
}

type ListMessageMedia struct{}

func (ListMessageMedia) Meta() contract.Meta {
	return contract.Meta{ID: "list_message_media", Label: "List message media metadata", Desc: "Return metadata only. Media binaries are not downloaded.", TimeoutSec: 60}
}
func (ListMessageMedia) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("message_sid").Label("Message SID"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListMessageMedia) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("media", []Media{}).Label("Media"), field.String("next_page_uri").Label("Next page URI"), field.Bool("has_more").Label("Has more")}
}
