package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/crawl4ai/schema"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var taskPattern = regexp.MustCompile(`^crawl_[a-f0-9]{8}$`)

func pageURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && len(raw) <= 8192 && utf8.ValidString(raw) && u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Opaque == "" && !strings.ContainsAny(raw, "\\\r\n\t \x00")
}

type runConfig struct {
	PageTimeout        int     `json:"page_timeout"`
	Stream             bool    `json:"stream"`
	WordCountThreshold *int    `json:"word_count_threshold,omitempty"`
	OnlyText           *bool   `json:"only_text,omitempty"`
	CSSSelector        *string `json:"css_selector,omitempty"`
}
type crawlRequest struct {
	URLs          []string  `json:"urls"`
	BrowserConfig struct{}  `json:"browser_config"`
	CrawlerConfig runConfig `json:"crawler_config"`
}

func crawlBody(urls []string, o schema.CrawlOptions) (crawlRequest, error) {
	body := crawlRequest{URLs: urls, CrawlerConfig: runConfig{PageTimeout: 30000, WordCountThreshold: o.WordCountThreshold, OnlyText: o.OnlyText, CSSSelector: o.CSSSelector}}
	if len(urls) < 1 || len(urls) > 5 {
		return body, errors.New("Provide between 1 and 5 page URLs.")
	}
	seen := map[string]bool{}
	for _, u := range urls {
		if !pageURL(u) || seen[u] {
			return body, errors.New("Page URLs must be distinct absolute HTTP or HTTPS URLs without credentials, at most 8192 bytes each.")
		}
		seen[u] = true
	}
	if o.PageTimeoutMS != nil {
		body.CrawlerConfig.PageTimeout = *o.PageTimeoutMS
	}
	if body.CrawlerConfig.PageTimeout < 100 || body.CrawlerConfig.PageTimeout > 60000 {
		return body, errors.New("Page timeout must be 100–60000 milliseconds.")
	}
	if o.WordCountThreshold != nil && (*o.WordCountThreshold < 0 || *o.WordCountThreshold > 10000) {
		return body, errors.New("Minimum word count must be 0–10000.")
	}
	if o.CSSSelector != nil && (!utf8.ValidString(*o.CSSSelector) || len(*o.CSSSelector) > 2048 || strings.ContainsAny(*o.CSSSelector, "\x00\r\n")) {
		return body, errors.New("Content CSS selector must be valid single-line text of at most 2048 bytes.")
	}
	return body, nil
}
func parseResults(raw json.RawMessage, expected int) ([]schema.CrawlResult, bool, error) {
	if e := requireKeys(raw, "success", "results"); e != nil {
		return nil, false, e
	}
	var v struct {
		Success bool              `json:"success"`
		Results []json.RawMessage `json:"results"`
	}
	if e := decodeJSON(raw, &v); e != nil {
		return nil, false, e
	}
	if !v.Success || len(v.Results) < 1 || len(v.Results) > 5 || (expected > 0 && len(v.Results) != expected) {
		return nil, false, errors.New("Crawl4AI returned an unsuccessful or incomplete crawl response.")
	}
	out := make([]schema.CrawlResult, 0, len(v.Results))
	all := true
	for _, b := range v.Results {
		if e := requireKeys(b, "url", "success"); e != nil {
			return nil, false, e
		}
		var r struct {
			URL        string `json:"url"`
			Success    bool   `json:"success"`
			StatusCode *int   `json:"status_code"`
			Metadata   struct {
				Title *string `json:"title"`
			} `json:"metadata"`
			Markdown *struct {
				Raw *string `json:"raw_markdown"`
			} `json:"markdown"`
		}
		if e := decodeJSON(b, &r); e != nil {
			return nil, false, e
		}
		if r.Success && !pageURL(r.URL) {
			return nil, false, errors.New("Crawl4AI returned an invalid successful page URL.")
		}
		if r.StatusCode != nil && (*r.StatusCode < 0 || *r.StatusCode > 599) {
			return nil, false, errors.New("Crawl4AI returned an invalid page HTTP status.")
		}
		item := schema.CrawlResult{URL: r.URL, Success: r.Success, StatusCode: r.StatusCode, Title: r.Metadata.Title, Message: "Page crawled."}
		if r.StatusCode != nil && *r.StatusCode >= 400 {
			item.Success = false
		}
		if item.Success {
			if r.Markdown == nil || r.Markdown.Raw == nil {
				return nil, false, errors.New("Crawl4AI omitted Markdown from a successful page.")
			}
			item.Markdown = r.Markdown.Raw
		} else {
			all = false
			item.Message = "Page crawl failed. Inspect the server logs; raw internal error details are not returned."
			item.Title = nil
			if !pageURL(item.URL) {
				item.URL = ""
			}
		}
		out = append(out, item)
	}
	return out, all, nil
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	b, e := c.request(ctx, "GET", "/health", nil, nil, 200)
	if e == nil {
		var v struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}
		e = decodeJSON(b, &v)
		if e == nil && (v.Status != "ok" || v.Version == "") {
			e = errors.New("Crawl4AI returned invalid health metadata.")
		}
	}
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Crawl4AI is reachable. This public health endpoint does not verify the token or browser readiness."}, nil
}
func (c *client) crawl(ctx context.Context, in *CrawlIn) (*CrawlOut, error) {
	body, e := crawlBody(in.Urls, in.Options)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "POST", "/crawl", nil, body, 200)
	if e != nil {
		return nil, e
	}
	results, all, e := parseResults(b, len(in.Urls))
	if e != nil {
		return nil, e
	}
	return &CrawlOut{Results: results, AllSucceeded: all}, nil
}
func (c *client) submit_crawl(ctx context.Context, in *SubmitCrawlIn) (*SubmitCrawlOut, error) {
	body, e := crawlBody(in.Urls, in.Options)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "POST", "/crawl/job", nil, body, 202)
	if e != nil {
		return nil, e
	}
	var v struct {
		ID string `json:"task_id"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	if !taskPattern.MatchString(v.ID) {
		return nil, errors.New("Crawl4AI returned an invalid task receipt.")
	}
	return &SubmitCrawlOut{TaskID: v.ID, Accepted: true}, nil
}
func (c *client) get_task(ctx context.Context, in *GetTaskIn) (*GetTaskOut, error) {
	if !taskPattern.MatchString(in.TaskID) {
		return nil, errors.New("Use the crawl task ID returned by Submit crawl task.")
	}
	b, e := c.request(ctx, "GET", "/crawl/job/"+in.TaskID, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		ID     string          `json:"task_id"`
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	if v.ID != in.TaskID {
		return nil, errors.New("Crawl4AI returned a mismatched task ID.")
	}
	out := &GetTaskOut{TaskID: v.ID, Status: v.Status, Results: []schema.CrawlResult{}}
	switch v.Status {
	case "processing":
		out.Message = "The task is still processing. Query this task again later."
	case "failed":
		out.Message = "The task failed. Inspect the server logs; raw internal error details are not returned."
	case "completed":
		out.Completed = true
		out.Results, out.AllSucceeded, e = parseResults(v.Result, 0)
		if e != nil {
			return nil, e
		}
		if out.AllSucceeded {
			out.Message = "The task completed and every page succeeded."
		} else {
			out.Message = "The task completed with one or more failed pages."
		}
	default:
		return nil, errors.New("Crawl4AI returned an unsupported task status.")
	}
	return out, nil
}
func (c *client) markdown(ctx context.Context, in *MarkdownIn) (*MarkdownOut, error) {
	if !pageURL(in.URL) {
		return nil, errors.New("Provide an absolute HTTP or HTTPS page URL without credentials, at most 8192 bytes.")
	}
	body := struct {
		URL    string `json:"url"`
		Filter string `json:"f"`
		Cache  string `json:"c"`
	}{in.URL, "raw", "0"}
	if in.UseCache {
		body.Cache = "1"
	}
	b, e := c.request(ctx, "POST", "/md", nil, body, 200)
	if e != nil {
		return nil, e
	}
	if e = requireKeys(b, "success", "url", "filter", "markdown"); e != nil {
		return nil, e
	}
	var v struct {
		URL      string `json:"url"`
		Success  bool   `json:"success"`
		Filter   string `json:"filter"`
		Markdown string `json:"markdown"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	if !v.Success || v.URL != in.URL || v.Filter != "raw" {
		return nil, errors.New("Crawl4AI returned an unsuccessful or mismatched Markdown response.")
	}
	return &MarkdownOut{URL: v.URL, Markdown: v.Markdown}, nil
}
