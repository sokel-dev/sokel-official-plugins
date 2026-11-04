package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func ptr[T any](v T) *T                                                   { return &v }

// Fixtures below are synthetic examples based on the official reference shapes.
// They are not captured from a live account. The transport checks the actual request.
func fixture(t *testing.T, c *client, method, path string, query url.Values, body string, status int, reply string) {
	t.Helper()
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Helper()
		if r.Method != method || r.URL.Scheme != "https" || r.URL.Host != "api.typeform.com" || r.URL.Path != path {
			t.Fatalf("request %s %s", r.Method, r.URL)
		}
		if r.URL.Query().Encode() != query.Encode() {
			t.Fatalf("query %s want %s", r.URL.RawQuery, query.Encode())
		}
		if r.Header.Get("Authorization") != "Bearer token-test" {
			t.Fatal("missing header authorization")
		}
		if r.URL.Query().Get("token") != "" || r.URL.Query().Get("key") != "" || r.URL.Query().Get("access_token") != "" {
			t.Fatal("URL credential leak")
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != body {
			t.Fatalf("body %s want %s", b, body)
		}
		if body != "" && r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON Content-Type")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})}
}
func bind(t *testing.T, body string, out any) {
	t.Helper()
	if e := contract.BindInput(json.RawMessage(body), out); e != nil {
		t.Fatal(e)
	}
}

func fresh() *client { return newClient(Cred{AccessToken: "token-test"}) }

const workspaceJSON = `{"id":"abc123","name":"Sales","account_id":"acct1","shared":false}`

func TestDocumentedWireRequests(t *testing.T) {
	ctx := context.Background()
	t.Run("health", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/me", nil, "", 200, `{"alias":"demo","email":"demo@example.com","language":"en"}`)
		o, e := c.health_check(ctx, &HealthCheckIn{})
		if e != nil || o == nil || !o.OK {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("workspaces", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/workspaces", url.Values{"page": {"1"}, "page_size": {"100"}}, "", 200, `{"items":[`+workspaceJSON+`],"total_items":1,"page_count":1}`)
		o, e := c.list_workspaces(ctx, &ListWorkspacesIn{})
		if e != nil || o == nil || len(o.Workspaces) != 1 || o.NextPage != 0 {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("workspace", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/workspaces/abc123", nil, "", 200, workspaceJSON)
		o, e := c.get_workspace(ctx, &GetWorkspaceIn{WorkspaceID: "abc123"})
		if e != nil || o == nil || o.Workspace.ID != "abc123" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("forms", func(t *testing.T) {
		c := fresh()
		var in ListFormsIn
		bind(t, `{"workspace_id":"abc123","search":"Lead","page":{"page":2,"page_size":20}}`, &in)
		fixture(t, c, "GET", "/forms", url.Values{"workspace_id": {"abc123"}, "search": {"Lead"}, "page": {"2"}, "page_size": {"20"}}, "", 200, `{"items":[{"id":"form123","title":"Lead","_links":{"display":"https://demo.typeform.com/to/form123"}}],"total_items":41,"page_count":3}`)
		o, e := c.list_forms(ctx, &in)
		if e != nil || o == nil || len(o.Forms) != 1 || o.NextPage != 3 || o.Forms[0].DisplayURL == nil {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("form", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/forms/form123", nil, "", 200, `{"id":"form123","title":"Lead","fields":[{"id":"field1","type":"yes_no","title":"Done?","validations":{"required":true}}],"hidden":["campaign"]}`)
		o, e := c.get_form(ctx, &GetFormIn{FormID: "form123"})
		if e != nil || o == nil || len(o.Form.Questions) != 1 || o.Form.Questions[0].Type != "yes_no" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("responses false and cursor", func(t *testing.T) {
		c := fresh()
		var in ListResponsesIn
		bind(t, `{"form_id":"form123","options":{"page_size":1,"before":"cursor-new"}}`, &in)
		fixture(t, c, "GET", "/forms/form123/responses", url.Values{"page_size": {"1"}, "before": {"cursor-new"}, "response_type": {"completed"}}, "", 200, `{"items":[{"token":"cursor-old","response_id":"resp1","landed_at":"2026-11-01T01:00:00Z","submitted_at":"2026-11-01T01:01:00Z","answers":[{"field":{"id":"field1","type":"yes_no","ref":"done"},"type":"boolean","boolean":false}],"hidden":{"campaign":"fall"},"calculated":{"score":0}}],"total_items":2,"page_count":2}`)
		o, e := c.list_responses(ctx, &in)
		if e != nil || o == nil || len(o.Responses) != 1 || o.Responses[0].Answers[0].Value != false || o.NextBefore != "cursor-old" || !o.MayHaveMore {
			t.Fatalf("%+v %v", o, e)
		}
	})
}
