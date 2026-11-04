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

func TestCreateSessionFalseAndRangeHeader(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/v1.0/drives/drive/items/book/workbook/createSession" || string(b) != `{"persistChanges":false}` {
			t.Errorf("request %s %s %s", r.Method, r.URL.String(), b)
		}
		reply(w, `{"id":"session-key","persistChanges":false}`)
	})
	out, e := c.createSession(context.Background(), &CreateSessionIn{DriveID: "drive", WorkbookID: "book", PersistChanges: false})
	if e != nil {
		t.Fatal(e)
	}
	if out.SessionID != "session-key" || out.PersistChanges {
		t.Fatalf("output %+v", out)
	}
}
