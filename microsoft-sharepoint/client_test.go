package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, fn http.HandlerFunc) *client {
	t.Helper()
	s := httptest.NewServer(fn)
	t.Cleanup(s.Close)
	target, _ := url.Parse(s.URL)
	transport := http.DefaultTransport
	c := newClient(Cred{AccessToken: "test-access-token"})
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "graph.microsoft.com" {
			t.Error("Graph origin changed")
		}
		r = r.Clone(r.Context())
		u := *r.URL
		r.URL = &u
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		return transport.RoundTrip(r)
	})}
	return c
}
func reply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, s)
}

var _ = strings.Contains

func TestUpdateFieldBytesAndETag(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "PATCH" || r.URL.Path != "/v1.0/sites/contoso.sharepoint.com,site,web/lists/list/items/1/fields" || r.Header.Get("If-Match") != `"v1"` || string(b) != `{"Count":0,"Title":"你好"}` {
			t.Errorf("request %s %s %s", r.Method, r.URL.String(), b)
		}
		reply(w, `{"Count":0,"Title":"你好"}`)
	})
	out, e := c.updateFields(context.Background(), &UpdateFieldsIn{SiteID: "contoso.sharepoint.com,site,web", ListID: "list", ItemID: "1", IfMatch: `"v1"`, Fields: map[string]any{"Title": "你好", "Count": 0}})
	if e != nil {
		t.Fatal(e)
	}
	if out.ItemID != "1" || out.Fields["Title"] != "你好" {
		t.Fatalf("output %+v", out)
	}
}
