package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/clickup/schema"
	"net/http"
	"strings"
	"testing"
)

// Synthetic fixtures describe public v2 responses, not captured account traffic.
func TestHierarchyRoutes(t *testing.T) {
	c := context.Background()
	calls := 0
	a := fakeAPI(t, func(r *http.Request) string {
		calls++
		if r.URL.Host != "api.clickup.com" || r.Method != "GET" {
			t.Fatal(r.URL)
		}
		switch r.URL.Path {
		case "/api/v2/team":
			return `{"teams":[{"id":"7","name":"Workspace"}]}`
		case "/api/v2/team/7/space":
			return `{"spaces":[]}`
		case "/api/v2/space/8/folder":
			return `{"folders":[]}`
		case "/api/v2/space/8/list", "/api/v2/folder/9/list":
			return `{"lists":[]}`
		}
		t.Fatal(r.URL)
		return "{}"
	})
	w, e := a.ListWorkspaces(c, &ListWorkspacesIn{})
	if e != nil || len(w.Workspaces) != 1 {
		t.Fatal(w, e)
	}
	if _, e = a.ListSpaces(c, &ListSpacesIn{WorkspaceID: "7"}); e != nil {
		t.Fatal(e)
	}
	if _, e = a.ListFolders(c, &ListFoldersIn{SpaceID: "8"}); e != nil {
		t.Fatal(e)
	}
	for _, in := range []ListListsIn{{ParentType: "space", ParentID: "8"}, {ParentType: "folder", ParentID: "9"}} {
		if _, e = a.ListLists(c, &in); e != nil {
			t.Fatal(e)
		}
	}
	if calls != 5 {
		t.Fatal(calls)
	}
}
func TestTaskPagination(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if r.URL.Path != "/api/v2/list/1/task" || q.Get("page") != "0" || q.Get("include_closed") != "false" || q.Get("include_timl") != "true" {
			t.Fatal(r.URL)
		}
		return `{"tasks":[{"id":"abc","name":"Task","due_date":"1700000000000","priority":null}],"last_page":false}`
	})
	out, e := a.ListTasks(context.Background(), &ListTasksIn{ListID: "1", Options: schema.QueryOptions{Page: ptr(0), IncludeClosed: ptr(false), IncludeTiml: ptr(true)}})
	if e != nil {
		t.Fatal(e)
	}
	if !out.HasMore || out.NextPage != 1 || out.Tasks[0].DueDate != "1700000000000" {
		t.Fatal(out)
	}
}
func TestTaskWritesPreserveFalseAndClearDescription(t *testing.T) {
	c := context.Background()
	var bodies []map[string]any
	var methods []string
	a := fakeAPI(t, func(r *http.Request) string {
		bodies = append(bodies, bodyOf(t, r))
		methods = append(methods, r.Method)
		return `{"id":"abc","name":"Task"}`
	})
	_, e := a.CreateTask(c, &CreateTaskIn{ListID: "1", Name: "Task"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = a.UpdateTask(c, &UpdateTaskIn{TaskID: "abc", Changes: schema.TaskChanges{Description: ptr(""), Archived: ptr(false), Priority: ptr(1), DueDate: ptr(int64(1700000000000)), DueDateTime: ptr(false)}})
	if e != nil {
		t.Fatal(e)
	}
	if methods[0] != "POST" || methods[1] != "PUT" || bodies[0]["name"] != "Task" {
		t.Fatal(bodies, methods)
	}
	b := bodies[1]
	if len(b) != 5 || b["archived"] != false || b["priority"] != float64(1) || b["due_date"] != float64(1700000000000) || b["due_date_time"] != false || b["description"] != " " {
		t.Fatal(b)
	}
}
func TestCommentNumericIDAndNotification(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.URL.Path != "/api/v2/task/abc/comment" || r.Method != "POST" {
			t.Fatal(r.URL)
		}
		b := bodyOf(t, r)
		if b["notify_all"] != false || b["comment_text"] != "comment" {
			t.Fatal(b)
		}
		return `{"id":9007199254740993}`
	})
	o, e := a.AddComment(context.Background(), &AddCommentIn{TaskID: "abc", Text: "comment"})
	if e != nil || o.CommentID != "9007199254740993" {
		t.Fatal(o, e)
	}
}
func TestMissingCollectionsFail(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { return `{}` })
	c := context.Background()
	if _, e := a.ListWorkspaces(c, &ListWorkspacesIn{}); e == nil {
		t.Fatal("missing teams accepted")
	}
	if _, e := a.ListTasks(c, &ListTasksIn{ListID: "1"}); e == nil {
		t.Fatal("missing tasks accepted")
	}
	if _, e := a.GetTask(c, &GetTaskIn{TaskID: "abc"}); e == nil {
		t.Fatal("missing task accepted")
	}
}
func TestInputValidation(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid request sent"); return `{}` })
	c := context.Background()
	tests := []func() error{func() error { _, e := a.GetTask(c, &GetTaskIn{TaskID: "../x"}); return e }, func() error { _, e := a.UpdateTask(c, &UpdateTaskIn{TaskID: "abc"}); return e }, func() error {
		_, e := a.UpdateTask(c, &UpdateTaskIn{TaskID: "abc", Changes: schema.TaskChanges{Priority: ptr(5)}})
		return e
	}, func() error {
		_, e := a.CreateTask(c, &CreateTaskIn{ListID: "1", Name: "Task", Details: schema.TaskChanges{Name: ptr("conflict")}})
		return e
	}, func() error { _, e := a.ListLists(c, &ListListsIn{ParentType: "url", ParentID: "1"}); return e }}
	for _, f := range tests {
		if f() == nil {
			t.Fatal("invalid accepted")
		}
	}
}
func TestFullPageFallbackAndEmptyEnd(t *testing.T) {
	tasks := make([]schema.Task, 100)
	for i := range tasks {
		tasks[i].ID = "abc"
	}
	raw, _ := json.Marshal(map[string]any{"tasks": tasks})
	a := fakeAPI(t, func(*http.Request) string { return string(raw) })
	o, e := a.ListTasks(context.Background(), &ListTasksIn{ListID: "1"})
	if e != nil || !o.HasMore || o.NextPage != 1 {
		t.Fatal(o, e)
	}
	a = fakeAPI(t, func(*http.Request) string { return `{"tasks":[]}` })
	o, e = a.ListTasks(context.Background(), &ListTasksIn{ListID: "1"})
	if e != nil || o.HasMore {
		t.Fatal(o, e)
	}
}
func TestHealthAndErrorRedaction(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.URL.Path != "/api/v2/user" {
			t.Fatal(r.URL)
		}
		return `{"user":{"id":1,"username":"Tester"}}`
	})
	o, e := a.HealthCheck(context.Background(), &HealthCheckIn{})
	if e != nil || !o.OK {
		t.Fatal(o, e)
	}
	a = fakeAPI(t, func(*http.Request) string { return `{"err":"test-secret","ECODE":"TOKEN_001"}` })
	_, e = a.GetTask(context.Background(), &GetTaskIn{TaskID: "abc"})
	if e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
}

