package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type PageOptions struct {
	Limit *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	After *string `json:"after,omitempty" sokel:"after,optional" label:"After ID"`
}
type Tag struct {
	Name  string `json:"name" sokel:"name" label:"Tag name"`
	Value string `json:"value" sokel:"value" label:"Tag value"`
}
type SendOptions struct {
	From        string   `json:"from" sokel:"from" label:"Sender address"`
	To          []string `json:"to" sokel:"to" label:"Recipient addresses"`
	Subject     string   `json:"subject" sokel:"subject" label:"Subject"`
	HTML        *string  `json:"html,omitempty" sokel:"html,optional" label:"HTML body"`
	Text        *string  `json:"text,omitempty" sokel:"text,optional" label:"Plain text body"`
	CC          []string `json:"cc,omitempty" sokel:"cc,optional" label:"CC addresses"`
	BCC         []string `json:"bcc,omitempty" sokel:"bcc,optional" label:"BCC addresses"`
	ReplyTo     []string `json:"reply_to,omitempty" sokel:"reply_to,optional" label:"Reply-to addresses"`
	ScheduledAt *string  `json:"scheduled_at,omitempty" sokel:"scheduled_at,optional" label:"Scheduled time"`
	Tags        []Tag    `json:"tags,omitempty" sokel:"tags,optional" label:"Tags"`
}
type EmailSummary struct {
	ID          string   `json:"id" sokel:"id" label:"Email ID"`
	MessageID   *string  `json:"message_id,omitempty" sokel:"message_id,optional" label:"Message-ID header"`
	From        string   `json:"from" sokel:"from" label:"Sender address"`
	To          []string `json:"to" sokel:"to" label:"Recipient addresses"`
	Subject     string   `json:"subject" sokel:"subject" label:"Subject"`
	CreatedAt   string   `json:"created_at" sokel:"created_at" label:"Created at"`
	CC          []string `json:"cc,omitempty" sokel:"cc,optional" label:"CC addresses"`
	BCC         []string `json:"bcc,omitempty" sokel:"bcc,optional" label:"BCC addresses"`
	ReplyTo     []string `json:"reply_to,omitempty" sokel:"reply_to,optional" label:"Reply-to addresses"`
	LastEvent   *string  `json:"last_event,omitempty" sokel:"last_event,optional" label:"Last delivery event"`
	ScheduledAt *string  `json:"scheduled_at,omitempty" sokel:"scheduled_at,optional" label:"Scheduled time"`
}
type Attachment struct {
	ID                 string  `json:"id" sokel:"id" label:"Attachment ID"`
	Filename           *string `json:"filename,omitempty" sokel:"filename,optional" label:"Filename"`
	ContentType        string  `json:"content_type" sokel:"content_type" label:"Content type"`
	Size               int64   `json:"size" sokel:"size" label:"Size in bytes"`
	ContentID          *string `json:"content_id,omitempty" sokel:"content_id,optional" label:"Inline content ID"`
	ContentDisposition *string `json:"content_disposition,omitempty" sokel:"content_disposition,optional" label:"Content disposition"`
}
type RawEmail struct {
	DownloadURL string `json:"download_url" sokel:"download_url" label:"Raw email download URL"`
	ExpiresAt   string `json:"expires_at" sokel:"expires_at" label:"Download URL expiry"`
}
type Authentication struct {
	SPF   string `json:"spf" sokel:"spf" label:"SPF result"`
	DKIM  string `json:"dkim" sokel:"dkim" label:"DKIM result"`
	DMARC string `json:"dmarc" sokel:"dmarc" label:"DMARC result"`
}
type DomainCapabilities struct {
	Sending   string `json:"sending" sokel:"sending" label:"Sending capability"`
	Receiving string `json:"receiving" sokel:"receiving" label:"Receiving capability"`
}
type DNSRecord struct {
	Record   string `json:"record" sokel:"record" label:"Record purpose"`
	Name     string `json:"name" sokel:"name" label:"DNS name"`
	Type     string `json:"type" sokel:"type" label:"DNS record type"`
	Value    string `json:"value" sokel:"value" label:"DNS value"`
	TTL      string `json:"ttl" sokel:"ttl" label:"DNS TTL"`
	Status   string `json:"status" sokel:"status" label:"Verification status"`
	Priority *int   `json:"priority,omitempty" sokel:"priority,optional" label:"DNS priority"`
}
type Domain struct {
	ID            string              `json:"id" sokel:"id" label:"Domain ID"`
	Name          string              `json:"name" sokel:"name" label:"Domain name"`
	Status        string              `json:"status" sokel:"status" label:"Verification status"`
	Region        string              `json:"region" sokel:"region" label:"Region"`
	CreatedAt     string              `json:"created_at" sokel:"created_at" label:"Created at"`
	OpenTracking  *bool               `json:"open_tracking,omitempty" sokel:"open_tracking,optional" label:"Open tracking"`
	ClickTracking *bool               `json:"click_tracking,omitempty" sokel:"click_tracking,optional" label:"Click tracking"`
	Capabilities  *DomainCapabilities `json:"capabilities,omitempty" sokel:"capabilities,optional" label:"Domain capabilities"`
}
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Use a Resend API key. Sending-only keys cannot read emails or domains; those operations require full access.")}
}

