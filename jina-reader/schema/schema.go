// Package schema declares the contract. Named option structs preserve omitted numeric and boolean values.

package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Read struct{}

func (Read) Meta() contract.Meta {
	return contract.Meta{ID: "read", Label: "Read page", TimeoutSec: 180}
}

func (Read) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("Page URL"),
		field.Enum("format", field.Opt("markdown", "markdown"), field.Opt("text", "text"), field.Opt("html", "html")).Label("Content format").Optional(),
		field.String("target_selector").Label("Target selector").Optional(),
		field.String("remove_selector").Label("Remove selector").Optional(),
		field.Json("options", ReadOptions{}).Label("Optional parameters").Optional()}
}

func (Read) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("Source URL"),
		field.String("final_url").Label("Final URL").Optional(),
		field.String("title").Label("Title").Optional(),
		field.String("description").Label("Description").Optional(),
		field.Text("content").Label("Content"),
		field.String("published_time").Label("Published time").Optional(),
		field.Object("metadata", "Custom page metadata keys and values are defined by the target website.").Label("Page metadata").Optional(),
		field.String("warning").Label("Upstream warning").Optional()}
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
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Create a key in the official service dashboard; do not enter deployment URLs.").Optional()}
}

type ReadOptions struct {
	NoCache *bool `json:"no_cache" sokel:"no_cache,optional" label:"Bypass cache"`
}
