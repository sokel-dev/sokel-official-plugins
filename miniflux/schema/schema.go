package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Create a per-application key in your own Miniflux settings. The service address is configured with SOKEL_MINIFLUX_URL.")}
}

type Category struct {
	ID    string `json:"id" sokel:"id" label:"Category ID"`
	Title string `json:"title" sokel:"title" label:"Category title"`
}
type Feed struct {
	ID                string   `json:"id" sokel:"id" label:"Feed ID"`
	Title             string   `json:"title" sokel:"title" label:"Feed title"`
	FeedURL           string   `json:"feed_url" sokel:"feed_url" label:"Feed URL"`
	SiteURL           string   `json:"site_url" sokel:"site_url" label:"Website URL"`
	Category          Category `json:"category" sokel:"category" label:"Category"`
	CheckedAt         *string  `json:"checked_at,omitempty" sokel:"checked_at,optional" label:"Last checked at"`
	Disabled          bool     `json:"disabled" sokel:"disabled" label:"Disabled"`
	ParsingErrorCount int64    `json:"parsing_error_count" sokel:"parsing_error_count" label:"Parsing error count"`
}
type Entry struct {
	ID          string `json:"id" sokel:"id" label:"Entry ID"`
	FeedID      string `json:"feed_id" sokel:"feed_id" label:"Feed ID"`
	Title       string `json:"title" sokel:"title" label:"Title"`
	URL         string `json:"url" sokel:"url" label:"Article URL"`
	Author      string `json:"author" sokel:"author" label:"Author"`
	Content     string `json:"content" sokel:"content" label:"Stored HTML content"`
	Status      string `json:"status" sokel:"status" label:"Reading status"`
	Starred     bool   `json:"starred" sokel:"starred" label:"Starred"`
	PublishedAt string `json:"published_at" sokel:"published_at" label:"Published at"`
}
type EntryOptions struct {
	Limit      *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	AfterID    *string `json:"after_id,omitempty" sokel:"after_id,optional" label:"After entry ID"`
	Status     *string `json:"status,omitempty" sokel:"status,optional" label:"Reading status"`
	Starred    *bool   `json:"starred,omitempty" sokel:"starred,optional" label:"Starred"`
	Search     *string `json:"search,omitempty" sokel:"search,optional" label:"Search text"`
	CategoryID *string `json:"category_id,omitempty" sokel:"category_id,optional" label:"Category ID"`
}
type EntryChanges struct {
	Status  *string `json:"status,omitempty" sokel:"status,optional" label:"Reading status"`
	Starred *bool   `json:"starred,omitempty" sokel:"starred,optional" label:"Starred"`
}

type GetCurrentUser struct{}

func (GetCurrentUser) Meta() contract.Meta {
	return contract.Meta{ID: "get_current_user", Label: "Get current user", TimeoutSec: 60}
}
func (GetCurrentUser) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (GetCurrentUser) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("user_id").Label("User ID"), field.String("username").Label("Username")}
}

type ListCategories struct{}

func (ListCategories) Meta() contract.Meta {
	return contract.Meta{ID: "list_categories", Label: "List categories", TimeoutSec: 60}
}
func (ListCategories) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListCategories) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("categories", []Category{}).Label("Categories")}
}

type CreateCategory struct{}

func (CreateCategory) Meta() contract.Meta {
	return contract.Meta{ID: "create_category", Label: "Create category", TimeoutSec: 60}
}
func (CreateCategory) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("title").Label("Category title")}
}
func (CreateCategory) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("category", Category{}).Label("Category")}
}

type ListFeeds struct{}

func (ListFeeds) Meta() contract.Meta {
	return contract.Meta{ID: "list_feeds", Label: "List feeds", TimeoutSec: 60}
}
func (ListFeeds) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListFeeds) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("feeds", []Feed{}).Label("Feeds")}
}

type CreateFeed struct{}

func (CreateFeed) Meta() contract.Meta {
	return contract.Meta{ID: "create_feed", Label: "Subscribe to feed", TimeoutSec: 60}
}
func (CreateFeed) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("feed_url").Label("Feed URL"), field.String("category_id").Label("Category ID")}
}
func (CreateFeed) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("feed_id").Label("Feed ID")}
}

type RefreshFeed struct{}

func (RefreshFeed) Meta() contract.Meta {
	return contract.Meta{ID: "refresh_feed", Label: "Request feed refresh", TimeoutSec: 60}
}
func (RefreshFeed) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("feed_id").Label("Feed ID")}
}
func (RefreshFeed) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("accepted").Label("Refresh accepted")}
}

type ListEntries struct{}

func (ListEntries) Meta() contract.Meta {
	return contract.Meta{ID: "list_entries", Label: "List stored entries", TimeoutSec: 60}
}
func (ListEntries) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("options", EntryOptions{}).Label("Entry filters").Optional()}
}
func (ListEntries) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("entries", []Entry{}).Label("Entries"), field.Bool("may_have_more").Label("May have more"), field.String("next_after_id").Label("Next after entry ID")}
}

type GetEntry struct{}

func (GetEntry) Meta() contract.Meta {
	return contract.Meta{ID: "get_entry", Label: "Get stored entry", TimeoutSec: 60}
}
func (GetEntry) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("entry_id").Label("Entry ID")}
}
func (GetEntry) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("entry", Entry{}).Label("Entry")}
}

type UpdateEntries struct{}

func (UpdateEntries) Meta() contract.Meta {
	return contract.Meta{ID: "update_entries", Label: "Update reading state", TimeoutSec: 60}
}
func (UpdateEntries) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Strings("entry_ids").Label("Entry IDs"), field.Json("changes", EntryChanges{}).Label("Reading state changes")}
}
func (UpdateEntries) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("accepted").Label("Update accepted")}
}
