package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Space struct {
	ID     string `json:"id" sokel:"id" label:"Space ID"`
	Key    string `json:"key" sokel:"key" label:"Space key"`
	Name   string `json:"name" sokel:"name" label:"Name"`
	Type   string `json:"type" sokel:"type" label:"Space type"`
	Status string `json:"status" sokel:"status" label:"Status"`
}
type Page struct {
	ID             string `json:"id" sokel:"id" label:"Page ID"`
	SpaceID        string `json:"space_id" sokel:"space_id" label:"Space ID"`
	ParentID       string `json:"parent_id" sokel:"parent_id" label:"Parent page ID"`
	Title          string `json:"title" sokel:"title" label:"Title"`
	Status         string `json:"status" sokel:"status" label:"Status"`
	Version        int    `json:"version" sokel:"version" label:"Current version"`
	Body           string `json:"body" sokel:"body" label:"Page body"`
	Representation string `json:"representation" sokel:"representation" label:"Body representation"`
	CreatedAt      string `json:"created_at" sokel:"created_at" label:"Created at"`
	WebPath        string `json:"web_path" sokel:"web_path" label:"Web UI path"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type ListSpaces struct{}

func (ListSpaces) Meta() contract.Meta {
	return contract.Meta{ID: "list_spaces", Label: "List spaces", TimeoutSec: 60}
}
func (ListSpaces) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50"), field.String("cursor").Label("Pagination cursor").Optional(), field.String("key").Label("Space key").Optional()}
}
func (ListSpaces) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("spaces", []Space{}).Label("Spaces"), field.String("next_cursor").Label("Next cursor")}
}

type GetSpace struct{}

func (GetSpace) Meta() contract.Meta {
	return contract.Meta{ID: "get_space", Label: "Get space", TimeoutSec: 60}
}
func (GetSpace) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("space_id").Label("Space ID")}
}
func (GetSpace) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("space", Space{}).Label("Space")}
}

type ListPages struct{}

func (ListPages) Meta() contract.Meta {
	return contract.Meta{ID: "list_pages", Label: "List pages", TimeoutSec: 60}
}
func (ListPages) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional().Desc("1 to 100; defaults to 50"), field.String("cursor").Label("Pagination cursor").Optional(), field.String("space_id").Label("Space ID").Optional(), field.String("title").Label("Exact page title").Optional(), field.String("representation").Label("Body representation").Optional().Desc("storage or atlas_doc_format; defaults to storage")}
}
func (ListPages) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("pages", []Page{}).Label("Pages"), field.String("next_cursor").Label("Next cursor")}
}

type GetPage struct{}

func (GetPage) Meta() contract.Meta {
	return contract.Meta{ID: "get_page", Label: "Get page", TimeoutSec: 60}
}
func (GetPage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("page_id").Label("Page ID"), field.String("representation").Label("Body representation").Optional().Desc("storage or atlas_doc_format; defaults to storage")}
}
func (GetPage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Page")}
}

type CreatePage struct{}

func (CreatePage) Meta() contract.Meta {
	return contract.Meta{ID: "create_page", Label: "Create page", TimeoutSec: 60}
}
func (CreatePage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("space_id").Label("Space ID"), field.String("title").Label("Title"), field.String("body").Label("Page body"), field.String("representation").Label("Body representation").Optional().Desc("storage or atlas_doc_format; defaults to storage"), field.String("parent_id").Label("Parent page ID").Optional()}
}
func (CreatePage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Page")}
}

type UpdatePage struct{}

func (UpdatePage) Meta() contract.Meta {
	return contract.Meta{ID: "update_page", Label: "Update page with version check", TimeoutSec: 60}
}
func (UpdatePage) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("page_id").Label("Page ID"), field.Int("expected_version").Label("Expected current version"), field.String("title").Label("Title"), field.String("body").Label("Page body"), field.String("representation").Label("Body representation").Optional().Desc("storage or atlas_doc_format; defaults to storage"), field.String("version_message").Label("Version message").Optional()}
}
func (UpdatePage) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Page")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("site").Label("Atlassian Cloud site").Optional().Desc("For tokens without scopes: https://your-site.atlassian.net"), field.Text("cloud_id").Label("Cloud ID for scoped tokens").Optional().Desc("Scoped tokens use the fixed api.atlassian.com gateway; takes precedence over site"), field.Text("email").Label("Atlassian account email"), field.Secret("api_token").Label("Atlassian API token")}
}