type SendEmail struct{}

func (SendEmail) Meta() contract.Meta {
	return contract.Meta{ID: "send_email", Label: "Send email", Desc: "Send one email or schedule it. An accepted email is not proof of delivery.", TimeoutSec: 60}
}
func (SendEmail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("email", SendOptions{}).Label("Email"), field.String("idempotency_key").Label("Idempotency key").Desc("Reuse the same key and payload when retrying within 24 hours; up to 256 ASCII characters.").Optional()}
}
func (SendEmail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email_id").Label("Email ID")}
}

type GetEmail struct{}

func (GetEmail) Meta() contract.Meta {
	return contract.Meta{ID: "get_email", Label: "Get sent email", TimeoutSec: 60}
}
func (GetEmail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email_id").Label("Email ID")}
}
func (GetEmail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("email", SentEmail{}).Label("Email")}
}

type CancelEmail struct{}

func (CancelEmail) Meta() contract.Meta {
	return contract.Meta{ID: "cancel_email", Label: "Cancel scheduled email", Desc: "Only emails that have not yet been sent can be canceled.", TimeoutSec: 60}
}
func (CancelEmail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email_id").Label("Email ID")}
}
func (CancelEmail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email_id").Label("Email ID")}
}

type GetReceivedEmail struct{}

func (GetReceivedEmail) Meta() contract.Meta {
	return contract.Meta{ID: "get_received_email", Label: "Get received email", Desc: "Read email content and attachment metadata. Inline images use CID references; download URLs are not fetched.", TimeoutSec: 60}
}
func (GetReceivedEmail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("email_id").Label("Email ID")}
}
func (GetReceivedEmail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("email", ReceivedEmail{}).Label("Email")}
}

type GetDomain struct{}

func (GetDomain) Meta() contract.Meta {
	return contract.Meta{ID: "get_domain", Label: "Get domain", TimeoutSec: 60}
}
func (GetDomain) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("domain_id").Label("Domain ID")}
}
func (GetDomain) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("domain", Domain{}).Label("Domain"), field.Array("records", []DNSRecord{}).Label("DNS records")}
}

type ListEmails struct{}

func (ListEmails) Meta() contract.Meta {
	return contract.Meta{ID: "list_emails", Label: "List sent emails", TimeoutSec: 60}
}
func (ListEmails) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListEmails) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("emails", []EmailSummary{}).Label("Emails"), field.Bool("has_more").Label("Has more"), field.String("next_after").Label("Next after ID")}
}

type ListReceivedEmails struct{}

