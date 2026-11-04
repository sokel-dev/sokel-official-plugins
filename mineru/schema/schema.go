// Package schema declares the contract. Named option structs preserve omitted numeric and boolean values.

package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Submit struct{}

func (Submit) Meta() contract.Meta {
	return contract.Meta{ID: "submit", Label: "Submit document", TimeoutSec: 60}
}

func (Submit) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("url").Label("File URL"),
		field.String("data_id").Label("Correlation ID").Optional(),
		field.String("page_ranges").Label("Page ranges").Optional(),
		field.Enum("model_version", field.Opt("pipeline", "pipeline"), field.Opt("vlm", "vlm"), field.Opt("MinerU-HTML", "MinerU-HTML")).Label("Parsing model").Optional(),
		field.String("language").Label("Document language").Optional(),
		field.Strings("extra_formats").Label("Extra formats").Optional(),
		field.Json("options", SubmitOptions{}).Label("Optional parameters").Optional()}
}

func (Submit) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID"),
		field.String("trace_id").Label("Request trace ID").Optional()}
}

type Status struct{}

func (Status) Meta() contract.Meta {
	return contract.Meta{ID: "status", Label: "Get document status", TimeoutSec: 60}
}

func (Status) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Task ID")}
}

func (Status) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("result", TaskResult{}).Label("Extraction task"),
		field.String("trace_id").Label("Request trace ID").Optional()}
}

type BatchSubmit struct{}

func (BatchSubmit) Meta() contract.Meta {
	return contract.Meta{ID: "batch_submit", Label: "Submit document batch", TimeoutSec: 60}
}

func (BatchSubmit) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("files", []BatchSubmitFiles{}).Label("Documents"),
		field.Enum("model_version", field.Opt("pipeline", "pipeline"), field.Opt("vlm", "vlm"), field.Opt("MinerU-HTML", "MinerU-HTML")).Label("Parsing model").Optional(),
		field.String("language").Label("Document language").Optional(),
		field.Strings("extra_formats").Label("Extra formats").Optional(),
		field.Json("options", BatchSubmitOptions{}).Label("Optional parameters").Optional()}
}

func (BatchSubmit) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("batch_id").Label("Batch ID"),
		field.String("trace_id").Label("Request trace ID").Optional()}
}

type BatchStatus struct{}

func (BatchStatus) Meta() contract.Meta {
	return contract.Meta{ID: "batch_status", Label: "Get batch status", TimeoutSec: 60}
}

func (BatchStatus) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("batch_id").Label("Batch ID")}
}

func (BatchStatus) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("batch_id").Label("Batch ID"),
		field.Array("results", []TaskResult{}).Label("Extraction tasks"),
		field.String("trace_id").Label("Request trace ID").Optional()}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 60}
}

func (HealthCheck) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Existing task ID").Optional()}
}

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"),
		field.String("message").Label("Check details")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Desc("Create a key in the official service dashboard; do not enter deployment URLs.")}
}

type SubmitOptions struct {
	IsOCR         *bool `json:"is_ocr" sokel:"is_ocr,optional" label:"Enable OCR"`
	EnableFormula *bool `json:"enable_formula" sokel:"enable_formula,optional" label:"Recognize formulas"`
	EnableTable   *bool `json:"enable_table" sokel:"enable_table,optional" label:"Recognize tables"`
}

type TaskResult struct {
	TaskID          string                    `json:"task_id" sokel:"task_id,optional" label:"Task ID"`
	DataID          string                    `json:"data_id" sokel:"data_id,optional" label:"Correlation ID"`
	FileName        string                    `json:"file_name" sokel:"file_name,optional" label:"File name"`
	State           string                    `json:"state" sokel:"state" label:"Task state"`
	FullZIPURL      string                    `json:"full_zip_url" sokel:"full_zip_url,optional" label:"Result ZIP URL"`
	ErrMsg          string                    `json:"err_msg" sokel:"err_msg,optional" label:"Parsing error"`
	ExtractProgress TaskResultExtractProgress `json:"extract_progress" sokel:"extract_progress,optional" label:"Parsing progress"`
}

type TaskResultExtractProgress struct {
	ExtractedPages int    `json:"extracted_pages" sokel:"extracted_pages,optional" label:"Extracted pages"`
	TotalPages     int    `json:"total_pages" sokel:"total_pages,optional" label:"Total pages"`
	StartTime      string `json:"start_time" sokel:"start_time,optional" label:"Start time"`
}

type BatchSubmitFiles struct {
	URL        string `json:"url" sokel:"url" label:"File URL"`
	DataID     string `json:"data_id" sokel:"data_id,optional" label:"Correlation ID"`
	IsOCR      *bool  `json:"is_ocr" sokel:"is_ocr,optional" label:"Enable OCR"`
	PageRanges string `json:"page_ranges" sokel:"page_ranges,optional" label:"Page ranges"`
}

type BatchSubmitOptions struct {
	EnableFormula *bool `json:"enable_formula" sokel:"enable_formula,optional" label:"Recognize formulas"`
	EnableTable   *bool `json:"enable_table" sokel:"enable_table,optional" label:"Recognize tables"`
}