func TestCommentRejectsNonIDJSONTypes(t *testing.T) {
	for _, raw := range []string{`{"id":true}`, `{"id":false}`, `{"id":null}`, `{"id":[]}`, `{"id":{}}`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.AddComment(context.Background(), &AddCommentIn{TaskID: "abc", Text: "Comment"}); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestUnsupportedZeroClearsAreRejected(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("unsupported clear sent"); return "{}" })
	for _, changes := range []schema.TaskChanges{{Priority: ptr(0)}, {DueDate: ptr(int64(0))}, {DueDateTime: ptr(false)}} {
		if _, e := a.UpdateTask(context.Background(), &UpdateTaskIn{TaskID: "abc", Changes: changes}); e == nil {
			t.Fatal(changes)
		}
	}
}

func TestDueDateTimeDefaultsAndOptIn(t *testing.T) {
	for _, opt := range []*bool{nil, ptr(true)} {
		a := fakeAPI(t, func(r *http.Request) string {
			b := bodyOf(t, r)
			want := opt != nil && *opt
			if b["due_date_time"] != want {
				t.Fatal(b)
			}
			return `{"id":"abc"}`
		})
		if _, e := a.UpdateTask(context.Background(), &UpdateTaskIn{TaskID: "abc", Changes: schema.TaskChanges{DueDate: ptr(int64(1700000000000)), DueDateTime: opt}}); e != nil {
			t.Fatal(e)
		}
	}
}
