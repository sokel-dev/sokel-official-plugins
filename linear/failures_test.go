package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/linear/schema"
	"net/http"
	"strings"
	"testing"
)

func TestGraphQLErrorsAndMissingData(t *testing.T) {
	for _, raw := range []string{`{"errors":[{"message":"test-secret"}],"data":{"viewer":{"id":"x"}}}`, `{"data":null}`, `{"data":[]}`, `{"data":{}}`, `{"data":{"issues":null}}`, `{"data":{"issues":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`} {
		a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, raw) })
		_, e := a.issues(context.Background(), &ListIssuesIn{})
		if e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("accepted invalid response %s: %v", raw, e)
		}
	}
}
func TestOptionalMutationFieldsAndValidation(t *testing.T) {
	zero := 0
	m, e := issueChanges(schema.IssueChanges{Description: str(""), AssigneeID: str(""), ProjectID: str(""), DueDate: str(""), Priority: &zero})
	if e != nil || len(m) != 5 || m["priority"] != 0 || m["assigneeId"] != nil || m["description"] != "" {
		t.Fatalf("%#v %v", m, e)
	}
	bad := 5
	for _, c := range []schema.IssueChanges{{Title: str(" ")}, {StateID: str("")}, {Priority: &bad}, {DueDate: str("2026-02-30")}} {
		if _, e := issueChanges(c); e == nil {
			t.Fatal("invalid fields accepted")
		}
	}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid operation reached network") })
	if _, e := a.updateIssue(context.Background(), &UpdateIssueIn{IssueID: "ABC-1"}); e == nil {
		t.Fatal("empty update accepted")
	}
	if _, e := a.issues(context.Background(), &ListIssuesIn{Limit: 101}); e == nil {
		t.Fatal("invalid limit")
	}
	for _, raw := range []string{`{"data":{"issue":null}}`, `{"data":{"issueCreate":{"success":false}}}`, `{"data":{"issueUpdate":{"success":true,"issue":null}}}`} {
		a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, raw) })
		if _, e := a.getIssue(context.Background(), &GetIssueIn{IssueID: "ABC-1"}); e == nil {
			t.Fatal("missing issue")
		}
		if _, e := a.createIssue(context.Background(), &CreateIssueIn{TeamID: "t", Title: "a"}); e == nil {
			t.Fatal("unconfirmed create")
		}
		if _, e := a.updateIssue(context.Background(), &UpdateIssueIn{IssueID: "ABC-1", Changes: schema.IssueChanges{Priority: &zero}}); e == nil {
			t.Fatal("unconfirmed update")
		}
	}
}

func TestMissingCollectionIsNotEmptySuccess(t *testing.T) {
	ctx := context.Background()
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		reply(w, `{"data":{"issues":{"pageInfo":{}},"teams":{"pageInfo":{}},"projects":{"pageInfo":{}}}}`)
	})
	if _, e := a.issues(ctx, &ListIssuesIn{}); e == nil {
		t.Fatal("missing collection accepted")
	}
	if _, e := a.teams(ctx, &ListTeamsIn{}); e == nil {
		t.Fatal("missing collection accepted")
	}
	if _, e := a.projects(ctx, &ListProjectsIn{}); e == nil {
		t.Fatal("missing collection accepted")
	}
}
