package main

import (
	"context"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/bocha-search/schema"
	"strings"
	"time"
)

func validFreshness(s string) bool {
	switch s {
	case "noLimit", "oneDay", "oneWeek", "oneMonth", "oneYear":
		return true
	}
	parts := strings.Split(s, "..")
	if len(parts) > 2 {
		return false
	}
	var previous time.Time
	for _, v := range parts {
		d, e := time.Parse("2006-01-02", v)
		if e != nil || d.Before(previous) {
			return false
		}
		previous = d
	}
	return true
}
func (a *api) WebSearch(ctx context.Context, in *WebSearchIn) (*WebSearchOut, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, fmt.Errorf("search query is required")
	}
	fresh := in.Freshness
	if fresh == "" {
		fresh = "noLimit"
	}
	if !validFreshness(fresh) {
		return nil, fmt.Errorf("invalid freshness; use a relative range, YYYY-MM-DD or an ascending date range joined by ..")
	}
	count, summary := 10, true
	if in.Options.Count != nil {
		count = *in.Options.Count
	}
	if count < 1 || count > 50 {
		return nil, fmt.Errorf("result count must be between 1 and 50")
	}
	if in.Options.Summary != nil {
		summary = *in.Options.Summary
	}
	var res struct {
		Code  *int   `json:"code"`
		LogID string `json:"log_id"`
		Data  *struct {
			QueryContext struct {
				Original string `json:"originalQuery"`
			} `json:"queryContext"`
			WebPages *struct {
				Total int             `json:"totalEstimatedMatches"`
				Value []schema.Result `json:"value"`
			} `json:"webPages"`
		} `json:"data"`
	}
	e := a.json(ctx, "POST", "/v1/web-search", nil, map[string]any{"query": in.Query, "freshness": fresh, "count": count, "summary": summary}, &res)
	if e != nil {
		return nil, e
	}
	if res.Code != nil && *res.Code != 200 {
		return nil, fmt.Errorf("Bocha rejected the search with code %d; check key, quota and parameters", *res.Code)
	}
	query := in.Query
	results := []schema.Result{}
	total := 0
	if res.Data != nil {
		if res.Data.QueryContext.Original != "" {
			query = res.Data.QueryContext.Original
		}
		if res.Data.WebPages != nil {
			total = res.Data.WebPages.Total
			if res.Data.WebPages.Value != nil {
				results = res.Data.WebPages.Value
			}
		}
	}
	// Bocha's official provider permits null/absent webPages for empty searches.
	// Require an explicit successful code if no result collection is present.
	if (res.Data == nil || res.Data.WebPages == nil || res.Data.WebPages.Value == nil) && (res.Code == nil || *res.Code != 200) {
		return nil, fmt.Errorf("Bocha response has neither a result collection nor an explicit success code")
	}
	for _, v := range results {
		if v.URL == "" {
			return nil, fmt.Errorf("Bocha result is missing a URL")
		}
	}
	return &WebSearchOut{Results: results, Query: query, TotalEstimatedMatches: total, LogID: res.LogID}, nil
}
func (a *api) VerifyCredential(ctx context.Context, in *VerifyCredentialIn) (*VerifyCredentialOut, error) {
	if strings.TrimSpace(in.ProbeQuery) == "" {
		return nil, fmt.Errorf("probe query is required; verification performs one billable search")
	}
	count, summary := 1, false
	_, e := a.WebSearch(ctx, &WebSearchIn{Query: in.ProbeQuery, Options: schema.SearchOptions{Count: &count, Summary: &summary}})
	if e != nil {
		return &VerifyCredentialOut{Message: e.Error()}, nil
	}
	return &VerifyCredentialOut{OK: true, Message: "Credential verified by one billable search"}, nil
}
