package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These are official-documentation-shaped mock responses, not live-account captures.
// Sources and the exact version boundary are recorded in README.md.
func TestDocumentedWireRequests(t *testing.T) {
	runCases(t, []wireCase{
		{"scrape", `{"url":"https://example.com","options":{"only_main_content":false,"wait_for":0}}`, "POST", "/v2/scrape", `{"url":"https://example.com","formats":["markdown"],"onlyMainContent":false,"waitFor":0}`, `{"success":true,"data":{"markdown":"# Example","metadata":{"title":"Example","sourceURL":"https://example.com","statusCode":200}}}`, func(t *testing.T, v any) {
			o := v.(*ScrapeOut)
			if o.Document.Markdown != "# Example" || o.Document.Metadata.SourceURL != "https://example.com" || o.Document.Metadata.StatusCode != 200 {
				t.Fatalf("bad mapping %+v", o)
			}
		}},
		{"crawl", `{"url":"https://example.com","options":{"limit":5,"max_discovery_depth":0}}`, "POST", "/v2/crawl", `{"url":"https://example.com","limit":5,"maxDiscoveryDepth":0,"scrapeOptions":{"formats":["markdown"]}}`, `{"success":true,"id":"job-1","url":"https://api.firecrawl.dev/v2/crawl/job-1"}`, nil},
		{"crawl_status", `{"id":"job-1","options":{"skip":10}}`, "GET", "/v2/crawl/job-1?skip=10", "", `{"status":"completed","total":1,"completed":1,"creditsUsed":1,"data":[{"markdown":"hello"}],"next":"https://api.firecrawl.dev/v2/crawl/job-1?skip=20"}`, func(t *testing.T, v any) {
			o := v.(*CrawlStatusOut)
			if len(o.Documents) != 1 || o.CreditsUsed != 1 || o.Next == "" {
				t.Fatalf("bad status %+v", o)
			}
		}},
		{"map", `{"url":"https://example.com","search":"docs","options":{"include_subdomains":false,"limit":2}}`, "POST", "/v2/map", `{"url":"https://example.com","search":"docs","includeSubdomains":false,"limit":2}`, `{"success":true,"links":[{"url":"https://example.com/docs","title":"Docs"}]}`, nil},
		{"extract", `{"urls":["https://example.com"],"schema":{"type":"object","properties":{"name":{"type":"string"}}}}`, "POST", "/v2/extract", `{"urls":["https://example.com"],"schema":{"type":"object","properties":{"name":{"type":"string"}}}}`, `{"success":true,"id":"extract-1"}`, nil},
		{"extract_status", `{"id":"extract-1"}`, "GET", "/v2/extract/extract-1", "", `{"success":true,"status":"completed","data":{"name":"Example"}}`, func(t *testing.T, v any) {
			if v.(*ExtractStatusOut).Data["name"] != "Example" {
				t.Fatal(v)
			}
		}},
		{"health_check", `{}`, "GET", "/v2/team/credit-usage", "", `{"success":true,"data":{"remainingCredits":1000,"planCredits":500000}}`, func(t *testing.T, v any) {
			if !v.(*HealthCheckOut).OK {
				t.Fatal(v)
			}
		}},
	}, []string{"SOKEL_FIRECRAWL_BASE_URL"})
}
func TestBoundaries(t *testing.T) {
	runBoundaries(t, "SOKEL_FIRECRAWL_BASE_URL", "scrape", `{"url":"https://example.com"}`, "")
}
func TestOptionalOmissionAndValidation(t *testing.T) {
	runCases(t, []wireCase{{"scrape", `{"url":"https://example.com"}`, "POST", "/v2/scrape", `{"url":"https://example.com","formats":["markdown"]}`, `{"success":true,"data":{"markdown":"ok"}}`, nil}}, []string{"SOKEL_FIRECRAWL_BASE_URL"})
	for _, x := range []struct{ op, in string }{{"scrape", `{"url":"file:///etc/passwd"}`}, {"scrape", `{"url":"https://user:secret@example.com"}`}, {"scrape", `{"url":"https://example.com","formats":["unknown"]}`}, {"crawl", `{"url":"https://example.com","options":{"limit":0}}`}, {"extract", `{"urls":[]}`}, {"crawl_status", `{"id":"../team/credit-usage"}`}} {
		_, e := invoke(context.Background(), "test-secret", x.op, x.in)
		if e == nil {
			t.Fatalf("accepted invalid %s", x.in)
		}
	}
}
func TestBusinessErrorsAndHealth(t *testing.T) {
	for _, body := range []string{`{"success":false,"error":"test-secret"}`, `{}`, `{"success":true,"data":null}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_FIRECRAWL_BASE_URL", s.URL)
		_, e := invoke(context.Background(), "test-secret", "scrape", `{"url":"https://example.com"}`)
		if e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("bad error %v", e)
		}
		v, e := invoke(context.Background(), "test-secret", "health_check", `{}`)
		if e != nil || v.(*HealthCheckOut).OK {
			t.Fatalf("health falsely succeeds %v %v", v, e)
		}
		s.Close()
	}
}

func TestEveryOperationRejectsMissingCredentialsAndIncompleteResponses(t *testing.T) {
	inputs := map[string]string{"scrape": `{"url":"https://example.com"}`, "crawl": `{"url":"https://example.com"}`, "crawl_status": `{"id":"job-1"}`, "map": `{"url":"https://example.com"}`, "extract": `{"urls":["https://example.com"],"prompt":"Extract title"}`, "extract_status": `{"id":"job-1"}`}
	for op, input := range inputs {
		if _, e := invoke(context.Background(), "", op, input); e == nil {
			t.Fatalf("%s accepted missing credential", op)
		}
	}
	for _, response := range []string{`{}`, `{"success":true}`, `{"success":false,"error":"test-secret"}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, response)
		}))
		t.Setenv("SOKEL_FIRECRAWL_BASE_URL", s.URL)
		for op, input := range inputs {
			if _, e := invoke(context.Background(), "test-secret", op, input); e == nil || strings.Contains(e.Error(), "test-secret") {
				t.Fatalf("%s accepted incomplete response: %v", op, e)
			}
		}
		s.Close()
	}
}
func TestFailedJobsAndZeroCredits(t *testing.T) {
	runCases(t, []wireCase{{"health_check", `{}`, "GET", "/v2/team/credit-usage", "", `{"success":true,"data":{"remainingCredits":0}}`, func(t *testing.T, v any) {
		if v.(*HealthCheckOut).OK {
			t.Fatal("zero credits reported usable")
		}
	}}}, []string{"SOKEL_FIRECRAWL_BASE_URL"})
	for _, status := range []string{"failed", "cancelled"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"success":true,"status":"`+status+`"}`)
		}))
		t.Setenv("SOKEL_FIRECRAWL_BASE_URL", s.URL)
		for _, op := range []string{"crawl_status", "extract_status"} {
			if _, e := invoke(context.Background(), "test-secret", op, `{"id":"job-1"}`); e == nil {
				t.Fatal("failed job reported successful")
			}
		}
		s.Close()
	}
	v, e := invoke(context.Background(), "", "health_check", `{}`)
	if e != nil || v.(*HealthCheckOut).OK {
		t.Fatal("missing credentials health falsely succeeded")
	}
}
