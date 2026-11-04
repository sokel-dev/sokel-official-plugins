package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_token").Label("API token").Desc("Copy the token from your own linkding settings. Configure the service address with SOKEL_LINKDING_URL.")}
}

type Bookmark struct {
	ID           string   `json:"id" sokel:"id" label:"Bookmark ID"`
	URL          string   `json:"url" sokel:"url" label:"URL"`
	Title        string   `json:"title" sokel:"title" label:"Title"`
	Description  string   `json:"description" sokel:"description" label:"Description"`
	Notes        string   `json:"notes" sokel:"notes" label:"Notes"`
	IsArchived   bool     `json:"is_archived" sokel:"is_archived" label:"Archived"`
	Unread       bool     `json:"unread" sokel:"unread" label:"Unread"`
	Shared       bool     `json:"shared" sokel:"shared" label:"Shared"`
	TagNames     []string `json:"tag_names" sokel:"tag_names" label:"Tag names"`
	DateAdded    string   `json:"date_added" sokel:"date_added" label:"Added at"`
	DateModified string   `json:"date_modified" sokel:"date_modified" label:"Modified at"`
}
type BookmarkFields struct {
	Title       *string   `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Description *string   `json:"description,omitempty" sokel:"description,optional" label:"Description"`
	Notes       *string   `json:"notes,omitempty" sokel:"notes,optional" label:"Notes"`
	IsArchived  *bool     `json:"is_archived,omitempty" sokel:"is_archived,optional" label:"Archived"`
	Unread      *bool     `json:"unread,omitempty" sokel:"unread,optional" label:"Unread"`
	Shared      *bool     `json:"shared,omitempty" sokel:"shared,optional" label:"Shared"`
	TagNames    *[]string `json:"tag_names,omitempty" sokel:"tag_names,optional" label:"Tag names"`
}
type BookmarkChanges struct {
	URL         *string   `json:"url,omitempty" sokel:"url,optional" label:"URL"`
	Title       *string   `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Description *string   `json:"description,omitempty" sokel:"description,optional" label:"Description"`
	Notes       *string   `json:"notes,omitempty" sokel:"notes,optional" label:"Notes"`
	IsArchived  *bool     `json:"is_archived,omitempty" sokel:"is_archived,optional" label:"Archived"`
	Unread      *bool     `json:"unread,omitempty" sokel:"unread,optional" label:"Unread"`
	Shared      *bool     `json:"shared,omitempty" sokel:"shared,optional" label:"Shared"`
	TagNames    *[]string `json:"tag_names,omitempty" sokel:"tag_names,optional" label:"Tag names"`
}
type PageOptions struct {
	Limit  *int `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Offset *int `json:"offset,omitempty" sokel:"offset,optional" label:"Offset"`
}
type BookmarkOptions struct {
	Limit         *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Offset        *int    `json:"offset,omitempty" sokel:"offset,optional" label:"Offset"`
	Archived      *bool   `json:"archived,omitempty" sokel:"archived,optional" label:"Archived list"`
	Search        *string `json:"search,omitempty" sokel:"search,optional" label:"Search text"`
	ModifiedSince *string `json:"modified_since,omitempty" sokel:"modified_since,optional" label:"Modified since"`
	AddedSince    *string `json:"added_since,omitempty" sokel:"added_since,optional" label:"Added since"`
}
type PageInfo struct {
	Total      string `json:"total" sokel:"total" label:"Total count"`
	NextOffset *int   `json:"next_offset,omitempty" sokel:"next_offset,optional" label:"Next offset"`
	HasMore    bool   `json:"has_more" sokel:"has_more" label:"Has more"`
}
type Tag struct {
	ID        string `json:"id" sokel:"id" label:"Tag ID"`
	Name      string `json:"name" sokel:"name" label:"Tag name"`
	DateAdded string `json:"date_added" sokel:"date_added" label:"Added at"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check connection", Desc: "", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Connected"), field.String("message").Label("Message")}
}

type ListBookmarks struct{}

func (ListBookmarks) Meta() contract.Meta {
	return contract.Meta{ID: "list_bookmarks", Label: "List bookmarks", Desc: "", TimeoutSec: 60}
}
func (ListBookmarks) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("options", BookmarkOptions{}).Label("List options").Optional()}
}
func (ListBookmarks) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("bookmarks", []Bookmark{}).Label("Bookmarks"), field.Json("page", PageInfo{}).Label("Page")}
}

type GetBookmark struct{}

func (GetBookmark) Meta() contract.Meta {
	return contract.Meta{ID: "get_bookmark", Label: "Get bookmark", Desc: "", TimeoutSec: 60}
}
func (GetBookmark) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("bookmark_id").Label("Bookmark ID")}
}
func (GetBookmark) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("bookmark", Bookmark{}).Label("Bookmark")}
}

type SaveBookmark struct{}

func (SaveBookmark) Meta() contract.Meta {
	return contract.Meta{ID: "save_bookmark", Label: "Save bookmark", Desc: "Saving an existing URL may replace bookmark fields. The request disables synchronous metadata scraping and automatic HTML snapshots; server background integrations have separate settings.", TimeoutSec: 60}
}
func (SaveBookmark) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("URL"), field.Json("fields", BookmarkFields{}).Label("Bookmark fields").Optional()}
}
func (SaveBookmark) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("bookmark", Bookmark{}).Label("Bookmark")}
}

type UpdateBookmark struct{}

func (UpdateBookmark) Meta() contract.Meta {
	return contract.Meta{ID: "update_bookmark", Label: "Update bookmark", Desc: "", TimeoutSec: 60}
}
func (UpdateBookmark) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("bookmark_id").Label("Bookmark ID"), field.Json("changes", BookmarkChanges{}).Label("Bookmark changes")}
}
func (UpdateBookmark) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("bookmark", Bookmark{}).Label("Bookmark")}
}

type ArchiveBookmark struct{}

func (ArchiveBookmark) Meta() contract.Meta {
	return contract.Meta{ID: "archive_bookmark", Label: "Archive bookmark", Desc: "", TimeoutSec: 60}
}
func (ArchiveBookmark) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("bookmark_id").Label("Bookmark ID")}
}
func (ArchiveBookmark) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("accepted").Label("Archive accepted")}
}

type UnarchiveBookmark struct{}

func (UnarchiveBookmark) Meta() contract.Meta {
	return contract.Meta{ID: "unarchive_bookmark", Label: "Unarchive bookmark", Desc: "", TimeoutSec: 60}
}
func (UnarchiveBookmark) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("bookmark_id").Label("Bookmark ID")}
}
func (UnarchiveBookmark) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("accepted").Label("Unarchive accepted")}
}

type DeleteBookmark struct{}

func (DeleteBookmark) Meta() contract.Meta {
	return contract.Meta{ID: "delete_bookmark", Label: "Delete bookmark", Desc: "", TimeoutSec: 60}
}
func (DeleteBookmark) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("bookmark_id").Label("Bookmark ID")}
}
func (DeleteBookmark) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("Deleted")}
}

type ListTags struct{}

func (ListTags) Meta() contract.Meta {
	return contract.Meta{ID: "list_tags", Label: "List tags", Desc: "", TimeoutSec: 60}
}
func (ListTags) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("options", PageOptions{}).Label("Page options").Optional()}
}
func (ListTags) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tags", []Tag{}).Label("Tags"), field.Json("page", PageInfo{}).Label("Page")}
}
