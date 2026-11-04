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

func TestListItemsCursorAndAuth(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1.0/drives/b!drive/root/children" || r.URL.Query().Get("$skiptoken") != "cursor+/=" || r.Header.Get("Authorization") != "Bearer test-access-token" {
			t.Errorf("request %s %s", r.Method, r.URL.String())
		}
		reply(w, `{"value":[{"id":"item1","name":"report.txt","size":3,"file":{"mimeType":"text/plain"}}]}`)
	})
	out, e := c.listItems(context.Background(), &ListItemsIn{DriveID: "b!drive", NextLink: "https://graph.microsoft.com/v1.0/drives/b!drive/root/children?$skiptoken=cursor%2B%2F%3D"})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Items) != 1 || out.Items[0].MimeType != "text/plain" {
		t.Fatalf("output %+v", out)
	}
}
