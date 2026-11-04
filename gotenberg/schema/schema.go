package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type HtmlOptions struct {
	Landscape         *bool `json:"landscape,omitempty" sokel:"landscape,optional" label:"Landscape"`
	PrintBackground   *bool `json:"print_background,omitempty" sokel:"print_background,optional" label:"Print background"`
	PreferCSSPageSize *bool `json:"prefer_css_page_size,omitempty" sokel:"prefer_css_page_size,optional" label:"Prefer CSS page size"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check service health", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Healthy"), field.String("message").Label("Check details")}
}

type HtmlToPDF struct{}

func (HtmlToPDF) Meta() contract.Meta {
	return contract.Meta{ID: "html_to_pdf", Label: "Convert HTML to PDF", TimeoutSec: 300}
}
func (HtmlToPDF) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("html").Label("HTML document"), field.Files("assets").Label("Platform asset files").Optional(), field.Json("options", HtmlOptions{}).Label("Rendering options").Optional(), field.String("output_name").Label("Output PDF filename").Optional()}
}
func (HtmlToPDF) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("PDF file")}
}

type OfficeToPDF struct{}

func (OfficeToPDF) Meta() contract.Meta {
	return contract.Meta{ID: "office_to_pdf", Label: "Convert office document to PDF", TimeoutSec: 300}
}
func (OfficeToPDF) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Office document"), field.String("output_name").Label("Output PDF filename").Optional()}
}
func (OfficeToPDF) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("PDF file")}
}

type MergePDFs struct{}

func (MergePDFs) Meta() contract.Meta {
	return contract.Meta{ID: "merge_pdfs", Label: "Merge PDFs in input order", TimeoutSec: 300}
}
func (MergePDFs) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Files("files").Label("PDF files in order"), field.String("output_name").Label("Output PDF filename").Optional()}
}
func (MergePDFs) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("PDF file")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("username").Label("Basic auth username").Desc("Optional HTTP Basic authentication for the configured service or its reverse proxy.").Optional(), field.Secret("password").Label("Basic auth password").Optional()}
}
