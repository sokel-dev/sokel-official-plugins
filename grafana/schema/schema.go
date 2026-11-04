package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type DataSource struct {
	UID       string `json:"uid" sokel:"uid" label:"Data source UID"`
	Name      string `json:"name" sokel:"name" label:"Data source name"`
	Type      string `json:"type" sokel:"type" label:"Data source type"`
	IsDefault bool   `json:"is_default" sokel:"is_default" label:"Default data source"`
	ReadOnly  bool   `json:"read_only" sokel:"read_only" label:"Read only"`
}
type SearchOptions struct {
	Page       *int     `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	Limit      *int     `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Tags       []string `json:"tags" sokel:"tags" label:"Tags"`
	FolderUIDs []string `json:"folder_uids" sokel:"folder_uids" label:"Folder UIDs"`
	Starred    *bool    `json:"starred,omitempty" sokel:"starred,optional" label:"Starred only"`
}
type DashboardHit struct {
	UID       string   `json:"uid" sokel:"uid" label:"Dashboard UID"`
	Title     string   `json:"title" sokel:"title" label:"Title"`
	URL       string   `json:"url" sokel:"url" label:"Dashboard URL"`
	Tags      []string `json:"tags" sokel:"tags" label:"Tags"`
	FolderUID *string  `json:"folder_uid,omitempty" sokel:"folder_uid,optional" label:"Folder UID"`
	IsStarred bool     `json:"is_starred" sokel:"is_starred" label:"Starred"`
}
type AnnotationFilter struct {
	From         *string  `json:"from,omitempty" sokel:"from,optional" label:"Start time in epoch milliseconds"`
	To           *string  `json:"to,omitempty" sokel:"to,optional" label:"End time in epoch milliseconds"`
	Limit        *int     `json:"limit,omitempty" sokel:"limit,optional" label:"Result limit"`
	DashboardUID *string  `json:"dashboard_uid,omitempty" sokel:"dashboard_uid,optional" label:"Dashboard UID"`
	PanelID      *string  `json:"panel_id,omitempty" sokel:"panel_id,optional" label:"Panel ID"`
	Tags         []string `json:"tags" sokel:"tags" label:"Tags"`
}
type AnnotationOptions struct {
	TimeEnd      *string  `json:"time_end,omitempty" sokel:"time_end,optional" label:"Region end in epoch milliseconds"`
	DashboardUID *string  `json:"dashboard_uid,omitempty" sokel:"dashboard_uid,optional" label:"Dashboard UID"`
	PanelID      *string  `json:"panel_id,omitempty" sokel:"panel_id,optional" label:"Panel ID"`
	Tags         []string `json:"tags" sokel:"tags" label:"Tags"`
}
type Annotation struct {
	ID           string   `json:"id" sokel:"id" label:"Annotation ID"`
	DashboardUID *string  `json:"dashboard_uid,omitempty" sokel:"dashboard_uid,optional" label:"Dashboard UID"`
	PanelID      string   `json:"panel_id" sokel:"panel_id" label:"Panel ID"`
	Time         string   `json:"time" sokel:"time" label:"Time in epoch milliseconds"`
	TimeEnd      *string  `json:"time_end,omitempty" sokel:"time_end,optional" label:"Region end in epoch milliseconds"`
	Text         string   `json:"text" sokel:"text" label:"Annotation text"`
	Tags         []string `json:"tags" sokel:"tags" label:"Tags"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check token and service", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Healthy"), field.String("message").Label("Check details")}
}

type ListDataSources struct{}

func (ListDataSources) Meta() contract.Meta {
	return contract.Meta{ID: "list_data_sources", Label: "List data source summaries", TimeoutSec: 60}
}
func (ListDataSources) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListDataSources) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("data_sources", []DataSource{}).Label("Data sources")}
}

type GetDataSource struct{}

func (GetDataSource) Meta() contract.Meta {
	return contract.Meta{ID: "get_data_source", Label: "Get data source summary", TimeoutSec: 60}
}
func (GetDataSource) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("uid").Label("Data source UID")}
}
func (GetDataSource) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("data_source", DataSource{}).Label("Data source")}
}

type SearchDashboards struct{}

func (SearchDashboards) Meta() contract.Meta {
	return contract.Meta{ID: "search_dashboards", Label: "Search dashboards", TimeoutSec: 60}
}
func (SearchDashboards) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Search query").Optional(), field.Json("options", SearchOptions{}).Label("Search options").Optional()}
}
func (SearchDashboards) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("dashboards", []DashboardHit{}).Label("Dashboards"), field.Int("next_page").Label("Next page; zero means finished")}
}

type GetDashboard struct{}

func (GetDashboard) Meta() contract.Meta {
	return contract.Meta{ID: "get_dashboard", Label: "Get dashboard model", TimeoutSec: 60}
}
func (GetDashboard) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("uid").Label("Dashboard UID")}
}
func (GetDashboard) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("uid").Label("Dashboard UID"), field.String("title").Label("Title"), field.String("version").Label("Dashboard version"), field.String("model_json").Label("Dashboard model JSON").Desc("Panel and plugin-defined dashboard models have variable schemas. JSON text preserves exact numbers and the original model structure.")}
}

type ListAnnotations struct{}

func (ListAnnotations) Meta() contract.Meta {
	return contract.Meta{ID: "list_annotations", Label: "Find annotations", TimeoutSec: 60}
}
func (ListAnnotations) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("filter", AnnotationFilter{}).Label("Annotation filters").Optional()}
}
func (ListAnnotations) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("annotations", []Annotation{}).Label("Annotations"), field.Bool("limit_reached").Label("Result limit reached")}
}

type CreateAnnotation struct{}

func (CreateAnnotation) Meta() contract.Meta {
	return contract.Meta{ID: "create_annotation", Label: "Create annotation", TimeoutSec: 60}
}
func (CreateAnnotation) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("text").Label("Annotation text"), field.String("time").Label("Time in epoch milliseconds"), field.Json("options", AnnotationOptions{}).Label("Annotation options").Optional()}
}
func (CreateAnnotation) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("Annotation ID")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("token").Label("Service account token")}
}
