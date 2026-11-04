package main

import (
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/exa/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func clientFor(ctx plugin.Ctx, timeout time.Duration) (*apiClient, error) {
	return newAPIClient("SOKEL_EXA_BASE_URL", "https://api.exa.ai", sokel.CredentialAs[Cred](ctx).APIKey, true, timeout)
}
func queryValid(q string) error {
	if strings.TrimSpace(q) == "" || len(q) > 20000 {
		return errors.New("Query must contain 1-20000 bytes of nonempty text")
	}
	return nil
}
func objectSchema(s map[string]any) error {
	if len(s) > 0 {
		if typ, ok := s["type"].(string); !ok || typ != "object" {
			return errors.New("output_schema must explicitly declare an object root")
		}
	}
	return nil
}
func contents(c schema.ContentOptions) (schema.ContentOptions, error) {
	if e := checkRange(c.MaxAgeHours, -1, 720, "max_age_hours"); e != nil {
		return c, e
	}
	if c.Text == nil && c.Highlights == nil {
		v := true
		c.Highlights = &v
	}
	return c, nil
}

type searchResponse struct {
	RequestID string                 `json:"requestId"`
	Results   []schema.Result        `json:"results"`
	Statuses  []schema.ContentStatus `json:"statuses"`
	Cost      schema.Cost            `json:"costDollars"`
	Error     json.RawMessage        `json:"error"`
}

func (r *searchResponse) validate() error {
	if r.RequestID == "" || r.Results == nil || len(r.Error) > 0 && string(r.Error) != "null" {
		return errors.New("Exa returned an incomplete or unsuccessful search response")
	}
	return nil
}
func opSearch(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	if e := queryValid(in.Query); e != nil {
		return nil, e
	}
	if e := checkRange(in.Options.NumResults, 1, 100, "num_results"); e != nil {
		return nil, e
	}
	switch in.SearchType {
	case "", "auto", "lite", "deep-lite", "deep":
	default:
		return nil, errors.New("search_type must be auto, lite, deep-lite or deep")
	}
	if len(in.IncludeDomains) > 1200 || len(in.ExcludeDomains) > 1200 {
		return nil, errors.New("Domain filters allow at most 1200 entries each")
	}
	for _, xs := range [][]string{in.IncludeDomains, in.ExcludeDomains} {
		for _, s := range xs {
			if strings.TrimSpace(s) == "" || len(s) > 2048 || strings.ContainsAny(s, "\r\n") {
				return nil, errors.New("Domain filters must be nonempty single-line domains or paths")
			}
		}
	}
	var from, to time.Time
	var e error
	if in.StartPublishedDate != "" {
		from, e = time.Parse(time.RFC3339, in.StartPublishedDate)
		if e != nil {
			return nil, errors.New("start_published_date must be RFC3339")
		}
	}
	if in.EndPublishedDate != "" {
		to, e = time.Parse(time.RFC3339, in.EndPublishedDate)
		if e != nil {
			return nil, errors.New("end_published_date must be RFC3339")
		}
	}
	if !from.IsZero() && !to.IsZero() && !to.After(from) {
		return nil, errors.New("Publication end must be after publication start")
	}
	if in.SearchType == "lite" && in.Contents.MaxAgeHours != nil {
		return nil, errors.New("lite search does not support live-fetch cache-age controls")
	}
	co, e := contents(in.Contents)
	if e != nil {
		return nil, e
	}
	body := struct {
		Query    string                `json:"query"`
		Type     string                `json:"type,omitempty"`
		Num      *int                  `json:"numResults,omitempty"`
		Include  []string              `json:"includeDomains,omitempty"`
		Exclude  []string              `json:"excludeDomains,omitempty"`
		From     string                `json:"startPublishedDate,omitempty"`
		To       string                `json:"endPublishedDate,omitempty"`
		Contents schema.ContentOptions `json:"contents"`
	}{in.Query, in.SearchType, in.Options.NumResults, in.IncludeDomains, in.ExcludeDomains, in.StartPublishedDate, in.EndPublishedDate, co}
	c, e := clientFor(ctx, 150*time.Second)
	if e != nil {
		return nil, e
	}
	var r searchResponse
	if e = c.request(ctx, http.MethodPost, "/search", body, nil, &r); e != nil {
		return nil, e
	}
	if e = r.validate(); e != nil {
		return nil, e
	}
	return &SearchOut{RequestID: r.RequestID, Results: r.Results, Cost: r.Cost}, nil
}
func opGetContents(ctx plugin.Ctx, in *GetContentsIn) (*GetContentsOut, error) {
	if (len(in.Urls) == 0) == (len(in.IDs) == 0) || len(in.Urls)+len(in.IDs) > 100 {
		return nil, errors.New("Provide exactly one of urls or ids with 1-100 entries")
	}
	urls := make([]string, len(in.Urls))
	for i, s := range in.Urls {
		u, e := pageURL(s)
		if e != nil {
			return nil, e
		}
		if len(u) > 2048 {
			return nil, errors.New("Page URLs must not exceed 2048 bytes")
		}
		urls[i] = u
	}
	for _, s := range in.IDs {
		if strings.TrimSpace(s) == "" || len(s) > 2048 || strings.ContainsAny(s, "\r\n") {
			return nil, errors.New("Document IDs must contain 1-2048 bytes")
		}
	}
	co, e := contents(in.Options)
	if e != nil {
		return nil, e
	}
	body := struct {
		URLs []string `json:"urls,omitempty"`
		IDs  []string `json:"ids,omitempty"`
		schema.ContentOptions
	}{urls, in.IDs, co}
	c, e := clientFor(ctx, 150*time.Second)
	if e != nil {
		return nil, e
	}
	var r searchResponse
	if e = c.request(ctx, http.MethodPost, "/contents", body, nil, &r); e != nil {
		return nil, e
	}
	if e = r.validate(); e != nil {
		return nil, e
	}
	if r.Statuses == nil {
		return nil, errors.New("Exa omitted per-document content statuses")
	}
	for i := range r.Statuses {
		if r.Statuses[i].Status != "success" && r.Statuses[i].Status != "error" {
			return nil, errors.New("Exa returned an unknown content fetch status")
		}
		if r.Statuses[i].Error != nil && r.Statuses[i].Error.Tag != nil {
			s := strings.ReplaceAll(*r.Statuses[i].Error.Tag, c.key, "[redacted]")
			r.Statuses[i].Error.Tag = &s
		}
	}
	return &GetContentsOut{RequestID: r.RequestID, Results: r.Results, Statuses: r.Statuses, Cost: r.Cost}, nil
}
func opAnswer(ctx plugin.Ctx, in *AnswerIn) (*AnswerOut, error) {
	if e := queryValid(in.Query); e != nil {
		return nil, e
	}
	if e := objectSchema(in.OutputSchema); e != nil {
		return nil, e
	}
	switch in.Model {
	case "", "exa", "exa-pro", "exa-research", "exa-fast":
	default:
		return nil, errors.New("model must be exa, exa-pro, exa-research or exa-fast")
	}
	body := struct {
		Query  string         `json:"query"`
		Model  string         `json:"model,omitempty"`
		System string         `json:"systemPrompt,omitempty"`
		Schema map[string]any `json:"outputSchema,omitempty"`
		Text   *bool          `json:"text,omitempty"`
	}{in.Query, in.Model, in.SystemPrompt, in.OutputSchema, in.Options.Text}
	c, e := clientFor(ctx, 150*time.Second)
	if e != nil {
		return nil, e
	}
	var r struct {
		RequestID string          `json:"requestId"`
		Answer    json.RawMessage `json:"answer"`
		Citations []schema.Result `json:"citations"`
		Cost      schema.Cost     `json:"costDollars"`
	}
	if e = c.request(ctx, http.MethodPost, "/answer", body, nil, &r); e != nil {
		return nil, e
	}
	if r.RequestID == "" || len(r.Answer) == 0 || string(r.Answer) == "null" {
		return nil, errors.New("Exa response is missing an answer or request ID")
	}
	out := &AnswerOut{RequestID: r.RequestID, Citations: r.Citations, Cost: r.Cost}
	if len(in.OutputSchema) > 0 {
		if json.Unmarshal(r.Answer, &out.AnswerData) != nil || out.AnswerData == nil {
			return nil, errors.New("Exa did not return the requested structured object")
		}
	} else {
		if json.Unmarshal(r.Answer, &out.Answer) != nil {
			return nil, errors.New("Exa did not return a text answer")
		}
	}
	if out.Citations == nil {
		out.Citations = []schema.Result{}
	}
	return out, nil
}

type agentResponse struct {
	schema.Agent
	Object string `json:"object"`
	Output *struct {
		Text       string          `json:"text"`
		Structured json.RawMessage `json:"structured"`
	} `json:"output"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
	Cost schema.Cost `json:"costDollars"`
}

func (r *agentResponse) validate() error {
	if validID(r.ID) != nil || r.Object != "agent_run" || r.CreatedAt == "" {
		return errors.New("Exa returned an incomplete agent run")
	}
	switch r.Status {
	case "queued", "running", "completed", "failed", "cancelled":
		return nil
	}
	return errors.New("Exa returned an unknown agent run status")
}
func opAgentStart(ctx plugin.Ctx, in *AgentStartIn) (*AgentStartOut, error) {
	if e := queryValid(in.Query); e != nil {
		return nil, e
	}
	if e := objectSchema(in.OutputSchema); e != nil {
		return nil, e
	}
	effort := in.Effort
	switch effort {
	case "", "auto", "minimal", "low", "medium", "high", "xhigh", "ultra":
	default:
		return nil, errors.New("Unsupported agent effort")
	}
	var budget *schema.Budget
	if in.Budget.MaxCostDollars != nil || in.Budget.MaxDurationSeconds != nil {
		budget = &in.Budget
		if effort != "" && effort != "auto" && effort != "ultra" {
			return nil, errors.New("Agent budget is supported only for auto and ultra effort")
		}
	}
	if p := in.Budget.MaxCostDollars; p != nil {
		if math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 1 || *p > 100 {
			return nil, errors.New("max_cost_dollars must be between 1 and 100")
		}
	}
	if in.Budget.MaxDurationSeconds != nil {
		if effort != "ultra" {
			return nil, errors.New("max_duration_seconds requires ultra effort")
		}
		if e := checkRange(in.Budget.MaxDurationSeconds, 300, 10800, "max_duration_seconds"); e != nil {
			return nil, e
		}
	}
	body := struct {
		Query  string         `json:"query"`
		Effort string         `json:"effort,omitempty"`
		System string         `json:"systemPrompt,omitempty"`
		Schema map[string]any `json:"outputSchema,omitempty"`
		Budget *schema.Budget `json:"budget,omitempty"`
	}{in.Query, effort, in.SystemPrompt, in.OutputSchema, budget}
	c, e := clientFor(ctx, 45*time.Second)
	if e != nil {
		return nil, e
	}
	var r agentResponse
	if e = c.request(ctx, http.MethodPost, "/agent/runs", body, nil, &r); e != nil {
		return nil, e
	}
	if e = r.validate(); e != nil {
		return nil, e
	}
	return &AgentStartOut{Run: r.Agent}, nil
}
func opAgentStatus(ctx plugin.Ctx, in *AgentStatusIn) (*AgentStatusOut, error) {
	if e := validID(in.RunID); e != nil {
		return nil, e
	}
	if in.RunID == "." || in.RunID == ".." {
		return nil, errors.New("Invalid run ID")
	}
	c, e := clientFor(ctx, 45*time.Second)
	if e != nil {
		return nil, e
	}
	var r agentResponse
	if e = c.request(ctx, http.MethodGet, "/agent/runs/"+url.PathEscape(in.RunID), nil, nil, &r); e != nil {
		return nil, e
	}
	if e = r.validate(); e != nil {
		return nil, e
	}
	out := &AgentStatusOut{Run: r.Agent, Cost: r.Cost}
	if r.Output != nil {
		out.Text = r.Output.Text
		if len(r.Output.Structured) > 0 && string(r.Output.Structured) != "null" {
			if json.Unmarshal(r.Output.Structured, &out.Structured) != nil {
				return nil, errors.New("Agent structured output is not an object; this plugin supports object-root schemas")
			}
		}
	}
	if r.Error != nil {
		out.ErrorCode = strings.ReplaceAll(r.Error.Code, c.key, "[redacted]")
	}
	return out, nil
}
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	c, e := clientFor(ctx, 20*time.Second)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	var r struct {
		Object  string            `json:"object"`
		Data    []json.RawMessage `json:"data"`
		HasMore *bool             `json:"hasMore"`
	}
	if e = c.request(ctx, http.MethodGet, "/agent/runs?limit=1", nil, nil, &r); e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.Object != "list" || r.Data == nil || r.HasMore == nil {
		return &HealthCheckOut{Message: "Exa returned an incomplete authentication response"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "API key accepted by the read-only Agent runs endpoint; search/answer credits and product access are not verified"}, nil
}
