package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type PageOptions struct {
	Page     *int `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	PageSize *int `json:"page_size,omitempty" sokel:"page_size,optional" label:"Page size"`
}
type Workspace struct {
	ID        string  `json:"id" sokel:"id" label:"Workspace ID"`
	Name      string  `json:"name" sokel:"name" label:"Workspace name"`
	AccountID *string `json:"account_id,omitempty" sokel:"account_id,optional" label:"Account ID"`
	Shared    *bool   `json:"shared,omitempty" sokel:"shared,optional" label:"Shared workspace"`
}
type FormSummary struct {
	ID         string  `json:"id" sokel:"id" label:"Form ID"`
	Title      string  `json:"title" sokel:"title" label:"Form title"`
	CreatedAt  *string `json:"created_at,omitempty" sokel:"created_at,optional" label:"Created at"`
	UpdatedAt  *string `json:"last_updated_at,omitempty" sokel:"updated_at,optional" label:"Updated at"`
	DisplayURL *string `json:"display_url,omitempty" sokel:"display_url,optional" label:"Public form URL"`
}
type Question struct {
	ID          string         `json:"id" sokel:"id" label:"Question ID"`
	Ref         *string        `json:"ref,omitempty" sokel:"ref,optional" label:"Question reference"`
	Type        string         `json:"type" sokel:"type" label:"Question type"`
	Title       string         `json:"title" sokel:"title" label:"Question title"`
	Properties  map[string]any `json:"properties,omitempty" sokel:"properties,optional" label:"Question properties" opaque:"Question-specific properties include choices and recursively nested group fields."`
	Validations map[string]any `json:"validations,omitempty" sokel:"validations,optional" label:"Question validations" opaque:"Validation keys depend on each Typeform question type."`
}
type Form struct {
	ID        string     `json:"id" sokel:"id" label:"Form ID"`
	Title     string     `json:"title" sokel:"title" label:"Form title"`
	Questions []Question `json:"fields" sokel:"questions" label:"Questions"`
	Hidden    []string   `json:"hidden,omitempty" sokel:"hidden,optional" label:"Hidden field names"`
}
type Answer struct {
	Type      string  `json:"type" sokel:"type" label:"Answer type"`
	FieldID   string  `json:"field_id" sokel:"field_id" label:"Question ID"`
	FieldRef  *string `json:"field_ref,omitempty" sokel:"field_ref,optional" label:"Question reference"`
	FieldType string  `json:"field_type" sokel:"field_type" label:"Question type"`
	Value     any     `json:"value" sokel:"value" label:"Answer value" opaque:"Answer values are a type-dependent union of scalars, choices, payments and other Typeform answer payloads."`
}
type Response struct {
	Token       string            `json:"token" sokel:"token" label:"Response cursor token"`
	ResponseID  *string           `json:"response_id,omitempty" sokel:"response_id,optional" label:"Response ID"`
	LandedAt    *string           `json:"landed_at,omitempty" sokel:"landed_at,optional" label:"Landed at"`
	SubmittedAt *string           `json:"submitted_at,omitempty" sokel:"submitted_at,optional" label:"Submitted at"`
	Answers     []Answer          `json:"answers" sokel:"answers" label:"Answers"`
	Hidden      map[string]string `json:"hidden,omitempty" sokel:"hidden,optional" label:"Hidden field values"`
	Calculated  map[string]any    `json:"calculated,omitempty" sokel:"calculated,optional" label:"Calculated values" opaque:"Forms define their own calculated score and price fields."`
}
type ResponseOptions struct {
	PageSize *int    `json:"page_size,omitempty" sokel:"page_size,optional" label:"Page size"`
	Before   *string `json:"before,omitempty" sokel:"before,optional" label:"Before response cursor"`
	Since    *string `json:"since,omitempty" sokel:"since,optional" label:"Submitted since"`
	Until    *string `json:"until,omitempty" sokel:"until,optional" label:"Submitted until"`
	Query    *string `json:"query,omitempty" sokel:"query,optional" label:"Answer search phrase"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListWorkspaces struct{}

func (ListWorkspaces) Meta() contract.Meta {
	return contract.Meta{ID: "list_workspaces", Label: "List workspaces", TimeoutSec: 60}
}
func (ListWorkspaces) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", PageOptions{}).Label("Pagination").Optional(), field.String("search").Label("Workspace search").Optional()}
}
func (ListWorkspaces) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("workspaces", []Workspace{}).Label("Workspaces"), field.Int("total_items").Label("Total items"), field.Int("page_count").Label("Page count"), field.Int("next_page").Label("Next page; zero means complete")}
}

type GetWorkspace struct{}

func (GetWorkspace) Meta() contract.Meta {
	return contract.Meta{ID: "get_workspace", Label: "Get workspace", TimeoutSec: 60}
}
func (GetWorkspace) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("workspace_id").Label("Workspace ID")}
}
func (GetWorkspace) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("workspace", Workspace{}).Label("Workspace")}
}

type ListForms struct{}

func (ListForms) Meta() contract.Meta {
	return contract.Meta{ID: "list_forms", Label: "List forms", TimeoutSec: 60}
}
func (ListForms) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("workspace_id").Label("Workspace ID").Optional(), field.String("search").Label("Form search").Optional(), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListForms) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("forms", []FormSummary{}).Label("Forms"), field.Int("total_items").Label("Total items"), field.Int("page_count").Label("Page count"), field.Int("next_page").Label("Next page; zero means complete")}
}

type GetForm struct{}

func (GetForm) Meta() contract.Meta {
	return contract.Meta{ID: "get_form", Label: "Get form", TimeoutSec: 60}
}
func (GetForm) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("form_id").Label("Form ID")}
}
func (GetForm) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("form", Form{}).Label("Form")}
}

type ListResponses struct{}

func (ListResponses) Meta() contract.Meta {
	return contract.Meta{ID: "list_responses", Label: "List completed responses", TimeoutSec: 60}
}
func (ListResponses) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("form_id").Label("Form ID"), field.Json("options", ResponseOptions{}).Label("Response filters").Optional()}
}
func (ListResponses) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("responses", []Response{}).Label("Responses"), field.Int("total_items").Label("Total matching responses"), field.Int("page_count").Label("Page count"), field.String("next_before").Label("Next before cursor"), field.Bool("may_have_more").Label("A further page may exist")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Use a personal access token with accounts:read, forms:read, responses:read and workspaces:read as needed.")}
}
