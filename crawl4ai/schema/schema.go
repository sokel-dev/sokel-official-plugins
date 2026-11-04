package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type CrawlOptions struct {
	PageTimeoutMS      *int    `json:"page_timeout_ms,omitempty" sokel:"page_timeout_ms,optional" label:"Page timeout in milliseconds"`
	WordCountThreshold *int    `json:"word_count_threshold,omitempty" sokel:"word_count_threshold,optional" label:"Minimum word count"`
	OnlyText           *bool   `json:"only_text,omitempty" sokel:"only_text,optional" label:"Extract text only"`
	CSSSelector        *string `json:"css_selector,omitempty" sokel:"css_selector,optional" label:"Content CSS selector"`
}
type CrawlResult struct {
	URL        string  `json:"url" sokel:"url" label:"Page URL"`
	Success    bool    `json:"success" sokel:"success" label:"Page crawl succeeded"`
	StatusCode *int    `json:"status_code,omitempty" sokel:"status_code,optional" label:"Page HTTP status"`
	Title      *string `json:"title,omitempty" sokel:"title,optional" label:"Page title"`
	Markdown   *string `json:"markdown,omitempty" sokel:"markdown,optional" label:"Raw Markdown"`
	Message    string  `json:"message" sokel:"message" label:"Result details"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check service availability", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Service reachable"), field.String("message").Label("Check details")}
}

type Crawl struct{}

func (Crawl) Meta() contract.Meta {
	return contract.Meta{ID: "crawl", Label: "Crawl pages", TimeoutSec: 180}
}
func (Crawl) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("urls", []string{}).Label("HTTP or HTTPS page URLs"), field.Json("options", CrawlOptions{}).Label("Crawl options").Optional()}
}
func (Crawl) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("results", []CrawlResult{}).Label("Page results"), field.Bool("all_succeeded").Label("Every page succeeded")}
}

type SubmitCrawl struct{}

func (SubmitCrawl) Meta() contract.Meta {
	return contract.Meta{ID: "submit_crawl", Label: "Submit crawl task", TimeoutSec: 60}
}
func (SubmitCrawl) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("urls", []string{}).Label("HTTP or HTTPS page URLs"), field.Json("options", CrawlOptions{}).Label("Crawl options").Optional()}
}
func (SubmitCrawl) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"), field.Bool("accepted").Label("Task accepted; completion is not confirmed")}
}

type GetTask struct{}

func (GetTask) Meta() contract.Meta {
	return contract.Meta{ID: "get_task", Label: "Get crawl task", TimeoutSec: 60}
}
func (GetTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID")}
}
func (GetTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"), field.String("status").Label("Task status"), field.Bool("completed").Label("Task completed"), field.Bool("all_succeeded").Label("Every page succeeded"), field.Array("results", []CrawlResult{}).Label("Page results"), field.String("message").Label("Task details")}
}

type Markdown struct{}

func (Markdown) Meta() contract.Meta {
	return contract.Meta{ID: "markdown", Label: "Extract raw Markdown", TimeoutSec: 180}
}
func (Markdown) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("HTTP or HTTPS page URL"), field.Bool("use_cache").Label("Allow cached content").Optional()}
}
func (Markdown) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("Page URL"), field.String("markdown").Label("Raw Markdown")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Self-hosted server API token or an existing JWT; no cloud account is required.")}
}
