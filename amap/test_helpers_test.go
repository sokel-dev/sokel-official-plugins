package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(t *testing.T, fn func(*http.Request) string) *api {
	t.Helper()
	a := newAPI(Cred{APIKey: "test-secret"})
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("key") != "test-secret" {
			t.Fatal("missing API key")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fn(r))), Request: r}, nil
	})
	return a
}
func ptr[T any](v T) *T { return &v }
