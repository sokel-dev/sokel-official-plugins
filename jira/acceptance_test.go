package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/jira/schema"
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
	a := newAPI(Cred{Site: "https://demo.atlassian.net", Email: "test@example.com", APIToken: "test-secret"})
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
		if r.Method != "GET" || r.URL.Path != "/rest/api/3/myself" {
			t.Fatal("health is not identity GET")
		}
		reply(w, `{"accountId":"u","displayName":"Tester","type":"known"}`)
	})
	out, e := a.health(context.Background(), &HealthCheckIn{})
	if e != nil || out == nil || !out.OK || calls != 1 {
		t.Fatalf("health %v %v calls=%d", out, e, calls)
	}
}
func TestCloudIssueWireContract(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "test@example.com" || p != "test-secret" || r.URL.Host != "demo.atlassian.net" {
			t.Fatal("invalid basic auth or site")
		}
		switch {
		case r.URL.Path == "/rest/api/3/search/jql":
			b := body(t, r)
			if r.Method != "POST" || b["jql"] != "project = DEMO" || b["nextPageToken"] != "p&2" {
				t.Fatal(b)
			}
			reply(w, `{"isLast":false,"nextPageToken":"next","issues":[{"id":"1","key":"DEMO-1","fields":{"summary":"Title","status":{"id":"2","name":"Open"},"labels":["a"]}}]}`)
		case strings.HasSuffix(r.URL.Path, "/transitions"):
			if r.Method == "POST" {
				b := body(t, r)
				if b["transition"].(map[string]any)["id"] != "31" {
					t.Fatal(b)
				}
				w.WriteHeader(204)
			} else {
				reply(w, `{"transitions":[{"id":"31","name":"Done","to":{"id":"3","name":"Done"},"fields":{"resolution":{"required":true}}}]}`)
			}
		case strings.HasSuffix(r.URL.Path, "/comment"):
			if r.Method == "POST" {
				b := body(t, r)
				if b["body"].(map[string]any)["type"] != "doc" {
					t.Fatal("missing ADF")
				}
				reply(w, `{"id":"c","body":{"type":"doc","version":1,"content":[]},"author":{"accountId":"u"}}`)
			} else {
				if r.URL.Query().Get("startAt") != "2" {
					t.Fatal("wrong offset")
				}
				reply(w, `{"startAt":2,"maxResults":1,"total":4,"comments":[{"id":"c","body":{"type":"doc"}}]}`)
			}
		case r.Method == "POST":
			b := body(t, r)
			f := b["fields"].(map[string]any)
			if f["project"].(map[string]any)["key"] != "DEMO" || f["customfield_10001"] != "x" {
				t.Fatal(f)
			}
			reply(w, `{"id":"1","key":"DEMO-1"}`)
		case r.Method == "PUT":
			b := body(t, r)
			f := b["fields"].(map[string]any)
			if _, ok := f["summary"]; ok {
				t.Fatal("absent summary sent")
			}
			if f["assignee"].(map[string]any)["accountId"] != nil {
				t.Fatal("empty assignee did not unassign")
			}
			w.WriteHeader(204)
		default:
			reply(w, `{"id":"1","key":"DEMO-1","fields":{"summary":"Title","description":{"type":"doc"}}}`)
		}
	})
	ctx := context.Background()
	if o, e := a.search(ctx, &SearchIssuesIn{Jql: "project = DEMO", NextPageToken: "p&2", Limit: 1}); e != nil || o == nil || o.NextPageToken != "next" || len(o.Issues) != 1 || o.Issues[0].Summary != "Title" {
		t.Fatalf("search %v %v", o, e)
	}
	if o, e := a.getIssue(ctx, &GetIssueIn{IssueKey: "DEMO-1"}); e != nil || o == nil || !strings.Contains(o.Issue.DescriptionADF, "doc") {
		t.Fatalf("get %v %v", o, e)
	}
	if o, e := a.createIssue(ctx, &CreateIssueIn{ProjectKey: "DEMO", IssueTypeID: "10001", Summary: "Title", CustomFields: map[string]any{"customfield_10001": "x"}}); e != nil || o == nil || o.Key != "DEMO-1" {
		t.Fatalf("create %v %v", o, e)
	}
	if o, e := a.updateIssue(ctx, &UpdateIssueIn{IssueKey: "DEMO-1", Changes: schema.IssueChanges{AssigneeAccountID: str("")}}); e != nil || o == nil || !o.OK {
		t.Fatalf("update %v %v", o, e)
	}
	if o, e := a.transitions(ctx, &ListTransitionsIn{IssueKey: "DEMO-1"}); e != nil || o == nil || len(o.Transitions) != 1 || !strings.Contains(o.Transitions[0].FieldsJSON, "resolution") {
		t.Fatalf("transitions %v %v", o, e)
	}
	if o, e := a.transition(ctx, &TransitionIssueIn{IssueKey: "DEMO-1", TransitionID: "31"}); e != nil || o == nil || !o.OK {
		t.Fatalf("transition %v %v", o, e)
	}
	if o, e := a.comments(ctx, &ListCommentsIn{IssueKey: "DEMO-1", StartAt: 2, Limit: 1}); e != nil || o == nil || o.NextStartAt != 3 || !o.HasMore {
		t.Fatalf("comments %v %v", o, e)
	}
	if o, e := a.addComment(ctx, &AddCommentIn{IssueKey: "DEMO-1", Text: "line1\nline2"}); e != nil || o == nil || o.Comment.ID != "c" {
		t.Fatalf("comment %v %v", o, e)
	}
}
