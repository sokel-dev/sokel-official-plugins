package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Synthetic protocol fixtures derived from the linked official API documentation; not live captures.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(t *testing.T, f func(*http.Request) (int, string)) *api {
	t.Helper()
	a := newAPI(testCred())
	a.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		code, body := f(r)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return a
}
func bodyOf(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func testCred() Cred {
	return Cred{Subdomain: "example", Email: "agent@example.com", APIToken: "test-secret"}
}
func TestProtocol(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		calls++
		u, p, ok := r.BasicAuth()
		if r.URL.Host != "example.zendesk.com" || !ok || u != "agent@example.com/token" || p != "test-secret" {
			t.Fatal("invalid auth/origin")
		}
		if r.URL.Path == "/api/v2/users/me.json" {
			return 200, `{"user":{"id":123,"role":"agent"}}`
		}
		if r.URL.Path != "/api/v2/tickets/123.json" || r.Method != "PUT" {
			t.Fatal(r.URL, r.Method)
		}
		b := bodyOf(t, r)["ticket"].(map[string]any)["comment"].(map[string]any)
		if b["public"] != false || b["body"] != "Private note" {
			t.Fatal(b)
		}
		return 200, `{"ticket":{"id":123,"subject":"Help","status":"open","requester_id":234}}`
	})
	h, e := a.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || h == nil || !h.OK || calls != 1 {
		t.Fatalf("health: %#v %v calls=%d", h, e, calls)
	}
	v, e := a.add_comment(context.Background(), &AddCommentIn{TicketID: "123", Body: "Private note", Public: false})
	if e != nil || v == nil || v.Ticket.ID != "123" {
		t.Fatalf("comment: %#v %v", v, e)
	}
}
