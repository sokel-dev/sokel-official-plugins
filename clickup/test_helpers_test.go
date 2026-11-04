package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(t *testing.T, fn func(*http.Request) string) *api {
	t.Helper()
	a := newAPI("test-secret")
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "test-secret" {
			t.Fatal("incorrect authorization")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fn(r))), Request: r}, nil
	})
	return a
}
func bodyOf(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var b map[string]any
	if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
		t.Fatal(e)
	}
	return b
}
func ptr[T any](v T) *T { return &v }
