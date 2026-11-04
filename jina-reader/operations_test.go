package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Mock examples follow https://docs.jina.ai/openapi.json, not a captured live account.
func TestDocumentedWireRequests(t *testing.T) {
	runCases(t, []wireCase{
		{"read", `{"url":" https://example.com/path?q=a#section ","target_selector":"main","remove_selector":"nav","options":{"no_cache":false}}`, "POST", "/", `{"url":"https://example.com/path?q=a"}`, `{"code":200,"status":20000,"data":{"url":"https://example.com/path?q=a","finalUrl":"https://example.com/article","title":"Example","description":"Article","content":"# Example","publishedTime":"2026-11-03","metadata":{"author":"Ada"}}}`, func(t *testing.T, v any) {
			o := v.(*ReadOut)
			if o.Content != "# Example" || o.FinalURL != "https://example.com/article" || o.Metadata["author"] != "Ada" {
				t.Fatal(o)
			}
		}},
		{"health_check", `{}`, "GET", "/v1/batches?limit=1", "", `[]`, func(t *testing.T, v any) {
			if !v.(*HealthCheckOut).OK {
				t.Fatal(v)
			}
		}},
	}, []string{"SOKEL_JINA_READER_BASE_URL", "SOKEL_JINA_API_BASE_URL"})
}
func TestReaderHeadersAndAnonymous(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, want := range map[string]string{"X-Respond-With": "text", "X-Target-Selector": "main", "X-Remove-Selector": "nav", "X-No-Cache": "true"} {
			if r.Header.Get(k) != want {
				t.Errorf("%s=%s", k, r.Header.Get(k))
			}
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("anonymous authorization sent")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":200,"status":20000,"data":{"url":"https://example.com","content":"text"}}`)
	}))
	defer s.Close()
	t.Setenv("SOKEL_JINA_READER_BASE_URL", s.URL)
	_, e := invoke(context.Background(), "", "read", `{"url":"https://example.com","format":"text","target_selector":"main","remove_selector":"nav","options":{"no_cache":true}}`)
	if e != nil {
		t.Fatal(e)
	}
	v, e := invoke(context.Background(), "", "health_check", `{}`)
	if e != nil || v.(*HealthCheckOut).OK {
		t.Fatal("anonymous credential falsely verified")
	}
}
func TestBoundaries(t *testing.T) {
	runBoundaries(t, "SOKEL_JINA_READER_BASE_URL", "read", `{"url":"https://example.com"}`, "")
}
func TestInvalidAndBusinessErrors(t *testing.T) {
	for _, in := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"https://user:secret@example.com"}`, `{"url":"https://example.com","format":"screenshot"}`} {
		_, e := invoke(context.Background(), "test-secret", "read", in)
		if e == nil {
			t.Fatal(in)
		}
	}
	for _, body := range []string{`{"code":401,"message":"test-secret"}`, `{}`, `{"code":200,"data":null}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_JINA_READER_BASE_URL", s.URL)
		_, e := invoke(context.Background(), "test-secret", "read", `{"url":"https://example.com"}`)
		if e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("bad error %v", e)
		}
		s.Close()
	}
}

func TestUnusablePagesAndCredentialHealth(t *testing.T) {
	for _, body := range []string{`{"code":200,"data":{"url":"https://example.com"}}`, `{"code":200,"data":{"url":"https://example.com","content":"Unavailable","httpStatus":403}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_JINA_READER_BASE_URL", s.URL)
		if _, e := invoke(context.Background(), "test-secret", "read", `{"url":"https://example.com"}`); e == nil {
			t.Fatal("unusable page accepted")
		}
		s.Close()
	}
	for _, tc := range []struct {
		body   string
		status int
	}{{`null`, 200}, {`test-secret`, 401}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			io.WriteString(w, tc.body)
		}))
		t.Setenv("SOKEL_JINA_API_BASE_URL", s.URL)
		v, e := invoke(context.Background(), "test-secret", "health_check", `{}`)
		if e != nil || v.(*HealthCheckOut).OK || strings.Contains(v.(*HealthCheckOut).Message, "test-secret") {
			t.Fatal("credential health falsely succeeded or exposed key")
		}
		s.Close()
	}
	if _, e := invoke(context.Background(), "test-secret", "read", `{"url":"https://example.com","target_selector":"main\r\nAuthorization: bad"}`); e == nil {
		t.Fatal("header injection accepted")
	}
}
