package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type SearchOptions struct {
	Count   *int  `json:"count,omitempty" sokel:"count,optional" label:"Result count"`
	Summary *bool `json:"summary,omitempty" sokel:"summary,optional" label:"Include summary"`
}
type Result struct {
	ID          string `json:"id" sokel:"id" label:"Result ID"`
	Name        string `json:"name" sokel:"name" label:"Title"`
	URL         string `json:"url" sokel:"url" label:"URL"`
	Snippet     string `json:"snippet" sokel:"snippet" label:"Snippet"`
	Summary     string `json:"summary" sokel:"summary" label:"Summary"`
	SiteName    string `json:"siteName" sokel:"site_name" label:"Site name"`
	PublishedAt string `json:"datePublished" sokel:"published_at" label:"Published at"`
}
type WebSearch struct{}

func (WebSearch) Meta() contract.Meta {
	return contract.Meta{ID: "web_search", Label: "Search the web", TimeoutSec: 60}
}
func (WebSearch) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Search query"), field.String("freshness").Label("Freshness").Optional().Desc("noLimit, oneDay, oneWeek, oneMonth, oneYear, YYYY-MM-DD or a date range joined by .."), field.Json("options", SearchOptions{}).Label("Search options").Optional()}
}
func (WebSearch) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("results", []Result{}).Label("Search results"), field.String("query").Label("Original query"), field.Int("total_estimated_matches").Label("Estimated matches"), field.String("log_id").Label("Request log ID")}
}

type VerifyCredential struct{}

func (VerifyCredential) Meta() contract.Meta {
	return contract.Meta{ID: "verify_credential", Label: "Verify credential with a billable search", TimeoutSec: 60}
}
func (VerifyCredential) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("probe_query").Label("Billable probe query").Desc("Performs one paid search; no automatic health check is registered")}
}
func (VerifyCredential) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Bocha API key")}
}
