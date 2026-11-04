package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/searxng/schema"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (c *client) authorize(r *http.Request) error {
	if strings.ContainsAny(c.cred.Username, ":\r\n") || strings.ContainsAny(c.cred.Password, "\r\n") {
		return errors.New("invalid proxy authentication characters")
	}
	if (c.cred.Username == "") != (c.cred.Password == "") {
		return errors.New("set both proxy username and proxy password, or leave both empty")
	}
	if c.cred.Username != "" {
		r.SetBasicAuth(c.cred.Username, c.cred.Password)
	}
	return nil
}
func (c *client) health(ctx context.Context, _ *HealthIn) (*HealthOut, error) {
	b, _, e := c.request(ctx, "GET", "/healthz", nil, nil, http.Header{"Accept": {"text/plain"}})
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(string(b)) != "OK" {
		return nil, errors.New("SearXNG returned an unexpected health response")
	}
	return &HealthOut{Healthy: true}, nil
}
func (c *client) getConfig(ctx context.Context, _ *GetConfigIn) (*GetConfigOut, error) {
	b, h, e := c.request(ctx, "GET", "/config", nil, nil, nil)
	if e != nil {
		return nil, e
	}
	var out GetConfigOut
	if e = decodeJSON(b, h, &out); e != nil {
		return nil, e
	}
	if out.Engines == nil || out.Categories == nil || out.Version == "" {
		return nil, errors.New("SearXNG returned an incomplete instance configuration")
	}
	return &out, nil
}
func addList(q url.Values, key string, values []string) error {
	if len(values) > 30 {
		return errors.New("at most 30 engines or categories may be selected")
	}
	for _, v := range values {
		if strings.TrimSpace(v) == "" || len(v) > 100 || strings.ContainsAny(v, ",\r\n") {
			return errors.New("engine and category names must be nonempty and contain no comma or newline")
		}
	}
	if len(values) > 0 {
		q.Set(key, strings.Join(values, ","))
	}
	return nil
}
func (c *client) search(ctx context.Context, in *SearchIn) (*SearchOut, error) {
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 4096 || !utf8.ValidString(in.Query) {
		return nil, errors.New("query must contain valid UTF-8 text, up to 4096 bytes")
	}
	o := in.Options
	page, limit := 1, 20
	if o.Page != nil {
		page = *o.Page
	}
	if o.Limit != nil {
		limit = *o.Limit
	}
	if page < 1 || page > 1000 {
		return nil, errors.New("page must be between 1 and 1000")
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("limit must be between 1 and 100")
	}
	q := url.Values{"q": {in.Query}, "format": {"json"}, "pageno": {strconv.Itoa(page)}}
	if e := addList(q, "engines", o.Engines); e != nil {
		return nil, e
	}
	if e := addList(q, "categories", o.Categories); e != nil {
		return nil, e
	}
	if o.Language != nil {
		if len(*o.Language) > 40 || strings.TrimSpace(*o.Language) == "" || strings.ContainsAny(*o.Language, "\r\n") {
			return nil, errors.New("language must be a nonempty language code of at most 40 bytes")
		}
		q.Set("language", *o.Language)
	}
	if o.SafeSearch != nil {
		if *o.SafeSearch < 0 || *o.SafeSearch > 2 {
			return nil, errors.New("safe_search must be 0, 1, or 2")
		}
		q.Set("safesearch", strconv.Itoa(*o.SafeSearch))
	}
	if o.TimeRange != nil {
		switch *o.TimeRange {
		case "day", "month", "year":
			q.Set("time_range", *o.TimeRange)
		default:
			return nil, errors.New("time_range must be day, month, or year")
		}
	}
	b, h, e := c.request(ctx, "GET", "/search", q, nil, nil)
	if e != nil {
		return nil, e
	}
	var wire struct {
		Query       *string           `json:"query"`
		Results     []schema.Result   `json:"results"`
		Suggestions []string          `json:"suggestions"`
		Corrections []string          `json:"corrections"`
		Errors      []json.RawMessage `json:"unresponsive_engines"`
	}
	if e = decodeJSON(b, h, &wire); e != nil {
		return nil, e
	}
	if wire.Query == nil || wire.Results == nil {
		return nil, errors.New("SearXNG response lacks query or results; enable JSON in search.formats")
	}
	out := &SearchOut{Query: *wire.Query, Results: wire.Results, Suggestions: wire.Suggestions, Corrections: wire.Corrections, Page: page, EngineErrors: []schema.EngineError{}}
	if out.Suggestions == nil {
		out.Suggestions = []string{}
	}
	if out.Corrections == nil {
		out.Corrections = []string{}
	}
	for _, raw := range wire.Errors {
		var pair []string
		if json.Unmarshal(raw, &pair) != nil || len(pair) < 2 {
			return nil, errors.New("SearXNG returned invalid engine failure metadata")
		}
		out.EngineErrors = append(out.EngineErrors, schema.EngineError{Engine: pair[0], Reason: pair[1]})
	}
	out.Partial = len(out.EngineErrors) > 0
	if len(out.Results) > limit {
		out.Results = out.Results[:limit]
		out.Clipped = true
	}
	return out, nil
}
