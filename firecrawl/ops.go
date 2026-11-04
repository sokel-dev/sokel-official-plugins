package main

import (
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/firecrawl/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"net/http"
	"strconv"
	"time"
)

func clientFor(ctx plugin.Ctx) (*apiClient, error) {
	return newAPIClient("SOKEL_FIRECRAWL_BASE_URL", "https://api.firecrawl.dev", sokel.CredentialAs[Cred](ctx).APIKey, true, 150*time.Second)
}
func successful(ok *bool) error {
	if ok == nil || !*ok {
		return errors.New("Firecrawl did not confirm success; check input, account credits and service status")
	}
	return nil
}

type wireDocument struct {
	Markdown string   `json:"markdown"`
	HTML     string   `json:"html"`
	Links    []string `json:"links"`
	Metadata struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		SourceURL   string `json:"sourceURL"`
		Language    string `json:"language"`
		StatusCode  int    `json:"statusCode"`
	} `json:"metadata"`
}

func (d wireDocument) document() schema.Document {
	return schema.Document{Markdown: d.Markdown, HTML: d.HTML, Links: d.Links, Metadata: schema.PageMetadata{Title: d.Metadata.Title, Description: d.Metadata.Description, SourceURL: d.Metadata.SourceURL, Language: d.Metadata.Language, StatusCode: d.Metadata.StatusCode}}
}
func opScrape(ctx plugin.Ctx, in *ScrapeIn) (*ScrapeOut, error) {
	target, e := pageURL(in.URL)
	if e != nil {
		return nil, e
	}
	if e = checkRange(in.Options.WaitFor, 0, 60000, "wait_for"); e != nil {
		return nil, e
	}
	formats := in.Formats
	if len(formats) == 0 {
		formats = []string{"markdown"}
	}
	for _, f := range formats {
		if f != "markdown" && f != "html" && f != "links" {
			return nil, errors.New("formats supports only markdown, html and links")
		}
	}
	body := struct {
		URL             string   `json:"url"`
		Formats         []string `json:"formats"`
		OnlyMainContent *bool    `json:"onlyMainContent,omitempty"`
		WaitFor         *int     `json:"waitFor,omitempty"`
	}{target, formats, in.Options.OnlyMainContent, in.Options.WaitFor}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Success *bool         `json:"success"`
		Data    *wireDocument `json:"data"`
	}
	if e = c.request(ctx, http.MethodPost, "/v2/scrape", body, nil, &r); e != nil {
		return nil, e
	}
	if e = successful(r.Success); e != nil {
		return nil, e
	}
	if r.Data == nil {
		return nil, errors.New("Firecrawl response is missing page data")
	}
	return &ScrapeOut{Document: r.Data.document()}, nil
}
func opCrawl(ctx plugin.Ctx, in *CrawlIn) (*CrawlOut, error) {
	target, e := pageURL(in.URL)
	if e != nil {
		return nil, e
	}
	if e = checkRange(in.Options.Limit, 1, 10000, "limit"); e != nil {
		return nil, e
	}
	if e = checkRange(in.Options.MaxDiscoveryDepth, 0, 100, "max_discovery_depth"); e != nil {
		return nil, e
	}
	body := struct {
		URL           string   `json:"url"`
		Limit         *int     `json:"limit,omitempty"`
		Depth         *int     `json:"maxDiscoveryDepth,omitempty"`
		Include       []string `json:"includePaths,omitempty"`
		Exclude       []string `json:"excludePaths,omitempty"`
		ScrapeOptions struct {
			Formats []string `json:"formats"`
		} `json:"scrapeOptions"`
	}{URL: target, Limit: in.Options.Limit, Depth: in.Options.MaxDiscoveryDepth, Include: in.IncludePaths, Exclude: in.ExcludePaths}
	body.ScrapeOptions.Formats = []string{"markdown"}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Success *bool  `json:"success"`
		ID      string `json:"id"`
		URL     string `json:"url"`
	}
	if e = c.request(ctx, http.MethodPost, "/v2/crawl", body, nil, &r); e != nil {
		return nil, e
	}
	if e = successful(r.Success); e != nil {
		return nil, e
	}
	if e = validID(r.ID); e != nil {
		return nil, errors.New("Firecrawl response is missing a valid crawl ID")
	}
	return &CrawlOut{ID: r.ID, URL: r.URL}, nil
}
func opCrawlStatus(ctx plugin.Ctx, in *CrawlStatusIn) (*CrawlStatusOut, error) {
	if e := validID(in.ID); e != nil {
		return nil, e
	}
	if e := checkRange(in.Options.Skip, 0, 1000000, "skip"); e != nil {
		return nil, e
	}
	path := "/v2/crawl/" + in.ID
	if in.Options.Skip != nil {
		path += "?skip=" + strconv.Itoa(*in.Options.Skip)
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Status      string         `json:"status"`
		Total       int            `json:"total"`
		Completed   int            `json:"completed"`
		CreditsUsed int            `json:"creditsUsed"`
		ExpiresAt   string         `json:"expiresAt"`
		Next        string         `json:"next"`
		Data        []wireDocument `json:"data"`
	}
	if e = c.request(ctx, http.MethodGet, path, nil, nil, &r); e != nil {
		return nil, e
	}
	if r.Status == "" {
		return nil, errors.New("Firecrawl response is missing crawl status")
	}
	if r.Status == "failed" || r.Status == "cancelled" {
		return nil, fmt.Errorf("Firecrawl crawl %s; inspect the job in the provider dashboard", r.Status)
	}
	docs := make([]schema.Document, 0, len(r.Data))
	for _, d := range r.Data {
		docs = append(docs, d.document())
	}
	return &CrawlStatusOut{Status: r.Status, Total: r.Total, Completed: r.Completed, CreditsUsed: r.CreditsUsed, ExpiresAt: r.ExpiresAt, Next: r.Next, Documents: docs}, nil
}
func opMap(ctx plugin.Ctx, in *MapIn) (*MapOut, error) {
	target, e := pageURL(in.URL)
	if e != nil {
		return nil, e
	}
	if e = checkRange(in.Options.Limit, 1, 10000, "limit"); e != nil {
		return nil, e
	}
	body := struct {
		URL     string `json:"url"`
		Search  string `json:"search,omitempty"`
		Limit   *int   `json:"limit,omitempty"`
		Include *bool  `json:"includeSubdomains,omitempty"`
	}{target, in.Search, in.Options.Limit, in.Options.IncludeSubdomains}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Success *bool             `json:"success"`
		Links   []schema.MapLinks `json:"links"`
	}
	if e = c.request(ctx, http.MethodPost, "/v2/map", body, nil, &r); e != nil {
		return nil, e
	}
	if e = successful(r.Success); e != nil {
		return nil, e
	}
	if r.Links == nil {
		return nil, errors.New("Firecrawl response is missing map links")
	}
	return &MapOut{Links: r.Links}, nil
}
func opExtract(ctx plugin.Ctx, in *ExtractIn) (*ExtractOut, error) {
	if len(in.Urls) < 1 || len(in.Urls) > 100 {
		return nil, errors.New("urls must contain 1–100 URLs")
	}
	urls := make([]string, len(in.Urls))
	for i, u := range in.Urls {
		var e error
		urls[i], e = pageURL(u)
		if e != nil {
			return nil, e
		}
	}
	if in.Prompt == "" && len(in.Schema) == 0 {
		return nil, errors.New("Provide an extraction prompt or an object JSON Schema")
	}
	if kind, ok := in.Schema["type"]; ok && kind != "object" {
		return nil, errors.New("Extraction schema must have an object root")
	}
	body := struct {
		URLs   []string       `json:"urls"`
		Prompt string         `json:"prompt,omitempty"`
		Schema map[string]any `json:"schema,omitempty"`
	}{urls, in.Prompt, in.Schema}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Success *bool  `json:"success"`
		ID      string `json:"id"`
	}
	if e = c.request(ctx, http.MethodPost, "/v2/extract", body, nil, &r); e != nil {
		return nil, e
	}
	if e = successful(r.Success); e != nil {
		return nil, e
	}
	if e = validID(r.ID); e != nil {
		return nil, errors.New("Firecrawl response is missing a valid extraction ID")
	}
	return &ExtractOut{ID: r.ID}, nil
}
func opExtractStatus(ctx plugin.Ctx, in *ExtractStatusIn) (*ExtractStatusOut, error) {
	if e := validID(in.ID); e != nil {
		return nil, e
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Success   *bool          `json:"success"`
		Status    string         `json:"status"`
		Data      map[string]any `json:"data"`
		ExpiresAt string         `json:"expiresAt"`
	}
	if e = c.request(ctx, http.MethodGet, "/v2/extract/"+in.ID, nil, nil, &r); e != nil {
		return nil, e
	}
	if e = successful(r.Success); e != nil {
		return nil, e
	}
	if r.Status == "" {
		return nil, errors.New("Firecrawl response is missing extraction status")
	}
	if r.Status == "failed" || r.Status == "cancelled" {
		return nil, fmt.Errorf("Firecrawl extraction %s; inspect the job in the provider dashboard", r.Status)
	}
	if r.Status == "completed" && r.Data == nil {
		return nil, errors.New("Completed extraction is missing result data")
	}
	return &ExtractStatusOut{Status: r.Status, Data: r.Data, ExpiresAt: r.ExpiresAt}, nil
}
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	c, e := clientFor(ctx)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	c.http.Timeout = 20 * time.Second
	var r struct {
		Success *bool `json:"success"`
		Data    *struct {
			Remaining *int `json:"remainingCredits"`
		} `json:"data"`
	}
	if e = c.request(ctx, http.MethodGet, "/v2/team/credit-usage", nil, nil, &r); e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if e = successful(r.Success); e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.Data == nil || r.Data.Remaining == nil {
		return &HealthCheckOut{Message: "Credit response is incomplete; credentials were not verified"}, nil
	}
	if *r.Data.Remaining <= 0 {
		return &HealthCheckOut{Message: "Credentials accepted, but no Firecrawl credits remain"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Credentials accepted; Firecrawl credits are available"}, nil
}
