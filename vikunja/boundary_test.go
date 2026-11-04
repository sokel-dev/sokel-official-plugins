package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/vikunja/schema"
	"net/http"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func TestIDsAndPartialFields(t *testing.T) {
	for _, s := range []string{"", "0", "01", "1e3", "1.0", "9223372036854775808", "../1"} {
		if positiveID(s) || projectID(s) {
			t.Fatal(s)
		}
	}
	if positiveID("-2") || !projectID("-2") || !positiveID("9007199254740993") {
		t.Fatal("ID domain")
	}
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	for _, f := range []schema.TaskFields{{}, {Title: ptr("")}, {Title: ptr(strings.Repeat("a", 251))}, {Description: ptr("\x00")}, {Description: ptr(strings.Repeat("x", (256<<10)+1))}, {Priority: ptr(-1)}, {Priority: ptr(6)}, {DueDate: ptr("")}, {DueDate: ptr("2026-01-01")}} {
		if _, e := c.update_task(context.Background(), &UpdateTaskIn{TaskID: "1", Fields: f}); e == nil {
			t.Fatal("invalid patch", f)
		}
	}
	if _, e := c.create_task(context.Background(), &CreateTaskIn{ProjectID: "-2", Title: "No virtual writes"}); e == nil {
		t.Fatal("virtual project write")
	}
	if _, e := c.get_task(context.Background(), &GetTaskIn{TaskID: "-1"}); e == nil {
		t.Fatal("negative task")
	}
	if _, e := c.create_project(context.Background(), &CreateProjectIn{Title: " "}); e == nil {
		t.Fatal("empty title")
	}
	if _, e := c.add_comment(context.Background(), &AddCommentIn{TaskID: "1", Comment: ""}); e == nil {
		t.Fatal("empty comment")
	}
	f := schema.TaskFields{Title: ptr("A"), Description: ptr(""), Done: ptr(false), Priority: ptr(0), DueDate: ptr("0001-01-01T00:00:00Z")}
	b, e := taskBody(f)
	if e != nil || len(b) != 5 || b["done"] != false || b["priority"] != 0 || b["description"] != "" {
		t.Fatal(b, e)
	}
}
func TestVikunjaPaginationBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		size       int
		wantError  bool
	}{
		{"empty null items", `{"items":null,"total":0,"page":1,"per_page":1,"total_pages":0}`, 1, false},
		{"server caps page", `{"items":[{}],"total":2,"page":1,"per_page":1,"total_pages":2}`, 50, false},
		{"missing items", `{"total":0,"page":1,"per_page":1,"total_pages":0}`, 1, true},
		{"bad page", `{"items":[],"total":0,"page":2,"per_page":1,"total_pages":0}`, 1, true},
		{"bad total", `{"items":[],"total":-1,"page":1,"per_page":1,"total_pages":0}`, 1, true},
		{"missing continuation rows", `{"items":[],"total":2,"page":1,"per_page":1,"total_pages":2}`, 1, true},
		{"wrong pages", `{"items":[{}],"total":2,"page":1,"per_page":1,"total_pages":1}`, 1, true},
		{"too many ordinary rows", `{"items":[{},{}],"total":2,"page":1,"per_page":1,"total_pages":2}`, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, e := pageValue([]byte(tt.body), 1, tt.size, false)
			if (e != nil) != tt.wantError {
				t.Fatal(e)
			}
		})
	}
	for _, p := range []schema.Page{{Number: ptr(0)}, {Number: ptr(1000001)}, {Size: ptr(0)}, {Size: ptr(1001)}, {Query: ptr(strings.Repeat("x", 16385))}, {Query: ptr("x\x00")}} {
		if _, _, _, e := pageQuery(p); e == nil {
			t.Fatal(p)
		}
	}
}
func TestVirtualProjectReadAndTaskMembership(t *testing.T) {
	task := fixtures(t)[5].Response
	c := fresh()
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/api/v2/projects/-2/tasks" {
			t.Fatal(r.URL)
		}
		return httpResponse(200, `{"items":[`+task+`],"total":1,"page":1,"per_page":50,"total_pages":1}`), nil
	})
	o, e := c.list_tasks(context.Background(), &ListTasksIn{ProjectID: "-2"})
	if e != nil || len(o.Tasks) != 1 || o.Tasks[0].ProjectID != "8" {
		t.Fatal(o, e)
	}
	if _, e = taskValue([]byte(task), "", "9"); e == nil {
		t.Fatal("real project mismatch")
	}
	project := `{"id":-2,"title":"Saved filter","description":"","is_archived":false}`
	withReply(c, 200, project)
	p, e := c.get_project(context.Background(), &GetProjectIn{ProjectID: "-2"})
	if e != nil || p.Project.ID != "-2" {
		t.Fatal(p, e)
	}
}
func TestIncompleteAndMismatchedReceipts(t *testing.T) {
	c := fresh()
	ctx := context.Background()
	for _, body := range []string{`{"id":2,"title":"wrong","is_archived":false}`, `{"id":0,"title":"x","is_archived":false}`, `{"id":1,"title":"x"}`} {
		withReply(c, 200, body)
		if _, e := c.get_project(ctx, &GetProjectIn{ProjectID: "1"}); e == nil {
			t.Fatal(body)
		}
	}
	base := fixtures(t)[5].Response
	for _, key := range []string{"id", "project_id", "done", "priority", "due_date"} {
		var v map[string]any
		_ = decodeJSON([]byte(base), &v)
		delete(v, key)
		b, _ := json.Marshal(v)
		withReply(c, 200, string(b))
		if _, e := c.get_task(ctx, &GetTaskIn{TaskID: "9007199254740993"}); e == nil {
			t.Fatal("missing", key)
		}
	}
	for _, body := range []string{`{"id":0,"comment":"x","created":"2026-11-04T00:00:00Z"}`, `{"id":1,"comment":"x","created":"bad"}`, `{"id":1,"comment":"","created":"2026-11-04T00:00:00Z"}`} {
		withReply(c, 201, body)
		if _, e := c.add_comment(ctx, &AddCommentIn{TaskID: "1", Comment: "x"}); e == nil {
			t.Fatal(body)
		}
	}
}
func TestPatchMediaType(t *testing.T) {
	c := fresh()
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Content-Type") != "application/merge-patch+json" || r.GetBody != nil {
			t.Fatal("unsafe patch", r.Header)
		}
		return httpResponse(200, fixtures(t)[5].Response), nil
	})
	if _, e := c.update_task(context.Background(), &UpdateTaskIn{TaskID: "9007199254740993", Fields: schema.TaskFields{Done: ptr(false)}}); e != nil {
		t.Fatal(e)
	}
}
