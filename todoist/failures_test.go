package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/todoist/schema"
	"testing"
)

func TestValidationNeverCallsAPI(t *testing.T) {
	a, n := fakeAPI(t, "GET", "unused", 200, "{}", nil)
	ctx := context.Background()
	for _, id := range []string{"", "tmp-123", "../task", "https://evil.invalid"} {
		if _, e := a.getTask(ctx, &GetTaskIn{TaskID: id}); e == nil {
			t.Fatal("ID accepted")
		}
	}
	if _, e := a.listTasks(ctx, &ListTasksIn{Cursor: "https://evil.invalid"}); e == nil {
		t.Fatal("cursor")
	}
	if _, e := a.listProjects(ctx, &ListProjectsIn{Limit: 201}); e == nil {
		t.Fatal("limit")
	}
	if _, e := a.updateTask(ctx, &UpdateTaskIn{TaskID: "TASK"}); e == nil {
		t.Fatal("empty patch")
	}
	for _, p := range []int{0, 5} {
		if _, e := a.createTask(ctx, &CreateTaskIn{Content: "Task", Details: schema.TaskDetails{Priority: &p}}); e == nil {
			t.Fatal("priority")
		}
	}
	date := "2030-02-30"
	datetime := "2030-01-01T00:00:00Z"
	for _, v := range []schema.TaskDetails{{DueDate: &date}, {DueDate: &date, DueDatetime: &datetime}, {DueDatetime: &date}} {
		if _, e := a.createTask(ctx, &CreateTaskIn{Content: "Task", Details: v}); e == nil {
			t.Fatal("date")
		}
	}
	badID := "../x"
	if _, e := a.createTask(ctx, &CreateTaskIn{Content: "Task", Details: schema.TaskDetails{ProjectID: &badID}}); e == nil {
		t.Fatal("project")
	}
	style := "invalid"
	if _, e := a.createProject(ctx, &CreateProjectIn{Name: "Plan", Details: schema.ProjectDetails{ViewStyle: &style}}); e == nil {
		t.Fatal("style")
	}
	if *n != 0 {
		t.Fatal("invalid input requested")
	}
}
func TestResponseShapes(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{"{}", "null", `{"results":[{}]}`, `{"results":[],"next_cursor":"https://evil.invalid"}`} {
		a, _ := fakeAPI(t, "GET", "/api/v1/tasks", 200, body, nil)
		if _, e := a.listTasks(ctx, &ListTasksIn{}); e == nil {
			t.Fatal("tasks shape")
		}
		a, _ = fakeAPI(t, "GET", "/api/v1/projects", 200, body, nil)
		if _, e := a.listProjects(ctx, &ListProjectsIn{}); e == nil {
			t.Fatal("projects shape")
		}
	}
	for _, body := range []string{"{}", "null"} {
		a, _ := fakeAPI(t, "GET", "/api/v1/tasks/TASK", 200, body, nil)
		if _, e := a.getTask(ctx, &GetTaskIn{TaskID: "TASK"}); e == nil {
			t.Fatal("missing task")
		}
		a, _ = fakeAPI(t, "GET", "/api/v1/projects/PROJ", 200, body, nil)
		if _, e := a.getProject(ctx, &GetProjectIn{ProjectID: "PROJ"}); e == nil {
			t.Fatal("missing project")
		}
	}
	a, _ := fakeAPI(t, "POST", "/api/v1/tasks/TASK/close", 200, `{}`, nil)
	if _, e := a.closeTask(ctx, &CloseTaskIn{TaskID: "TASK"}); e == nil {
		t.Fatal("close shape")
	}
}
