// Package schema declares the contract. Named option structs preserve omitted numeric and boolean values.

package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Scrape struct{}

func (Scrape) Meta() contract.Meta {
	return contract.Meta{ID: "scrape", Label: "Scrape page", TimeoutSec: 180}
}

func (Scrape) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("Page URL"),
		field.Strings("formats").Label("Response formats").Desc("Supports markdown, html and links; defaults to markdown.").Optional(),
		field.Json("options", ScrapeOptions{}).Label("Optional parameters").Optional()}
}

func (Scrape) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("document", Document{}).Label("Document")}
}

type Crawl struct{}

func (Crawl) Meta() contract.Meta {
	return contract.Meta{ID: "crawl", Label: "Start crawl", TimeoutSec: 60}
}

func (Crawl) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("Page URL"),
		field.Strings("include_paths").Label("Include paths").Optional(),
		field.Strings("exclude_paths").Label("Exclude paths").Optional(),
		field.Json("options", CrawlOptions{}).Label("Optional parameters").Optional()}
}

func (Crawl) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("Job ID"),
		field.String("url").Label("Status URL").Optional()}
}

type CrawlStatus struct{}

func (CrawlStatus) Meta() contract.Meta {
	return contract.Meta{ID: "crawl_status", Label: "Get crawl status", TimeoutSec: 60}
}

func (CrawlStatus) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("Job ID"),
		field.Json("options", CrawlStatusOptions{}).Label("Optional parameters").Optional()}
}

func (CrawlStatus) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("status").Label("Job status"),
		field.Int("total").Label("Total pages"),
		field.Int("completed").Label("Completed pages"),
		field.Int("credits_used").Label("Credits used").Optional(),
		field.String("expires_at").Label("Expires at").Optional(),
		field.String("next").Label("Next page URL").Optional(),
		field.Array("documents", []Document{}).Label("Documents").Optional()}
}

type Map struct{}

func (Map) Meta() contract.Meta {
	return contract.Meta{ID: "map", Label: "Map website", TimeoutSec: 120}
}

func (Map) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("Page URL"),
		field.String("search").Label("Link search").Optional(),
		field.Json("options", MapOptions{}).Label("Optional parameters").Optional()}
}

func (Map) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("links", []MapLinks{}).Label("Discovered links")}
}

type Extract struct{}

func (Extract) Meta() contract.Meta {
	return contract.Meta{ID: "extract", Label: "Start extraction", TimeoutSec: 180}
}

func (Extract) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Strings("urls").Label("Page URLs"),
		field.Text("prompt").Label("Extraction prompt").Optional(),
		field.Object("schema", "Caller-defined JSON Schema; its fields depend on the extraction task.").Label("JSON Schema").Optional()}
}

func (Extract) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("Job ID")}
}

type ExtractStatus struct{}

func (ExtractStatus) Meta() contract.Meta {
	return contract.Meta{ID: "extract_status", Label: "Get extraction status", TimeoutSec: 60}
}

func (ExtractStatus) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("Job ID")}
}

func (ExtractStatus) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("status").Label("Job status"),
		field.Object("data", "The result shape is defined by the JSON Schema submitted with the task.").Label("Extracted data").Optional(),
		field.String("expires_at").Label("Expires at").Optional()}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 60}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"),
		field.String("message").Label("Check details")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Create a key in the official service dashboard; do not enter deployment URLs.")}
}

type ScrapeOptions struct {
	OnlyMainContent *bool `json:"only_main_content" sokel:"only_main_content,optional" label:"Only main content"`
	WaitFor         *int  `json:"wait_for" sokel:"wait_for,optional" label:"Wait milliseconds"`
}

type Document struct {
	Markdown string       `json:"markdown" sokel:"markdown,optional" label:"Markdown content"`
	HTML     string       `json:"html" sokel:"html,optional" label:"HTML content"`
	Links    []string     `json:"links" sokel:"links,optional" label:"Links"`
	Metadata PageMetadata `json:"metadata" sokel:"metadata,optional" label:"Page metadata"`
}

type PageMetadata struct {
	Title       string `json:"title" sokel:"title,optional" label:"Title"`
	Description string `json:"description" sokel:"description,optional" label:"Description"`
	SourceURL   string `json:"source_url" sokel:"source_url,optional" label:"Source URL"`
	Language    string `json:"language" sokel:"language,optional" label:"Language"`
	StatusCode  int    `json:"status_code" sokel:"status_code,optional" label:"Page status code"`
}

type CrawlOptions struct {
	Limit             *int `json:"limit" sokel:"limit,optional" label:"Maximum pages"`
	MaxDiscoveryDepth *int `json:"max_discovery_depth" sokel:"max_discovery_depth,optional" label:"Discovery depth"`
}

type CrawlStatusOptions struct {
	Skip *int `json:"skip" sokel:"skip,optional" label:"Skip pages"`
}

type MapOptions struct {
	Limit             *int  `json:"limit" sokel:"limit,optional" label:"Maximum links"`
	IncludeSubdomains *bool `json:"include_subdomains" sokel:"include_subdomains,optional" label:"Include subdomains"`
}

type MapLinks struct {
	URL         string `json:"url" sokel:"url" label:"Page URL"`
	Title       string `json:"title" sokel:"title,optional" label:"Title"`
	Description string `json:"description" sokel:"description,optional" label:"Description"`
}
