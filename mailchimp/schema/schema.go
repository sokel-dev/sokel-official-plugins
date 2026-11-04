package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type PageOptions struct {
	Limit  *int64 `json:"limit" sokel:"limit,optional" label:"Page size"`
	Offset *int64 `json:"offset" sokel:"offset,optional" label:"Offset"`
}
type Audience struct {
	ID          string `json:"id" sokel:"id" label:"Audience ID"`
	Name        string `json:"name" sokel:"name" label:"Audience name"`
	DateCreated string `json:"date_created" sokel:"date_created" label:"Created at"`
	DoubleOptin bool   `json:"double_optin" sokel:"double_optin" label:"Double opt-in"`
}
type ListMembersOptions struct {
	VipOnly *bool `json:"vip_only" sokel:"vip_only,optional" label:"Only VIP members"`
}
type Member struct {
	ID           string         `json:"id" sokel:"id" label:"Member hash"`
	EmailAddress string         `json:"email_address" sokel:"email_address" label:"Email address"`
	Status       string         `json:"status" sokel:"status" label:"Subscription status"`
	LastChanged  string         `json:"last_changed" sokel:"last_changed" label:"Last changed"`
	Vip          bool           `json:"vip" sokel:"vip" label:"VIP"`
	MergeFields  map[string]any `json:"merge_fields" sokel:"merge_fields" label:"Merge fields" opaque:"Audience-defined merge tags and values"`
}
type UpsertMemberOptions struct {
	Vip *bool `json:"vip" sokel:"vip,optional" label:"VIP"`
}
type MemberTag struct {
	ID        string `json:"id" sokel:"id" label:"Tag ID"`
	Name      string `json:"name" sokel:"name" label:"Tag name"`
	DateAdded string `json:"date_added" sokel:"date_added" label:"Added at"`
}
type Campaign struct {
	ID          string `json:"id" sokel:"id" label:"Campaign ID"`
	Type        string `json:"type" sokel:"type" label:"Campaign type"`
	Status      string `json:"status" sokel:"status" label:"Campaign status"`
	Title       string `json:"title" sokel:"title" label:"Title"`
	SubjectLine string `json:"subject_line" sokel:"subject_line" label:"Subject"`
	ListID      string `json:"list_id" sokel:"list_id" label:"Audience ID"`
	CreateTime  string `json:"create_time" sokel:"create_time" label:"Created at"`
	SendTime    string `json:"send_time" sokel:"send_time" label:"Sent at"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", Desc: "Check credentials", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type ListAudiences struct{}

func (ListAudiences) Meta() contract.Meta {
	return contract.Meta{ID: "list_audiences", Label: "List audiences", Desc: "List audiences", TimeoutSec: 60}
}
func (ListAudiences) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListAudiences) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("audiences", []Audience{}).Label("Audiences"), field.Int("total").Label("Total count"), field.Bool("has_more").Label("More results"), field.Int("next_offset").Label("Next offset")}
}

type GetAudience struct{}

func (GetAudience) Meta() contract.Meta {
	return contract.Meta{ID: "get_audience", Label: "Get audience", Desc: "Get audience", TimeoutSec: 60}
}
func (GetAudience) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("Audience ID")}
}
func (GetAudience) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("audience", Audience{}).Label("Audience")}
}

type ListMembers struct{}

func (ListMembers) Meta() contract.Meta {
	return contract.Meta{ID: "list_members", Label: "List audience members", Desc: "List audience members", TimeoutSec: 60}
}
func (ListMembers) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("Audience ID"), field.Enum("status", contract.Option{Value: "subscribed"}, contract.Option{Value: "unsubscribed"}, contract.Option{Value: "cleaned"}, contract.Option{Value: "pending"}, contract.Option{Value: "transactional"}, contract.Option{Value: "archived"}).Label("Subscription status").Optional(), field.String("since_last_changed").Label("Changed since").Desc("RFC 3339 timestamp including a timezone").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional(), field.Json("options", ListMembersOptions{}).Label("Optional settings").Optional()}
}
func (ListMembers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("members", []Member{}).Label("Members"), field.Int("total").Label("Total count"), field.Bool("has_more").Label("More results"), field.Int("next_offset").Label("Next offset")}
}

type GetMember struct{}

func (GetMember) Meta() contract.Meta {
	return contract.Meta{ID: "get_member", Label: "Get audience member", Desc: "Get audience member", TimeoutSec: 60}
}
func (GetMember) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("Audience ID"), field.String("email_address").Label("Email address")}
}
func (GetMember) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("member", Member{}).Label("Member")}
}

type UpsertMember struct{}

func (UpsertMember) Meta() contract.Meta {
	return contract.Meta{ID: "upsert_member", Label: "Add or update audience member", Desc: "Add or update audience member", TimeoutSec: 60}
}
func (UpsertMember) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("Audience ID"), field.String("email_address").Label("Email address"), field.Enum("status_if_new", contract.Option{Value: "subscribed"}, contract.Option{Value: "unsubscribed"}, contract.Option{Value: "cleaned"}, contract.Option{Value: "pending"}, contract.Option{Value: "transactional"}).Label("Status for a new member").Desc("Required explicit choice. Existing subscription status is never changed; pending can send an opt-in email"), field.Object("merge_fields", "Audience-defined merge tags and values; omitted fields are not changed").Label("Merge fields").Optional(), field.Json("options", UpsertMemberOptions{}).Label("Optional settings").Optional()}
}
func (UpsertMember) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("member", Member{}).Label("Member")}
}

type ListMemberTags struct{}

func (ListMemberTags) Meta() contract.Meta {
	return contract.Meta{ID: "list_member_tags", Label: "List member tags", Desc: "List member tags", TimeoutSec: 60}
}
func (ListMemberTags) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("Audience ID"), field.String("email_address").Label("Email address"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListMemberTags) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tags", []MemberTag{}).Label("Tags"), field.Int("total").Label("Total count"), field.Bool("has_more").Label("More results"), field.Int("next_offset").Label("Next offset")}
}

type ListCampaigns struct{}

func (ListCampaigns) Meta() contract.Meta {
	return contract.Meta{ID: "list_campaigns", Label: "List campaigns", Desc: "List campaigns", TimeoutSec: 60}
}
func (ListCampaigns) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("list_id").Label("Audience ID").Optional(), field.Enum("status", contract.Option{Value: "save"}, contract.Option{Value: "paused"}, contract.Option{Value: "schedule"}, contract.Option{Value: "sending"}, contract.Option{Value: "sent"}).Label("Campaign status").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListCampaigns) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("campaigns", []Campaign{}).Label("Campaigns"), field.Int("total").Label("Total count"), field.Bool("has_more").Label("More results"), field.Int("next_offset").Label("Next offset")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Marketing API key including its data-center suffix, for example -us6")}
}
