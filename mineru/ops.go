package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/mineru/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func clientFor(ctx plugin.Ctx) (*apiClient, error) {
	return newAPIClient("SOKEL_MINERU_BASE_URL", "https://mineru.net", sokel.CredentialAs[Cred](ctx).APIKey, true, 45*time.Second)
}

type envelope struct {
	Code    json.RawMessage `json:"code"`
	Data    json.RawMessage `json:"data"`
	TraceID string          `json:"trace_id"`
}

var safeCode = regexp.MustCompile(`^[A-Z0-9_-]{1,20}$`)

func (c *apiClient) mineru(ctx plugin.Ctx, method, path string, body any, out any) (string, error) {
	var r envelope
	if e := c.request(ctx, method, path, body, nil, &r); e != nil {
		return "", e
	}
	code := strings.Trim(string(r.Code), "\"")
	if code != "0" {
		if !safeCode.MatchString(code) || strings.Contains(code, c.key) {
			code = "unknown"
		}
		return "", fmt.Errorf("MinerU rejected the request (code %s); check token validity, document URL and provider limits", code)
	}
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return "", errors.New("MinerU response is missing task data")
	}
	if e := json.Unmarshal(r.Data, out); e != nil {
		return "", errors.New("MinerU returned an unexpected task data shape")
	}
	return strings.ReplaceAll(r.TraceID, c.key, "[redacted]"), nil
}

type wireFile struct {
	URL        string `json:"url"`
	DataID     string `json:"data_id,omitempty"`
	IsOCR      *bool  `json:"is_ocr,omitempty"`
	PageRanges string `json:"page_ranges,omitempty"`
}
type wireOptions struct {
	ModelVersion string   `json:"model_version,omitempty"`
	Language     string   `json:"language,omitempty"`
	Formula      *bool    `json:"enable_formula,omitempty"`
	Table        *bool    `json:"enable_table,omitempty"`
	ExtraFormats []string `json:"extra_formats,omitempty"`
}

func options(model, language string, formats []string, formula, table *bool) (wireOptions, error) {
	if model != "" && model != "pipeline" && model != "vlm" && model != "MinerU-HTML" {
		return wireOptions{}, errors.New("model_version must be pipeline, vlm or MinerU-HTML")
	}
	if len(language) > 64 {
		return wireOptions{}, errors.New("language exceeds 64 bytes")
	}
	for _, f := range formats {
		if f != "docx" && f != "html" && f != "latex" {
			return wireOptions{}, errors.New("extra_formats supports only docx, html and latex; Markdown and JSON are included by default")
		}
	}
	return wireOptions{model, language, formula, table, formats}, nil
}

var rangesPattern = regexp.MustCompile(`^[0-9,-]+$`)

func file(raw, dataID, ranges string, ocr *bool) (wireFile, error) {
	u, e := pageURL(raw)
	if e != nil {
		return wireFile{}, e
	}
	if len(dataID) > 128 {
		return wireFile{}, errors.New("data_id exceeds 128 bytes")
	}
	if len(ranges) > 1024 || (ranges != "" && !rangesPattern.MatchString(ranges)) {
		return wireFile{}, errors.New("page_ranges must contain page numbers, commas and hyphens")
	}
	return wireFile{u, dataID, ocr, ranges}, nil
}
func opSubmit(ctx plugin.Ctx, in *SubmitIn) (*SubmitOut, error) {
	f, e := file(in.URL, in.DataID, in.PageRanges, in.Options.IsOCR)
	if e != nil {
		return nil, e
	}
	o, e := options(in.ModelVersion, in.Language, in.ExtraFormats, in.Options.EnableFormula, in.Options.EnableTable)
	if e != nil {
		return nil, e
	}
	body := struct {
		wireFile
		wireOptions
	}{f, o}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		TaskID string `json:"task_id"`
	}
	trace, e := c.mineru(ctx, http.MethodPost, "/api/v4/extract/task", body, &r)
	if e != nil {
		return nil, e
	}
	if e = validID(r.TaskID); e != nil {
		return nil, errors.New("MinerU response is missing a valid task ID")
	}
	return &SubmitOut{TaskID: r.TaskID, TraceID: trace}, nil
}
func opStatus(ctx plugin.Ctx, in *StatusIn) (*StatusOut, error) {
	if e := validID(in.TaskID); e != nil {
		return nil, e
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r schema.TaskResult
	trace, e := c.mineru(ctx, http.MethodGet, "/api/v4/extract/task/"+in.TaskID, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.State == "" {
		return nil, errors.New("MinerU response is missing task state")
	}
	if r.TaskID == "" {
		r.TaskID = in.TaskID
	}
	r.ErrMsg = strings.ReplaceAll(r.ErrMsg, c.key, "[redacted]")
	return &StatusOut{Result: r, TraceID: trace}, nil
}
func opBatchSubmit(ctx plugin.Ctx, in *BatchSubmitIn) (*BatchSubmitOut, error) {
	if len(in.Files) < 1 || len(in.Files) > 200 {
		return nil, errors.New("files must contain 1–200 document URLs")
	}
	files := make([]wireFile, len(in.Files))
	for i, f := range in.Files {
		var e error
		files[i], e = file(f.URL, f.DataID, f.PageRanges, f.IsOCR)
		if e != nil {
			return nil, e
		}
	}
	o, e := options(in.ModelVersion, in.Language, in.ExtraFormats, in.Options.EnableFormula, in.Options.EnableTable)
	if e != nil {
		return nil, e
	}
	body := struct {
		Files []wireFile `json:"files"`
		wireOptions
	}{files, o}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		BatchID string `json:"batch_id"`
	}
	trace, e := c.mineru(ctx, http.MethodPost, "/api/v4/extract/task/batch", body, &r)
	if e != nil {
		return nil, e
	}
	if e = validID(r.BatchID); e != nil {
		return nil, errors.New("MinerU response is missing a valid batch ID")
	}
	return &BatchSubmitOut{BatchID: r.BatchID, TraceID: trace}, nil
}
func opBatchStatus(ctx plugin.Ctx, in *BatchStatusIn) (*BatchStatusOut, error) {
	if e := validID(in.BatchID); e != nil {
		return nil, e
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		BatchID string              `json:"batch_id"`
		Results []schema.TaskResult `json:"extract_result"`
	}
	trace, e := c.mineru(ctx, http.MethodGet, "/api/v4/extract-results/batch/"+in.BatchID, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.BatchID == "" || r.Results == nil {
		return nil, errors.New("MinerU response is missing batch ID or results")
	}
	for i := range r.Results {
		if r.Results[i].State == "" {
			return nil, errors.New("MinerU batch result is missing task state")
		}
		r.Results[i].ErrMsg = strings.ReplaceAll(r.Results[i].ErrMsg, c.key, "[redacted]")
	}
	return &BatchStatusOut{BatchID: r.BatchID, Results: r.Results, TraceID: trace}, nil
}
func opHealthCheck(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
	if in.TaskID == "" {
		return &HealthCheckOut{Message: "Provide an existing task_id in the debug panel for read-only credential verification; no parsing task was created"}, nil
	}
	_, e := opStatus(ctx, &StatusIn{TaskID: in.TaskID})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "MinerU accepted the token and returned the existing task; no parsing task was created"}, nil
}
