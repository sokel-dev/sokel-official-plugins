package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type errorTransport struct{ err error }

func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }
func TestHTTPFailuresAreSanitizedAndBounded(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 404, 409, 422, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "https://attacker.invalid/steal")
				w.WriteHeader(status)
				reply(w, `{"error":"test-secret"}`)
			})
			_, e := a.request(context.Background(), "GET", "/safe", nil, nil, nil)
			if e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
				t.Fatalf("error=%v calls=%d", e, calls)
			}
		})
	}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, `{not JSON test-secret`) })
	var out map[string]any
	if _, e := a.request(context.Background(), "GET", "/safe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
	a.client.Transport = errorTransport{errors.New("test-secret https://private.invalid")}
	if _, e := a.request(context.Background(), "GET", "/safe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, strings.Repeat("x", responseLimit+1)) })
	if _, e := a.request(context.Background(), "GET", "/safe", nil, nil, &out); e == nil || !strings.Contains(e.Error(), "8 MiB") {
		t.Fatal(e)
	}
	calls := 0
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, path := range []string{"//attacker.invalid/x", "https://attacker.invalid/x", "/x?q=secret", "/x#fragment", "/\r\nx"} {
		if _, e := a.request(context.Background(), "GET", path, nil, nil, nil); e == nil {
			t.Fatal("accepted path", path)
		}
	}
	if _, e := a.request(context.Background(), "POST", "/safe", nil, map[string]any{"x": strings.Repeat("x", requestLimit)}, nil); e == nil {
		t.Fatal("large request accepted")
	}
	if calls != 0 {
		t.Fatal("invalid request reached transport")
	}
}
func TestCancellationAndHealthFailure(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); reply(w, "test-secret") })
	o, e := a.health(context.Background(), &HealthCheckIn{})
	if e != nil || o.OK || o.Message == "" || strings.Contains(o.Message, "test-secret") {
		t.Fatalf("health %v %v", o, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.client.Transport = errorTransport{context.Canceled}
	if _, e := a.request(ctx, "GET", "/safe", nil, nil, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 56*time.Second {
			t.Fatal("HTTP timeout not set")
		}
		reply(w, "{}")
	})
	if _, e := a.request(context.Background(), "GET", "/safe", nil, nil, nil); e != nil {
		t.Fatal(e)
	}
}

func TestCloudCredentialBoundary(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid origin reached network") })
	for _, site := range []string{"http://demo.atlassian.net", "https://demo.atlassian.net.evil.invalid", "https://test-secret@demo.atlassian.net", "https://demo.atlassian.net:8443", "https://demo.atlassian.net/path", "https://demo.atlassian.net?secret=x", "https://localhost", "https://127.0.0.1", "https://demo.atlassian.net#x"} {
		a.cred.Site = site
		if _, e := a.base(); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("site %s error %v", site, e)
		}
	}
	a.cred.Site = "demo.atlassian.net"
	a.cred.CloudID = "bad"
	if _, e := a.base(); e == nil {
		t.Fatal("invalid Cloud ID accepted")
	}
	a.cred.CloudID = "12345678-1234-1234-1234-123456789abc"
	got, e := a.base()
	if e != nil || got != "https://api.atlassian.com/ex/confluence/"+a.cred.CloudID {
		t.Fatalf("gateway %s %v", got, e)
	}
	a.client.Transport = handlerTransport{func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "test@example.com" || p != "test-secret" || r.URL.Host != "api.atlassian.com" || r.URL.Path != "/ex/confluence/"+a.cred.CloudID+"/safe" {
			t.Fatal("bad scoped credential request")
		}
		reply(w, "{}")
	}}
	if _, e := a.request(context.Background(), "GET", "/safe", nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	a.cred.Email = "Other <test@example.com>"
	if _, e := a.base(); e == nil {
		t.Fatal("display email accepted")
	}
	a.cred.Email = "test@example.com"
	a.cred.APIToken = "bad\nsecret"
	if _, e := a.base(); e == nil {
		t.Fatal("multiline token accepted")
	}
}