func (ListReceivedEmails) Meta() contract.Meta {
	return contract.Meta{ID: "list_received_emails", Label: "List received emails", TimeoutSec: 60}
}
func (ListReceivedEmails) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListReceivedEmails) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("emails", []EmailSummary{}).Label("Emails"), field.Bool("has_more").Label("Has more"), field.String("next_after").Label("Next after ID")}
}

type ListDomains struct{}

func (ListDomains) Meta() contract.Meta {
	return contract.Meta{ID: "list_domains", Label: "List domains", TimeoutSec: 60}
}
func (ListDomains) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListDomains) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("domains", []Domain{}).Label("Domains"), field.Bool("has_more").Label("Has more"), field.String("next_after").Label("Next after ID")}
}

type SentEmail struct {
	ID          string   `json:"id" sokel:"id" label:"Email ID"`
	MessageID   *string  `json:"message_id,omitempty" sokel:"message_id,optional" label:"Message-ID header"`
	From        string   `json:"from" sokel:"from" label:"Sender address"`
	To          []string `json:"to" sokel:"to" label:"Recipient addresses"`
	Subject     string   `json:"subject" sokel:"subject" label:"Subject"`
	CreatedAt   string   `json:"created_at" sokel:"created_at" label:"Created at"`
	CC          []string `json:"cc,omitempty" sokel:"cc,optional" label:"CC addresses"`
	BCC         []string `json:"bcc,omitempty" sokel:"bcc,optional" label:"BCC addresses"`
	ReplyTo     []string `json:"reply_to,omitempty" sokel:"reply_to,optional" label:"Reply-to addresses"`
	LastEvent   *string  `json:"last_event,omitempty" sokel:"last_event,optional" label:"Last delivery event"`
	ScheduledAt *string  `json:"scheduled_at,omitempty" sokel:"scheduled_at,optional" label:"Scheduled time"`
	HTML        *string  `json:"html,omitempty" sokel:"html,optional" label:"HTML body"`
	Text        *string  `json:"text,omitempty" sokel:"text,optional" label:"Plain text body"`
	Tags        []Tag    `json:"tags,omitempty" sokel:"tags,optional" label:"Tags"`
}

type ReceivedEmail struct {
	ID             string          `json:"id" sokel:"id" label:"Email ID"`
	MessageID      *string         `json:"message_id,omitempty" sokel:"message_id,optional" label:"Message-ID header"`
	From           string          `json:"from" sokel:"from" label:"Sender address"`
	To             []string        `json:"to" sokel:"to" label:"Recipient addresses"`
	Subject        string          `json:"subject" sokel:"subject" label:"Subject"`
	CreatedAt      string          `json:"created_at" sokel:"created_at" label:"Created at"`
	CC             []string        `json:"cc,omitempty" sokel:"cc,optional" label:"CC addresses"`
	BCC            []string        `json:"bcc,omitempty" sokel:"bcc,optional" label:"BCC addresses"`
	ReplyTo        []string        `json:"reply_to,omitempty" sokel:"reply_to,optional" label:"Reply-to addresses"`
	LastEvent      *string         `json:"last_event,omitempty" sokel:"last_event,optional" label:"Last delivery event"`
	ScheduledAt    *string         `json:"scheduled_at,omitempty" sokel:"scheduled_at,optional" label:"Scheduled time"`
	HTML           *string         `json:"html,omitempty" sokel:"html,optional" label:"HTML body"`
	Text           *string         `json:"text,omitempty" sokel:"text,optional" label:"Plain text body"`
	HTMLFormat     *string         `json:"html_format,omitempty" sokel:"html_format,optional" label:"Inline image format"`
	Attachments    []Attachment    `json:"attachments" sokel:"attachments" label:"Attachments"`
	Raw            *RawEmail       `json:"raw,omitempty" sokel:"raw,optional" label:"Raw email"`
	Authentication *Authentication `json:"authentication,omitempty" sokel:"authentication,optional" label:"Email authentication"`
	ReceivedFor    []string        `json:"received_for,omitempty" sokel:"received_for,optional" label:"Forwarded recipient addresses"`
}
