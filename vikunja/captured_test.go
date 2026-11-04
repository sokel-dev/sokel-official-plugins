package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/vikunja/schema"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCapturedServiceResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-service-captures.json")
	if e != nil {
		t.Fatal(e)
	}
	var captures []captureEntry
	if e = json.Unmarshal(b, &captures); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	ctx := context.Background()
	for i, x := range captures {
		if x.Method == "DELETE" {
			continue
		}
		c := fresh()
		withReply(c, x.Status, string(x.Response))
		parts := strings.Split(strings.Trim(x.Path, "/"), "/")
		q, _ := url.ParseQuery(x.Query)
		page, size := 1, 50
		if v := q.Get("page"); v != "" {
			page, _ = strconv.Atoi(v)
		}
		if v := q.Get("per_page"); v != "" {
			size, _ = strconv.Atoi(v)
		}
		pg := schema.Page{Number: &page, Size: &size}
		if q.Has("q") {
			s := q.Get("q")
			pg.Query = &s
		}
		op := ""
		e = nil
		switch {
		case x.Path == "/api/v2/projects" && x.Method == "GET" && i == 0:
			op = "health_check"
			var o *HealthCheckOut
			o, e = c.health_check(ctx, &HealthCheckIn{})
			if e == nil && !o.OK {
				t.Fatal(o)
			}
		case x.Path == "/api/v2/projects" && x.Method == "GET":
			op = "list_projects"
			_, e = c.list_projects(ctx, &ListProjectsIn{Page: pg})
		case x.Path == "/api/v2/projects" && x.Method == "POST":
			op = "create_project"
			var in CreateProjectIn
			_ = json.Unmarshal(x.Request, &in)
			_, e = c.create_project(ctx, &in)
		case len(parts) == 4 && parts[2] == "projects":
			op = "get_project"
			_, e = c.get_project(ctx, &GetProjectIn{ProjectID: parts[3]})
		case len(parts) == 5 && parts[2] == "projects" && x.Method == "GET":
			op = "list_tasks"
			_, e = c.list_tasks(ctx, &ListTasksIn{ProjectID: parts[3], Page: pg})
		case len(parts) == 5 && parts[2] == "projects" && x.Method == "POST":
			op = "create_task"
			var f schema.TaskOptions
			_ = json.Unmarshal(x.Request, &f)
			var v struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(x.Request, &v)
			_, e = c.create_task(ctx, &CreateTaskIn{ProjectID: parts[3], Title: v.Title, Fields: f})
		case len(parts) == 4 && parts[2] == "tasks" && x.Method == "GET":
			op = "get_task"
			_, e = c.get_task(ctx, &GetTaskIn{TaskID: parts[3]})
		case len(parts) == 4 && parts[2] == "tasks" && x.Method == "PATCH":
			op = "update_task"
			var f schema.TaskFields
			_ = json.Unmarshal(x.Request, &f)
			_, e = c.update_task(ctx, &UpdateTaskIn{TaskID: parts[3], Fields: f})
		case len(parts) == 5 && parts[2] == "tasks" && parts[4] == "comments":
			op = "add_comment"
			var v struct {
				Comment string `json:"comment"`
			}
			_ = json.Unmarshal(x.Request, &v)
			_, e = c.add_comment(ctx, &AddCommentIn{TaskID: parts[3], Comment: v.Comment})
		default:
			t.Fatal("unexpected captured route", x.Method, x.Path)
		}
		if e != nil {
			t.Fatal(op, e)
		}
		seen[op] = true
	}
	if len(seen) != 9 {
		t.Fatal("not every operation family replayed", seen)
	}
}
