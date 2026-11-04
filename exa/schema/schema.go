// Package schema declares the Exa contract. Pointer options preserve omission.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Result struct {
	ID            string   `json:"id" sokel:"id" label:"Document ID"`
	URL           string   `json:"url" sokel:"url" label:"Page URL"`
	Title         *string  `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	PublishedDate *string  `json:"publishedDate,omitempty" sokel:"published_date,optional" label:"Published date"`
	Author        *string  `json:"author,omitempty" sokel:"author,optional" label:"Author"`
	Text          *string  `json:"text,omitempty" sokel:"text,optional" label:"Page text"`
	Highlights    []string `json:"highlights,omitempty" sokel:"highlights,optional" label:"Highlights"`
	Summary       *string  `json:"summary,omitempty" sokel:"summary,optional" label:"Summary"`
}
type Cost struct {
	Total *float64 `json:"total,omitempty" sokel:"total,optional" label:"Estimated USD cost"`
}
type ContentOptions struct {
	Text        *bool `json:"text,omitempty" sokel:"text,optional" label:"Include page text"`
	Highlights  *bool `json:"highlights,omitempty" sokel:"highlights,optional" label:"Include highlights"`
	MaxAgeHours *int  `json:"maxAgeHours,omitempty" sokel:"max_age_hours,optional" label:"Maximum cache age in hours"`
}
type SearchOptions struct {
	NumResults *int `json:"numResults,omitempty" sokel:"num_results,optional" label:"Number of results"`
}
type ContentStatus struct {
	ID     string        `json:"id" sokel:"id" label:"Document ID"`
	Status string        `json:"status" sokel:"status" label:"Fetch status"`
	Source *string       `json:"source,omitempty" sokel:"source,optional" label:"Content source"`
	Error  *ContentError `json:"error,omitempty" sokel:"error,optional" label:"Fetch error"`
}
type ContentError struct {
	Tag            *string `json:"tag,omitempty" sokel:"tag,optional" label:"Error tag"`
	HTTPStatusCode *int    `json:"httpStatusCode,omitempty" sokel:"http_status_code,optional" label:"HTTP status code"`
}
type AnswerOptions struct {
	Text *bool `json:"text,omitempty" sokel:"text,optional" label:"Include citation text"`
}
type Budget struct {
	MaxCostDollars     *float64 `json:"maxCostDollars,omitempty" sokel:"max_cost_dollars,optional" label:"Maximum cost in USD"`
	MaxDurationSeconds *int     `json:"maxDurationSeconds,omitempty" sokel:"max_duration_seconds,optional" label:"Maximum duration in seconds"`
}
type Agent struct {
	ID          string  `json:"id" sokel:"id" label:"Run ID"`
	Status      string  `json:"status" sokel:"status" label:"Run status"`
	StopReason  *string `json:"stopReason,omitempty" sokel:"stop_reason,optional" label:"Stop reason"`
	CreatedAt   string  `json:"createdAt" sokel:"created_at" label:"Created at"`
	CompletedAt *string `json:"completedAt,omitempty" sokel:"completed_at,optional" label:"Completed at"`
}

type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "Search the web", TimeoutSec: 180}
}
func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("query").Label("Search query"), field.String("search_type").Label("Search mode").Optional().Desc("auto, lite, deep-lite or deep; omitted uses auto."), field.Strings("include_domains").Label("Include domains").Optional(), field.Strings("exclude_domains").Label("Exclude domains").Optional(), field.String("start_published_date").Label("Published after").Optional(), field.String("end_published_date").Label("Published before").Optional(), field.Json("options", SearchOptions{}).Label("Search options").Optional(), field.Json("contents", ContentOptions{}).Label("Content options").Optional()}
}
func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("request_id").Label("Request ID"), field.Array("results", []Result{}).Label("Search results"), field.Json("cost", Cost{}).Label("Estimated cost").Optional()}
}

type GetContents struct{}

func (GetContents) Meta() contract.Meta {
	return contract.Meta{ID: "get_contents", Label: "Get page contents", TimeoutSec: 180}
}
func (GetContents) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Strings("urls").Label("Page URLs").Optional(), field.Strings("ids").Label("Document IDs").Optional(), field.Json("options", ContentOptions{}).Label("Content options").Optional().Desc("Supply exactly one of urls or ids; highlights are requested when no content format is specified.")}
}
func (GetContents) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("request_id").Label("Request ID"), field.Array("results", []Result{}).Label("Page contents"), field.Array("statuses", []ContentStatus{}).Label("Per-document status"), field.Json("cost", Cost{}).Label("Estimated cost").Optional()}
}

type Answer struct{}

func (Answer) Meta() contract.Meta {
	return contract.Meta{ID: "answer", Label: "Answer with citations", TimeoutSec: 180}
}
func (Answer) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("query").Label("Question"), field.String("model").Label("Answer model").Optional(), field.Text("system_prompt").Label("Answer instructions").Optional(), field.Object("output_schema", "The caller defines the object fields returned by Exa.").Label("Output JSON Schema").Optional(), field.Json("options", AnswerOptions{}).Label("Answer options").Optional()}
}
func (Answer) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("request_id").Label("Request ID"), field.Text("answer").Label("Answer text").Optional(), field.Object("answer_data", "Object fields follow the submitted output JSON Schema.").Label("Structured answer").Optional(), field.Array("citations", []Result{}).Label("Citations"), field.Json("cost", Cost{}).Label("Estimated cost").Optional()}
}

type AgentStart struct{}

func (AgentStart) Meta() contract.Meta {
	return contract.Meta{ID: "agent_start", Label: "Start research agent", TimeoutSec: 60}
}
func (AgentStart) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("query").Label("Research instructions"), field.String("effort").Label("Research effort").Optional(), field.Text("system_prompt").Label("Agent instructions").Optional(), field.Object("output_schema", "The caller defines the object fields returned by the research agent.").Label("Output JSON Schema").Optional(), field.Json("budget", Budget{}).Label("Research budget").Optional().Desc("USD cap: 1-100 for auto/ultra; duration: 300-10800 seconds for ultra only.")}
}
func (AgentStart) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("run", Agent{}).Label("Agent run")}
}

type AgentStatus struct{}

func (AgentStatus) Meta() contract.Meta {
	return contract.Meta{ID: "agent_status", Label: "Get research status", TimeoutSec: 60}
}
func (AgentStatus) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("run_id").Label("Run ID")}
}
func (AgentStatus) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("run", Agent{}).Label("Agent run"), field.Text("text").Label("Research text").Optional(), field.Object("structured", "Object fields follow the schema of the submitted agent run.").Label("Structured research").Optional(), field.String("error_code").Label("Failure code").Optional(), field.Json("cost", Cost{}).Label("Estimated cost").Optional()}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return nil }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Create an API key in the Exa dashboard. Deployment URLs belong in environment configuration.")}
}
