package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These synthetic protocol fixtures follow the linked Google API docs; they are not live captures.
type handlerTransport struct{ handler http.HandlerFunc }

func (r handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	r.handler.ServeHTTP(w, req)
	resp := w.Result()
	resp.Request = req
	return resp, nil
}
func fakeAPI(t *testing.T, h http.HandlerFunc) *api {
	t.Helper()
	a := newAPI("test-secret")
	a.client = &http.Client{Transport: handlerTransport{h}}
	return a
}
func reply(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}
func TestRequestAuthAndNoLeak(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing bearer")
		}
		w.WriteHeader(401)
		reply(w, `{"error":{"message":"test-secret"}}`)
	})
	_, _, err := a.request(context.Background(), "GET", "https://www.googleapis.com", "/probe", nil, nil, "", 1024)
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
	e, ok := err.(*apiError)
	if !ok || e.Status != 401 {
		t.Fatalf("expected typed 401, got %v", err)
	}
}
func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	hits := 0
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "www.googleapis.com" {
			hits++
			w.WriteHeader(200)
			return
		}
		http.Redirect(w, r, "https://untrusted.example/steal", 302)
	})
	_, _, err := a.request(context.Background(), "GET", "https://www.googleapis.com", "/probe", nil, nil, "", 1024)
	if err == nil || hits != 0 {
		t.Fatalf("redirect was followed or accepted: %v %d", err, hits)
	}
}
func TestMissingTokenAndBoundedBody(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `123456789`) })
	_, _, err := a.request(context.Background(), "GET", "https://www.googleapis.com", "/probe", nil, nil, "", 4)
	if err == nil {
		t.Fatal("oversized body accepted")
	}
	a.token = ""
	_, _, err = a.request(context.Background(), "GET", "https://www.googleapis.com", "/probe", nil, nil, "", 100)
	if err == nil {
		t.Fatal("missing token accepted")
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("test-secret in unsafe transport error")
}
func TestAPIErrorBoundaries(t *testing.T) {
	a := newAPI("test-secret")
	a.client = &http.Client{Transport: failingTransport{}}
	_, _, e := a.request(context.Background(), "GET", "https://www.googleapis.com", "/x", nil, nil, "", 100)
	if e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatalf("transport error leaked: %v", e)
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e = a.request(c, "GET", "https://www.googleapis.com", "/x", nil, nil, "", 100)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel %v", e)
	}
	for _, test := range []struct{ origin, path, method string }{{"https://evil.example", "/x", "GET"}, {"https://www.googleapis.com", "//evil.example", "GET"}, {"https://www.googleapis.com", "/x?secret=y", "GET"}, {"https://www.googleapis.com", "/x", "bad\nmethod"}} {
		if _, _, e = a.request(context.Background(), test.method, test.origin, test.path, nil, nil, "", 100); e == nil {
			t.Fatal("invalid request accepted")
		}
	}
	for _, status := range []int{401, 403, 404, 410, 429, 500} {
		if (&apiError{Status: status}).Error() == "" {
			t.Fatal("missing status explanation")
		}
	}
}
func TestJSONAndHealthFailures(t *testing.T) {
	body := `{"email":"test@example.com"}`
	status := 200
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); reply(w, body) })
	h, e := a.health(context.Background())
	if e != nil || !h.OK || !strings.Contains(h.Message, "test@example.com") {
		t.Fatalf("health %v %v", h, e)
	}
	body = `{}`
	h, e = a.health(context.Background())
	if e != nil || h.OK {
		t.Fatal("missing identity accepted")
	}
	status = 403
	h, e = a.health(context.Background())
	if e != nil || h.OK {
		t.Fatal("health must report ok=false")
	}
	status = 200
	body = `not-json`
	var out map[string]any
	if e = a.json(context.Background(), "GET", "https://www.googleapis.com", "/x", nil, nil, &out); e == nil {
		t.Fatal("malformed JSON accepted")
	}
	if e = a.json(context.Background(), "POST", "https://www.googleapis.com", "/x", nil, map[string]any{"bad": make(chan int)}, &out); e == nil {
		t.Fatal("non JSON body accepted")
	}
	if _, e = segment("..", "id"); e == nil {
		t.Fatal("dot path accepted")
	}
	if _, e = segment("", "id"); e == nil {
		t.Fatal("missing id accepted")
	}
}
