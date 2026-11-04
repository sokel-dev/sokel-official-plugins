package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/linear/schema"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic protocol examples from official API documentation, not live account captures.
type handlerTransport struct{ h http.HandlerFunc }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.h.ServeHTTP(w, r)
	res := w.Result()
	res.Request = r
	return res, nil
}
func fakeAPI(h http.HandlerFunc) *api {
	a := newAPI(Cred{APIKey: "test-secret"})
	a.client = &http.Client{Transport: handlerTransport{h}}
	return a
}
func reply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s)
}
func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func str(v string) *string { return &v }
func TestHealthChecksReadOnly(t *testing.T) {
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || !strings.Contains(body(t, r)["query"].(string), "viewer") {
			t.Fatal("not a viewer query")
		}
		reply(w, `{"data":{"viewer":{"id":"u","name":"Tester"}}}`)
	})
	out, e := a.health(context.Background(), &HealthCheckIn{})
	if e != nil || out == nil || !out.OK || calls != 1 {
		t.Fatalf("health %v %v calls=%d", out, e, calls)
	}
}
func TestGraphQLQueriesAndMutations(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "https://api.linear.app/graphql" || r.Header.Get("Authorization") != "test-secret" {
			t.Fatal("wrong endpoint or personal-key auth")
		}
		b := body(t, r)
		q := b["query"].(string)
		v := b["variables"].(map[string]any)
		switch {
		case strings.Contains(q, "issueCreate"):
			input := v["input"].(map[string]any)
			if input["teamId"] != "t" || input["title"] != "Quote \"title\"" {
				t.Fatalf("input %v", input)
			}
			reply(w, `{"data":{"issueCreate":{"success":true,"issue":{"id":"i","identifier":"T-1","title":"Created"}}}}`)
		case strings.Contains(q, "issueUpdate"):
			input := v["input"].(map[string]any)
			if _, ok := input["description"]; !ok || input["description"] != "" {
				t.Fatal("empty patch description was lost")
			}
			if _, ok := input["title"]; ok {
				t.Fatal("omitted title was changed")
			}
			reply(w, `{"data":{"issueUpdate":{"success":true,"issue":{"id":"i","title":"Updated"}}}}`)
		case strings.Contains(q, "issues("):
			if v["after"] != "cursor&2" || v["first"] != float64(3) {
				t.Fatal(v)
			}
			reply(w, `{"data":{"issues":{"nodes":[{"id":"i","identifier":"T-1","team":{"id":"t","key":"T"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}`)
		case strings.Contains(q, "projects("):
			reply(w, `{"data":{"projects":{"nodes":[{"id":"p","name":"Project"}],"pageInfo":{"hasNextPage":false}}}}`)
		case strings.Contains(q, "teams("):
			reply(w, `{"data":{"teams":{"nodes":[{"id":"t","key":"T","name":"Team"}],"pageInfo":{"hasNextPage":false}}}}`)
		default:
			reply(w, `{"data":{"issue":{"id":"i","identifier":"T-1"}}}`)
		}
	})
	ctx := context.Background()
	if o, e := a.issues(ctx, &ListIssuesIn{Limit: 3, Cursor: "cursor&2", TeamID: "t"}); e != nil || o == nil || !o.HasMore || o.NextCursor != "next" || len(o.Issues) != 1 {
		t.Fatalf("issues %v %v", o, e)
	}
	if o, e := a.teams(ctx, &ListTeamsIn{}); e != nil || o == nil || len(o.Teams) != 1 {
		t.Fatalf("teams %v %v", o, e)
	}
	if o, e := a.projects(ctx, &ListProjectsIn{}); e != nil || o == nil || len(o.Projects) != 1 {
		t.Fatalf("projects %v %v", o, e)
	}
	if o, e := a.getIssue(ctx, &GetIssueIn{IssueID: "T-1"}); e != nil || o == nil || o.Issue.ID != "i" {
		t.Fatalf("get %v %v", o, e)
	}
	if o, e := a.createIssue(ctx, &CreateIssueIn{TeamID: "t", Title: "Quote \"title\""}); e != nil || o == nil || o.Issue.ID != "i" {
		t.Fatalf("create %v %v", o, e)
	}
	if o, e := a.updateIssue(ctx, &UpdateIssueIn{IssueID: "T-1", Changes: schema.IssueChanges{Description: str("")}}); e != nil || o == nil || o.Issue.ID != "i" {
		t.Fatalf("update %v %v", o, e)
	}
}
