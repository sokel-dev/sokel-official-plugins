package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/bocha-search/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Synthetic fixtures follow the official Web Search envelope; no live searches are performed.
func TestSearchWireAndProjection(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.Method != "POST" || r.URL.String() != "https://api.bocha.cn/v1/web-search" {
			t.Fatal(r.Method, r.URL)
		}
		b := bodyOf(t, r)
		if b["query"] != "中文 search" || b["summary"] != false || b["count"] != float64(2) || b["freshness"] != "oneWeek" {
			t.Fatal(b)
		}
		return `{"code":200,"log_id":"log","data":{"queryContext":{"originalQuery":"中文 search"},"webPages":{"totalEstimatedMatches":5,"value":[{"name":"Title","url":"https://example.com","siteName":"Example","datePublished":"2026-01-01","summary":"Text"}]}}}`
	})
	out, e := a.WebSearch(context.Background(), &WebSearchIn{Query: "中文 search", Freshness: "oneWeek", Options: schema.SearchOptions{Count: ptr(2), Summary: ptr(false)}})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Results) != 1 || out.Results[0].SiteName != "Example" || out.TotalEstimatedMatches != 5 || out.LogID != "log" {
		t.Fatal(out)
	}
}
func TestVerificationRequiresExplicitPaidProbe(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(r *http.Request) string {
		calls++
		b := bodyOf(t, r)
		if b["count"] != float64(1) || b["summary"] != false {
			t.Fatal(b)
		}
		return `{"code":200,"data":{"webPages":{"value":[]}}}`
	})
	out, e := a.VerifyCredential(context.Background(), &VerifyCredentialIn{})
	if e == nil || out != nil || calls != 0 {
		t.Fatal(out, e, calls)
	}
	out, e = a.VerifyCredential(context.Background(), &VerifyCredentialIn{ProbeQuery: "probe"})
	if e != nil || !out.OK || calls != 1 {
		t.Fatal(out, e, calls)
	}
}
func TestInvalidQueriesAndEnvelopes(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid query sent"); return "{}" })
	for _, in := range []WebSearchIn{{}, {Query: "q", Freshness: "2026-02-30"}, {Query: "q", Freshness: "2026-02-02..2026-01-01"}, {Query: "q", Options: schema.SearchOptions{Count: ptr(0)}}} {
		if _, e := a.WebSearch(context.Background(), &in); e == nil {
			t.Fatal(in)
		}
	}
	for _, raw := range []string{`{}`, `{"code":500,"message":"test-secret"}`, `{"code":200,"data":{"webPages":{"value":[{}]}}}`} {
		a = fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.WebSearch(context.Background(), &WebSearchIn{Query: "q"}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(e)
		}
	}
}
func TestHTTPFailuresNoRetryOrSecret(t *testing.T) {
	for _, status := range []int{301, 400, 401, 403, 429, 500} {
		a := newAPI("test-secret")
		calls := 0
		a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://evil.example"}}, Body: io.NopCloser(strings.NewReader(`{"message":"test-secret"}`)), Request: r}, nil
		})
		_, e := a.WebSearch(context.Background(), &WebSearchIn{Query: "q"})
		if e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
			t.Fatal(status, e, calls)
		}
	}
}

func TestExplicitSuccessWithoutWebPagesIsEmpty(t *testing.T) {
	for _, raw := range []string{`{"code":200,"data":{"webPages":null}}`, `{"code":200,"data":{}}`, `{"code":200,"data":null}`, `{"code":200,"data":{"webPages":{"value":null}}}`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		out, e := a.WebSearch(context.Background(), &WebSearchIn{Query: "no matching results"})
		if e != nil || out == nil || out.Results == nil || len(out.Results) != 0 || out.Query != "no matching results" {
			t.Fatal(raw, out, e)
		}
	}
}
