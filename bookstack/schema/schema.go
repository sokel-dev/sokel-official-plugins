package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Pagination struct {
	Offset *int `json:"offset,omitempty" sokel:"offset,optional" label:"Offset"`
	Count  *int `json:"count,omitempty" sokel:"count,optional" label:"Page size"`
}
type SearchPagination struct {
	Page  *int `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	Count *int `json:"count,omitempty" sokel:"count,optional" label:"Page size"`
}
type Entry struct {
	ID          string  `json:"id" sokel:"id" label:"Entity ID"`
	Kind        string  `json:"kind" sokel:"kind" label:"Entity type"`
	Name        string  `json:"name" sokel:"name" label:"Name"`
	Slug        string  `json:"slug" sokel:"slug" label:"Slug"`
	Description string  `json:"description" sokel:"description" label:"Description"`
	BookID      *string `json:"book_id,omitempty" sokel:"book_id,optional" label:"Book ID"`
	ChapterID   *string `json:"chapter_id,omitempty" sokel:"chapter_id,optional" label:"Chapter ID"`
	WebURL      *string `json:"web_url,omitempty" sokel:"web_url,optional" label:"Web URL"`
}
type Tag struct {
	Name  string `json:"name" sokel:"name" label:"Tag name"`
	Value string `json:"value" sokel:"value" label:"Tag value"`
}
type Page struct {
	ID            string  `json:"id" sokel:"id" label:"Page ID"`
	Name          string  `json:"name" sokel:"name" label:"Page name"`
	BookID        string  `json:"book_id" sokel:"book_id" label:"Book ID"`
	ChapterID     *string `json:"chapter_id,omitempty" sokel:"chapter_id,optional" label:"Chapter ID"`
	HTML          *string `json:"html,omitempty" sokel:"html,optional" label:"Rendered HTML"`
	RawHTML       *string `json:"raw_html,omitempty" sokel:"raw_html,optional" label:"Stored HTML"`
	Markdown      *string `json:"markdown,omitempty" sokel:"markdown,optional" label:"Markdown source"`
	RevisionCount string  `json:"revision_count" sokel:"revision_count" label:"Revision count"`
	Tags          []Tag   `json:"tags" sokel:"tags" label:"Tags"`
}
type CreateOptions struct {
	Priority  *int    `json:"priority,omitempty" sokel:"priority,optional" label:"Ordering priority"`
	Tags      *[]Tag  `json:"tags,omitempty" sokel:"tags,optional" label:"Replace tags"`
	Changelog *string `json:"changelog,omitempty" sokel:"changelog,optional" label:"Revision note"`
}
type PageChanges struct {
	Name      *string `json:"name,omitempty" sokel:"name,optional" label:"Page name"`
	HTML      *string `json:"html,omitempty" sokel:"html,optional" label:"HTML content"`
	Markdown  *string `json:"markdown,omitempty" sokel:"markdown,optional" label:"Markdown content"`
	Priority  *int    `json:"priority,omitempty" sokel:"priority,optional" label:"Ordering priority"`
	Tags      *[]Tag  `json:"tags,omitempty" sokel:"tags,optional" label:"Replace tags"`
	Changelog *string `json:"changelog,omitempty" sokel:"changelog,optional" label:"Revision note"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check token and service", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Healthy"), field.String("message").Label("Check details")}
}

type ListEntities struct{}

func (ListEntities) Meta() contract.Meta {
	return contract.Meta{ID: "list_entities", Label: "List shelves, books, chapters or pages", TimeoutSec: 60}
}
func (ListEntities) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("kind").Label("Entity type: shelves, books, chapters or pages"), field.Json("pagination", Pagination{}).Label("Pagination").Optional()}
}
func (ListEntities) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("entities", []Entry{}).Label("Entities"), field.String("total_count").Label("Total count"), field.Int("next_offset").Label("Next offset; zero means finished")}
}

type GetShelf struct{}

func (GetShelf) Meta() contract.Meta {
	return contract.Meta{ID: "get_shelf", Label: "Get shelf", TimeoutSec: 60}
}
func (GetShelf) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("shelf_id").Label("Shelf ID")}
}
func (GetShelf) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("shelf", Entry{}).Label("Shelf"), field.Array("books", []Entry{}).Label("Books")}
}

type GetBook struct{}

func (GetBook) Meta() contract.Meta {
	return contract.Meta{ID: "get_book", Label: "Get book", TimeoutSec: 60}
}
func (GetBook) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("book_id").Label("Book ID")}
}
func (GetBook) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("book", Entry{}).Label("Book"), field.Array("contents", []Entry{}).Label("Flattened contents")}
}

type GetChapter struct{}

func (GetChapter) Meta() contract.Meta {
	return contract.Meta{ID: "get_chapter", Label: "Get chapter", TimeoutSec: 60}
}
func (GetChapter) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("chapter_id").Label("Chapter ID")}
}
func (GetChapter) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("chapter", Entry{}).Label("Chapter"), field.Array("pages", []Entry{}).Label("Pages")}
}

type GetPage struct{}

func (GetPage) Meta() contract.Meta {
	return contract.Meta{ID: "get_page", Label: "Get page", TimeoutSec: 60}
}
func (GetPage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("page_id").Label("Page ID")}
}
func (GetPage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Page")}
}

type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "Search knowledge content", TimeoutSec: 60}
}
func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Search query"), field.Json("pagination", SearchPagination{}).Label("Pagination").Optional()}
}
func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("results", []Entry{}).Label("Search results"), field.String("total_count").Label("Total count"), field.Int("next_page").Label("Next page; zero means finished")}
}

type CreatePage struct{}

func (CreatePage) Meta() contract.Meta {
	return contract.Meta{ID: "create_page", Label: "Create and publish page", TimeoutSec: 60}
}
func (CreatePage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("parent_type").Label("Parent type: book or chapter"), field.String("parent_id").Label("Parent ID"), field.String("name").Label("Page name"), field.String("format").Label("Content format: html or markdown"), field.String("content").Label("Page content"), field.Json("options", CreateOptions{}).Label("Page options").Optional()}
}
func (CreatePage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Page")}
}

type UpdatePage struct{}

func (UpdatePage) Meta() contract.Meta {
	return contract.Meta{ID: "update_page", Label: "Update published page", TimeoutSec: 60}
}
func (UpdatePage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("page_id").Label("Page ID"), field.Json("changes", PageChanges{}).Label("Page changes")}
}
func (UpdatePage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Page")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("token_id").Label("Token ID"), field.Secret("token_secret").Label("Token secret")}
}
