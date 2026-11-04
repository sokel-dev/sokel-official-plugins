package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func fakeAPI(t *testing.T, method, path string, status int, body string, inspect func(*http.Request, []byte)) (*api, *int) {
	t.Helper()
	calls := 0
	a := newAPI(Cred{AccessToken: "test-secret-do-not-expose"})
	a.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "api.todoist.com" {
			t.Errorf("unexpected destination: %s", r.URL.Host)
		}
		if r.Method != method || r.URL.Path != path {
			t.Errorf("route: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret-do-not-expose" {
			t.Error("missing credential header")
		}
		b, _ := io.ReadAll(r.Body)
		if inspect != nil {
			inspect(r, b)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return a, &calls
}
func TestHealth(t *testing.T) {
	a, _ := fakeAPI(t, "GET", "/api/v1/user", 200, `{"id":"person"}`, nil)
	o, e := a.health(context.Background(), &HealthCheckIn{})
	if e != nil || o == nil || !o.OK {
		t.Fatalf("health: %+v %v", o, e)
	}
	a.cred.AccessToken = ""
	o, e = a.health(context.Background(), &HealthCheckIn{})
	if e != nil || o == nil || o.OK {
		t.Fatalf("invalid health: %+v %v", o, e)
	}
}
