package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/mailchimp/schema"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, fn func(*http.Request) (int, string)) *client {
	t.Helper()
	c := newClient(&Cred{APIKey: "fixture-secret-us6"})
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		code, body := fn(r)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"17"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return c
}
func file(t *testing.T, n string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/" + n + ".official.json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func ptr[T any](v T) *T { return &v }
func TestOfficialPing(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "sokel" || p != "fixture-secret-us6" || r.URL.Host != "us6.api.mailchimp.com" || r.URL.Path != "/3.0/ping" {
			t.Fatal("invalid authentication or URL")
		}
		return 200, file(t, "ping")
	})
	o, e := c.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || o == nil || !o.OK {
		t.Fatal(o, e)
	}
}
func TestUpsertExplicitConsentWire(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || r.URL.Path != "/3.0/lists/list1/members/2b9150605ac374d671a306b5fcee60a0" {
			t.Fatal(r.Method, r.URL)
		}
		if !strings.Contains(string(b), `"status_if_new":"pending"`) || !strings.Contains(string(b), `"vip":false`) || strings.Contains(string(b), `"status":`) {
			t.Fatal(string(b))
		}
		return 200, `{"id":"2b9150605ac374d671a306b5fcee60a0","email_address":"ada.lovelace@example.com","status":"pending"}`
	})
	o, e := c.upsert_member(context.Background(), &UpsertMemberIn{ListID: "list1", EmailAddress: "Ada.Lovelace@Example.com", StatusIfNew: "pending", Options: schema.UpsertMemberOptions{Vip: ptr(false)}})
	if e != nil || o == nil || o.Member.Status != "pending" {
		t.Fatal(o, e)
	}
}
func TestOfficialTags(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("offset") != "0" || !strings.HasSuffix(r.URL.Path, "/tags") {
			t.Fatal(r.URL)
		}
		return 200, file(t, "tags")
	})
	o, e := c.list_member_tags(context.Background(), &ListMemberTagsIn{ListID: "list1", EmailAddress: "user@example.com", Page: schema.PageOptions{Offset: ptr(int64(0))}})
	if e != nil || o == nil || len(o.Tags) != 2 || o.Tags[0].ID != "10297" || o.HasMore {
		t.Fatal(o, e)
	}
}
