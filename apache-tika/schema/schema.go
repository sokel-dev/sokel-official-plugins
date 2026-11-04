package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type MetadataField struct {
	Name   string   `json:"name" sokel:"name" label:"Metadata name"`
	Values []string `json:"values" sokel:"values" label:"Metadata values"`
}
type Document struct {
	Text     string          `json:"text" sokel:"text" label:"Extracted text"`
	Metadata []MetadataField `json:"metadata" sokel:"metadata" label:"Metadata"`
}

type ExtractText struct{}

func (ExtractText) Meta() contract.Meta {
	return contract.Meta{ID: "extract_text", Label: "Extract document text", TimeoutSec: 300}
}
func (ExtractText) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform document")}
}
func (ExtractText) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("text").Label("Extracted text"), field.Array("metadata", []MetadataField{}).Label("Metadata")}
}

type ExtractMetadata struct{}

func (ExtractMetadata) Meta() contract.Meta {
	return contract.Meta{ID: "extract_metadata", Label: "Extract document metadata", TimeoutSec: 300}
}
func (ExtractMetadata) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform document")}
}
func (ExtractMetadata) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("metadata", []MetadataField{}).Label("Metadata")}
}

type ExtractRecursive struct{}

func (ExtractRecursive) Meta() contract.Meta {
	return contract.Meta{ID: "extract_recursive", Label: "Extract text and metadata from embedded documents", TimeoutSec: 300}
}
func (ExtractRecursive) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform document")}
}
func (ExtractRecursive) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("documents", []Document{}).Label("Documents")}
}

type DetectType struct{}

func (DetectType) Meta() contract.Meta {
	return contract.Meta{ID: "detect_type", Label: "Detect document media type", TimeoutSec: 300}
}
func (DetectType) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform document")}
}
func (DetectType) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("media_type").Label("Detected media type")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("username").Label("Basic auth username").Desc("Optional HTTP Basic authentication for the configured service or its reverse proxy.").Optional(), field.Secret("password").Label("Basic auth password").Optional()}
}
