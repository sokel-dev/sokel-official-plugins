package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/asana/schema"
	"net/http"
	"strings"
	"testing"
)

// Synthetic fixtures follow Asana's documented data and next_page envelopes.
func TestPagedProjectTasks(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if r.URL.Host != "app.asana.com" || r.URL.Path != "/api/1.0/tasks" || q.Get("project") != "123" || q.Get("limit") != "2" || q.Get("offset") != "one&two" || !strings.Contains(q.Get("opt_fields"), "assignee.name") {
			t.Fatal(r.URL)
		}
		return `{"data":[{"gid":"9","name":"Task","completed":false,"due_on":null,"assignee":{"gid":"8","name":"Owner"}}],"next_page":{"offset":"next","uri":"https://evil.example"}}`
	})
	o, e := a.ListTasks(context.Background(), &ListTasksIn{ProjectGid: "123", Page: schema.PageOptions{Limit: ptr(2), Offset: "one&two"}})
	if e != nil {
		t.Fatal(e)
	}
	if !o.HasMore || o.NextOffset != "next" || o.Tasks[0].Assignee.GID != "8" {
		t.Fatal(o)
	}
}
func TestMutationsWrapDataAndClearNull(t *testing.T) {
	var bodies []map[string]any
	var methods []string
	a := fakeAPI(t, func(r *http.Request) string {
		bodies = append(bodies, bodyOf(t, r))
		methods = append(methods, r.Method)
		return `{"data":{"gid":"9","name":"Task"}}`
	})
	c := context.Background()
	_, e := a.CreateTask(c, &CreateTaskIn{WorkspaceGid: "1", Name: "Task", ProjectGids: []string{"2"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = a.UpdateTask(c, &UpdateTaskIn{TaskGid: "9", Changes: schema.TaskChanges{Completed: ptr(false), Assignee: ptr(""), DueOn: ptr(""), Notes: ptr("")}})
	if e != nil {
		t.Fatal(e)
	}
	b := bodies[1]["data"].(map[string]any)
	if methods[0] != "POST" || methods[1] != "PUT" || len(b) != 4 || b["completed"] != false || b["notes"] != "" {
		t.Fatal(b, methods)
	}
	for _, key := range []string{"assignee", "due_on"} {
		v, ok := b[key]
		if !ok || v != nil {
			t.Fatal(b)
		}
	}
	first := bodies[0]["data"].(map[string]any)
	if first["workspace"] != "1" || first["name"] != "Task" {
		t.Fatal(first)
	}
}
func TestDiscoveryGetAndComment(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/api/1.0/users/me":
			return `{"data":{"gid":"1"}}`
		case "/api/1.0/workspaces":
			return `{"data":[{"gid":"2","name":"Work"}],"next_page":null}`
		case "/api/1.0/projects":
			if r.URL.Query().Get("workspace") != "2" {
				t.Fatal(r.URL)
			}
			return `{"data":[]}`
		case "/api/1.0/tasks/9":
			return `{"data":{"gid":"9","name":"Task"}}`
		case "/api/1.0/tasks/9/stories":
			b := bodyOf(t, r)
			if b["data"].(map[string]any)["text"] != "Hello" || r.Method != "POST" {
				t.Fatal(b)
			}
			return `{"data":{"gid":"3","text":"Hello","resource_subtype":"comment_added"}}`
		}
		t.Fatal(r.URL)
		return "{}"
	})
	c := context.Background()
	h, e := a.HealthCheck(c, &HealthCheckIn{})
	if e != nil || !h.OK {
		t.Fatal(h, e)
	}
	w, e := a.ListWorkspaces(c, &ListWorkspacesIn{})
	if e != nil || w.HasMore || len(w.Workspaces) != 1 {
		t.Fatal(w, e)
	}
	if _, e = a.ListProjects(c, &ListProjectsIn{WorkspaceGid: "2"}); e != nil {
		t.Fatal(e)
	}
	if _, e = a.GetTask(c, &GetTaskIn{TaskGid: "9"}); e != nil {
		t.Fatal(e)
	}
	o, e := a.AddComment(c, &AddCommentIn{TaskGid: "9", Text: "Hello"})
	if e != nil || o.Story.GID != "3" {
		t.Fatal(o, e)
	}
}
func TestMalformedPaginationFails(t *testing.T) {
	for _, raw := range []string{`{}`, `{"data":null}`, `{"data":[] ,"next_page":{}}`, `{"data":[],"next_page":{"offset":"same"}}`, `{"data":[{}]}`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.ListWorkspaces(context.Background(), &ListWorkspacesIn{Page: schema.PageOptions{Offset: "same"}}); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestInvalidMutationsDoNotSend(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid request sent"); return "{}" })
	c := context.Background()
	for _, in := range []UpdateTaskIn{{TaskGid: "../1", Changes: schema.TaskChanges{Notes: ptr("x")}}, {TaskGid: "1"}, {TaskGid: "1", Changes: schema.TaskChanges{DueOn: ptr("2026-02-30")}}, {TaskGid: "1", Changes: schema.TaskChanges{Name: ptr("")}}} {
		if _, e := a.UpdateTask(c, &in); e == nil {
			t.Fatal(in)
		}
	}
	if _, e := a.ListWorkspaces(c, &ListWorkspacesIn{Page: schema.PageOptions{Limit: ptr(0)}}); e == nil {
		t.Fatal("zero limit accepted")
	}
	if _, e := a.CreateTask(c, &CreateTaskIn{WorkspaceGid: "1", Name: "Task", Details: schema.TaskChanges{Name: ptr("conflict")}}); e == nil {
		t.Fatal("conflicting name accepted")
	}
}
