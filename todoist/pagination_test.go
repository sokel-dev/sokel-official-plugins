package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRejectRepeatedContinuationCursor(t *testing.T) {
	for _, resource := range []string{"projects", "tasks"} {
		t.Run(resource, func(t *testing.T) {
			a, n := fakeAPI(t, "GET", "/api/v1/"+resource, 200, `{"results":[],"next_cursor":"prior.cursor"}`, nil)
			var err error
			if resource == "projects" {
				_, err = a.listProjects(context.Background(), &ListProjectsIn{Cursor: "prior.cursor"})
			} else {
				_, err = a.listTasks(context.Background(), &ListTasksIn{Cursor: "prior.cursor"})
			}
			if err == nil || *n != 1 {
				t.Fatalf("repeated cursor accepted; error=%v requests=%d", err, *n)
			}
		})
	}
}

func TestFinalAndDistinctContinuationCursors(t *testing.T) {
	for _, resource := range []string{"projects", "tasks"} {
		for _, tc := range []struct{ prior, next string }{{"", ""}, {"prior.cursor", ""}, {"prior.cursor", "next.cursor"}} {
			raw, _ := json.Marshal(map[string]any{"results": []any{}, "next_cursor": tc.next})
			a, _ := fakeAPI(t, "GET", "/api/v1/"+resource, 200, string(raw), nil)
			var more bool
			var err error
			if resource == "projects" {
				o, e := a.listProjects(context.Background(), &ListProjectsIn{Cursor: tc.prior})
				err = e
				if o != nil {
					more = o.HasMore
				}
			} else {
				o, e := a.listTasks(context.Background(), &ListTasksIn{Cursor: tc.prior})
				err = e
				if o != nil {
					more = o.HasMore
				}
			}
			if err != nil || more != (tc.next != "") {
				t.Fatalf("valid cursor rejected: resource=%s error=%v", resource, err)
			}
		}
	}
}
