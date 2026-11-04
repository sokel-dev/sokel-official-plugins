package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/jira/schema"
	"net/http"
	"testing"
)

func TestJiraValidationAndIncompletePagination(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid operation reached network") })
	ctx := context.Background()
	for _, id := range []string{"../other", "DEMO-0", "DEMO-1/comment", ""} {
		if _, e := a.getIssue(ctx, &GetIssueIn{IssueKey: id}); e == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	for _, c := range []schema.IssueChanges{{Summary: str(" ")}, {DueDate: str("2025-02-29")}, {PriorityID: str("high")}, {Labels: []string{"two words"}}} {
		if _, e := issueChanges(c, nil); e == nil {
			t.Fatal("invalid fields accepted")
		}
	}
	if _, e := issueChanges(schema.IssueChanges{}, map[string]any{"summary": "bypass"}); e == nil {
		t.Fatal("reserved custom field accepted")
	}
	if _, e := a.updateIssue(ctx, &UpdateIssueIn{IssueKey: "DEMO-1"}); e == nil {
		t.Fatal("empty update")
	}
	if _, e := a.comments(ctx, &ListCommentsIn{IssueKey: "DEMO-1", StartAt: -1}); e == nil {
		t.Fatal("negative page")
	}
	if _, e := a.search(ctx, &SearchIssuesIn{Jql: "project=DEMO", Limit: 101}); e == nil {
		t.Fatal("invalid limit")
	}
	if _, e := a.addComment(ctx, &AddCommentIn{IssueKey: "DEMO-1", Text: " "}); e == nil {
		t.Fatal("blank comment")
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, `{"isLast":false,"issues":[]}`) })
	if _, e := a.search(ctx, &SearchIssuesIn{Jql: "project=DEMO"}); e == nil {
		t.Fatal("missing token")
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, `{"startAt":0,"total":2,"comments":[]}`) })
	if _, e := a.comments(ctx, &ListCommentsIn{IssueKey: "DEMO-1"}); e == nil {
		t.Fatal("stalled pagination")
	}
}
func TestADFAndClearSemantics(t *testing.T) {
	m, e := issueChanges(schema.IssueChanges{Description: str(""), AssigneeAccountID: str(""), Labels: []string{}, DueDate: str("")}, map[string]any{"customfield_10001": nil})
	if e != nil || len(m) != 5 || m["description"] != nil || m["duedate"] != nil || m["assignee"].(map[string]any)["accountId"] != nil || len(m["labels"].([]string)) != 0 {
		t.Fatalf("%#v %v", m, e)
	}
	d := adf("a\r\n\nb")
	p := d["content"].([]any)
	if len(p) != 3 || len(p[1].(map[string]any)["content"].([]any)) != 0 {
		t.Fatal(d)
	}
}

func TestMissingCollectionIsNotEmptySuccess(t *testing.T) {
	ctx := context.Background()
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if _, e := a.search(ctx, &SearchIssuesIn{Jql: "project=DEMO"}); e == nil {
		t.Fatal("missing collection accepted")
	}
	if _, e := a.comments(ctx, &ListCommentsIn{IssueKey: "DEMO-1"}); e == nil {
		t.Fatal("missing collection accepted")
	}
	if _, e := a.transitions(ctx, &ListTransitionsIn{IssueKey: "DEMO-1"}); e == nil {
		t.Fatal("missing collection accepted")
	}
}
