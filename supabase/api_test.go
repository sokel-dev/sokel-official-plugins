package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAuthenticationModes(t *testing.T) {
	for _, tc := range []struct{ key, token, want string }{{"sb_publishable_key", "", ""}, {"sb_secret_key", "", ""}, {"legacy.jwt.value", "", "Bearer legacy.jwt.value"}, {"sb_publishable_key", "user.jwt.value", "Bearer user.jwt.value"}} {
		a := fakeAPI(func(r *http.Request) (int, string) {
			if r.Header.Get("Authorization") != tc.want || r.Header.Get("apikey") != tc.key {
				t.Fatal(r.Header)
			}
			return 200, "[]"
		})
		a.key = tc.key
		a.token = tc.token
		if _, _, e := a.request(context.Background(), "GET", "/rest/v1/items", "public", nil, nil, ""); e != nil {
			t.Fatal(e)
		}
	}
}
func TestOriginsPathsAndMissingKey(t *testing.T) {
	for _, base := range []string{"", "http://example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?token=x", "https://example.com#x"} {
		if _, e := origin(base); e == nil {
			t.Fatal("accepted", base)
		}
	}
	for _, base := range []string{"https://example.com/", "http://localhost:54321", "http://127.0.0.1:54321", "http://[::1]:54321"} {
		if _, e := origin(base); e != nil {
			t.Fatal(base, e)
		}
	}
	a := fakeAPI(func(*http.Request) (int, string) { t.Fatal("unexpected request"); return 200, "[]" })
	for _, tc := range []struct{ path, schema, method string }{{"https://other.com", "", "GET"}, {"/rest/v1/x?foo=bar", "", "GET"}, {"/rest/v1/x", "a\nb", "GET"}, {"/rest/v1/x", "", "bad\nmethod"}} {
		if _, _, e := a.request(context.Background(), tc.method, tc.path, tc.schema, nil, nil, ""); e == nil {
			t.Fatal("invalid request accepted")
		}
	}
	a.key = ""
	if _, _, e := a.request(context.Background(), "GET", "/rest/v1/x", "", nil, nil, ""); e == nil {
		t.Fatal("missing key accepted")
	}
}
func TestErrorsNoRetriesAndNoRedirect(t *testing.T) {
	for _, status := range []int{302, 401, 403, 404, 409, 429, 500} {
		calls := 0
		a := fakeAPI(func(*http.Request) (int, string) { calls++; return status, `{"error":"sb_secret_test"}` })
		a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.example/steal"}}, Body: io.NopCloser(strings.NewReader("sb_secret_test")), Request: r}, nil
		})
		_, _, e := a.request(context.Background(), "POST", "/rest/v1/items", "public", nil, map[string]any{"x": 1}, "")
		if e == nil || strings.Contains(e.Error(), "sb_secret_test") || calls != 1 {
			t.Fatal(status, calls, e)
		}
	}
	a := newAPI("https://example.com", "test", "")
	a.client.Transport = transport(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("unsafe-secret") })
	_, _, e := a.request(context.Background(), "GET", "/rest/v1/x", "", nil, nil, "")
	if e == nil || strings.Contains(e.Error(), "unsafe-secret") {
		t.Fatal(e)
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e = a.request(c, "GET", "/rest/v1/x", "", nil, nil, "")
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestMalformedOversizedAndHealth(t *testing.T) {
	a := fakeAPI(func(*http.Request) (int, string) { return 200, "not-json" })
	if _, e := a.selectRows(context.Background(), &SelectRowsIn{Table: "items"}); e == nil {
		t.Fatal("bad JSON accepted")
	}
	if _, _, e := a.request(context.Background(), "POST", "/rest/v1/x", "", nil, make(chan int), ""); e == nil {
		t.Fatal("non-JSON body accepted")
	}
	a = fakeAPI(func(*http.Request) (int, string) { return 200, strings.Repeat("x", int(jsonLimit)+1) })
	if _, _, e := a.request(context.Background(), "GET", "/rest/v1/x", "", nil, nil, ""); e == nil {
		t.Fatal("oversized response accepted")
	}
	a = fakeAPI(func(r *http.Request) (int, string) {
		if r.Method != "HEAD" || r.URL.Query().Get("limit") != "1" {
			t.Fatal(r.Method, r.URL)
		}
		return 200, ""
	})
	r, e := a.health(context.Background(), &HealthCheckIn{Table: "items"})
	if e != nil || !r.OK {
		t.Fatal(r, e)
	}
	r, e = a.health(context.Background(), &HealthCheckIn{})
	if e != nil || r.OK {
		t.Fatal(r, e)
	}
	a.key = ""
	r, e = a.health(context.Background(), &HealthCheckIn{Table: "items"})
	if e != nil || r.OK {
		t.Fatal(r, e)
	}
}
func TestPaginationAndFilterValidation(t *testing.T) {
	for _, tc := range [][2]int{{-1, 0}, {1001, 0}, {1, -1}, {1, 1000000001}} {
		if _, e := page(tc[0], tc[1]); e == nil {
			t.Fatal(tc)
		}
	}
	q, e := filterQuery([]RowFilter{{Column: "id", Operator: "gte", Value: "1"}, {Column: "id", Operator: "lt", Value: "10"}}, false)
	if e != nil || len(q["id"]) != 2 {
		t.Fatal(q, e)
	}
	if _, e = filterQuery([]RowFilter{{Column: "id", Operator: "unknown", Value: "x"}}, false); e == nil {
		t.Fatal("bad operator")
	}
	for _, raw := range []string{"null", "{}", "[null]", "[1]"} {
		if _, e := parseRows([]byte(raw)); e == nil {
			t.Fatal("bad rows", raw)
		}
	}
	a := fakeAPI(func(*http.Request) (int, string) { return 200, `[]` })
	out, e := a.selectRows(context.Background(), &SelectRowsIn{Table: "items", Offset: 10})
	if e != nil || out.NextOffset != 10 || out.Count != 0 {
		t.Fatal(out, e)
	}
	if _, _, e = a.request(context.Background(), "GET", "/rest/v1/items", "public", url.Values{"name": {"eq.x&limit=999"}}, nil, ""); e != nil {
		t.Fatal(e)
	}
}
func TestRPCShapesAndSignedURLTrust(t *testing.T) {
	for _, body := range []string{`null`, `true`, `42`, `[1,2]`, `{"ok":true}`, `"text"`, ``} {
		a := fakeAPI(func(*http.Request) (int, string) { return 200, body })
		r, e := a.callFunction(context.Background(), &CallFunctionIn{Function: "f"})
		if e != nil {
			t.Fatal(e)
		}
		if body == "" {
			body = "null"
		}
		if r.ResultJSON != body {
			t.Fatal(r)
		}
	}
	for _, body := range []string{`{"signedURL":"https://evil.example/object/sign/a?token=x"}`, `{"signedURL":"//evil.example/object/sign/a?token=x"}`, `{"signedURL":"/object/sign/a"}`, `{}`} {
		a := fakeAPI(func(*http.Request) (int, string) { return 200, body })
		if _, e := a.signedURL(context.Background(), &CreateSignedURLIn{Bucket: "b", Path: "x"}); e == nil {
			t.Fatal("untrusted URL accepted", body)
		}
	}
}

func TestLargeDatabaseIntegersPreserveWireDigits(t *testing.T) {
	rows, e := parseRows([]byte(`[{"id":9007199254740993}]`))
	if e != nil {
		t.Fatal(e)
	}
	n, ok := rows[0].(map[string]any)["id"].(json.Number)
	if !ok || n.String() != "9007199254740993" {
		t.Fatal(rows)
	}
	raw, e := json.Marshal(rows)
	if e != nil || string(raw) != `[{"id":9007199254740993}]` {
		t.Fatal(string(raw), e)
	}
}
