package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("access_token").Label("Access token").Desc("Optional ntfy token for protected topics. The service address is configured with SOKEL_NTFY_URL.").Optional(),
	}
}

type Attachment struct {
	Name    string  `json:"name" sokel:"name" label:"Filename"`
	URL     string  `json:"url" sokel:"url" label:"Attachment URL"`
	Type    *string `json:"type,omitempty" sokel:"type,optional" label:"Content type"`
	Size    *int64  `json:"size,omitempty" sokel:"size,optional" label:"Size in bytes"`
	Expires *int64  `json:"expires,omitempty" sokel:"expires,optional" label:"Expires at"`
}
type Message struct {
	ID         string      `json:"id" sokel:"id" label:"Message ID"`
	Time       int64       `json:"time" sokel:"time" label:"Unix timestamp"`
	Event      string      `json:"event" sokel:"event" label:"Event type"`
	Topic      string      `json:"topic" sokel:"topic" label:"Topic"`
	Message    *string     `json:"message,omitempty" sokel:"message,optional" label:"Message body"`
	Title      *string     `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Priority   *int        `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	Tags       []string    `json:"tags,omitempty" sokel:"tags,optional" label:"Tags"`
	Click      *string     `json:"click,omitempty" sokel:"click,optional" label:"Click URL"`
	Expires    *int64      `json:"expires,omitempty" sokel:"expires,optional" label:"Expires at"`
	Attachment *Attachment `json:"attachment,omitempty" sokel:"attachment,optional" label:"Attachment metadata"`
}
type PublishOptions struct {
	Title    *string  `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Priority *int     `json:"priority,omitempty" sokel:"priority,optional" label:"Priority"`
	Tags     []string `json:"tags,omitempty" sokel:"tags,optional" label:"Tags"`
	Click    *string  `json:"click,omitempty" sokel:"click,optional" label:"Click URL"`
	Cache    *bool    `json:"cache,omitempty" sokel:"cache,optional" label:"Cache message"`
}
type Health struct{}

func (Health) Meta() contract.Meta {
	return contract.Meta{ID: "health", Label: "Check service health", TimeoutSec: 30}
}
func (Health) Inputs() []contract.FieldSpec { return nil }
func (Health) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("healthy").Label("Healthy")}
}

type Publish struct{}

func (Publish) Meta() contract.Meta {
	return contract.Meta{ID: "publish", Label: "Publish notification", Desc: "Publish to your ntfy server with Firebase disabled. Acceptance does not prove device delivery.", TimeoutSec: 60}
}
func (Publish) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("topic").Label("Topic"), field.Text("message").Label("Message body"), field.Json("options", PublishOptions{}).Label("Notification options").Optional()}
}
func (Publish) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("message", Message{}).Label("Published message")}
}

type Poll struct{}

func (Poll) Meta() contract.Meta {
	return contract.Meta{ID: "poll", Label: "Poll cached notifications", Desc: "Read one topic with a deduplicating cursor. Persist next_cursor between runs; cache expiry is not durable delivery.", TimeoutSec: 60}
}
func (Poll) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("topic").Label("Topic"), field.String("since").Label("Initial time window").Desc("Positive Unix timestamp, duration such as 10m, or all. Defaults to all; omit when using cursor.").Optional(), field.String("cursor").Label("Continuation cursor").Desc("Reuse next_cursor for the same topic. Do not edit it or combine it with since.").Optional(), field.Int("limit").Label("Maximum messages").Desc("Return 1–1000 messages; default 100. Continue with next_cursor when has_more is true.").Optional()}
}
func (Poll) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("messages", []Message{}).Label("Messages"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("Has more")}
}
