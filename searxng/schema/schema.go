package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("username").Label("Proxy username").Desc("Optional HTTP Basic authentication at your reverse proxy; SearXNG has no API key requirement.").Optional(),
		field.Secret("password").Label("Proxy password").Optional(),
	}
}

type Result struct {
	Title    *string  `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	URL      *string  `json:"url,omitempty" sokel:"url,optional" label:"Result URL"`
	Content  *string  `json:"content,omitempty" sokel:"content,optional" label:"Snippet"`
	Engine   *string  `json:"engine,omitempty" sokel:"engine,optional" label:"Engine"`
	Engines  []string `json:"engines,omitempty" sokel:"engines,optional" label:"Engines"`
	Category *string  `json:"category,omitempty" sokel:"category,optional" label:"Category"`
	Score    *float64 `json:"score,omitempty" sokel:"score,optional" label:"Score"`
}
type Engine struct {
	Name       string   `json:"name" sokel:"name" label:"Engine"`
	Shortcut   string   `json:"shortcut" sokel:"shortcut" label:"Shortcut"`
	Categories []string `json:"categories" sokel:"categories" label:"Categories"`
	Enabled    bool     `json:"enabled" sokel:"enabled" label:"Enabled"`
	Paging     bool     `json:"paging" sokel:"paging" label:"Supports pagination"`
	SafeSearch bool     `json:"safesearch" sokel:"safesearch" label:"Supports safe search"`
	TimeRange  bool     `json:"time_range_support" sokel:"time_range_support" label:"Supports time range"`
}
type EngineError struct {
	Engine string `json:"engine" sokel:"engine" label:"Engine"`
	Reason string `json:"reason" sokel:"reason" label:"Failure reason"`
}
type SearchOptions struct {
	Page       *int     `json:"page,omitempty" sokel:"page,optional" label:"Page number" desc:"Starts at 1; engines may not support further pages."`
	Limit      *int     `json:"limit,omitempty" sokel:"limit,optional" label:"Maximum results" desc:"Maximum results from this page, 1–100; default 20. Clipping does not fetch subsequent pages."`
	Engines    []string `json:"engines,omitempty" sokel:"engines,optional" label:"Engines"`
	Categories []string `json:"categories,omitempty" sokel:"categories,optional" label:"Categories"`
	Language   *string  `json:"language,omitempty" sokel:"language,optional" label:"Language"`
	SafeSearch *int     `json:"safe_search,omitempty" sokel:"safe_search,optional" label:"Safe search" desc:"0 off, 1 moderate, 2 strict; omitted uses the instance setting."`
	TimeRange  *string  `json:"time_range,omitempty" sokel:"time_range,optional" label:"Time range" desc:"day, month, or year; omitted uses no time filter."`
}
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "Search the web", Desc: "Search through your SearXNG instance. JSON output must be enabled; external engines may return partial results.", TimeoutSec: 90}
}
func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("query").Label("Query"), field.Json("options", SearchOptions{}).Label("Search options").Optional(),
	}
}
func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("query").Label("Query"), field.Array("results", []Result{}).Label("Results"), field.Strings("suggestions").Label("Suggestions"), field.Strings("corrections").Label("Corrections"), field.Array("engine_errors", []EngineError{}).Label("Engine failures"), field.Bool("partial").Label("Partial results"), field.Bool("clipped").Label("Page results clipped"), field.Int("page").Label("Page number"),
	}
}

type GetConfig struct{}

func (GetConfig) Meta() contract.Meta {
	return contract.Meta{ID: "get_config", Label: "List search engines", Desc: "Read engine names and supported search features from your instance.", TimeoutSec: 30}
}
func (GetConfig) Inputs() []contract.FieldSpec { return nil }
func (GetConfig) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("instance_name").Label("Instance name"), field.String("version").Label("Service version"), field.Strings("categories").Label("Categories"), field.Array("engines", []Engine{}).Label("Engines")}
}

type Health struct{}

func (Health) Meta() contract.Meta {
	return contract.Meta{ID: "health", Label: "Check service health", Desc: "Check the process only; this does not verify engine access or JSON search availability.", TimeoutSec: 30}
}
func (Health) Inputs() []contract.FieldSpec { return nil }
func (Health) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("healthy").Label("Healthy")}
}
