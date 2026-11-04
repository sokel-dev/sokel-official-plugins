package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"net/http"
	"testing"
)

func TestCreateTaskV1Bytes(t *testing.T) {
	var in CreateTaskIn
	if e := bindTestInput(map[string]any{"content": "Buy milk", "details": map[string]any{"priority": 1, "order": 0, "labels": []any{}, "due_datetime": "2030-01-02T10:00:00+08:00"}}, &in); e != nil {
		t.Fatal(e)
	}
	a, n := fakeAPI(t, "POST", "/api/v1/tasks", 200, fixture(t, "task"), func(r *http.Request, b []byte) {
		var x map[string]any
		json.Unmarshal(b, &x)
		if x["order"] != float64(0) || x["priority"] != float64(1) || x["due_datetime"] != "2030-01-02T02:00:00Z" || x["labels"] == nil {
			t.Fatalf("body %s", b)
		}
	})
	o, e := a.createTask(context.Background(), &in)
	if e != nil || o == nil || o.Task.ID != "6XGgmFVcrG5RRjVr" || *n != 1 {
		t.Fatalf("task %+v %v", o, e)
	}
}
func TestUpdateFalseZeroAndClearLabels(t *testing.T) {
	var in UpdateTaskIn
	if e := bindTestInput(map[string]any{"task_id": "6XGgmFVcrG5RRjVr", "changes": map[string]any{"child_order": 0, "is_collapsed": false, "labels": []any{}, "description": ""}}, &in); e != nil {
		t.Fatal(e)
	}
	a, _ := fakeAPI(t, "POST", "/api/v1/tasks/6XGgmFVcrG5RRjVr", 200, fixture(t, "task"), func(r *http.Request, b []byte) {
		var x map[string]any
		json.Unmarshal(b, &x)
		if x["child_order"] != float64(0) || x["is_collapsed"] != false || x["description"] != "" || x["labels"] == nil {
			t.Fatalf("body %s", b)
		}
	})
	o, e := a.updateTask(context.Background(), &in)
	if e != nil || o == nil {
		t.Fatalf("update %+v %v", o, e)
	}
}
func TestTaskPagination(t *testing.T) {
	a, n := fakeAPI(t, "GET", "/api/v1/tasks", 200, fixture(t, "tasks"), func(r *http.Request, b []byte) {
		if r.URL.Query().Get("cursor") != "prior.cursor" || r.URL.Query().Get("limit") != "50" {
			t.Fatal("page query")
		}
	})
	o, e := a.listTasks(context.Background(), &ListTasksIn{Cursor: "prior.cursor"})
	if e != nil || o == nil || !o.HasMore || o.Tasks == nil || *n != 1 {
		t.Fatalf("tasks %+v %v", o, e)
	}
}
func TestCloseUses200Null(t *testing.T) {
	a, n := fakeAPI(t, "POST", "/api/v1/tasks/6XGgmFVcrG5RRjVr/close", 200, "null", func(r *http.Request, b []byte) {
		if len(b) != 0 {
			t.Fatalf("unexpected body %s", b)
		}
	})
	o, e := a.closeTask(context.Background(), &CloseTaskIn{TaskID: "6XGgmFVcrG5RRjVr"})
	if e != nil || o == nil || !o.Accepted || *n != 1 {
		t.Fatalf("close %+v %v", o, e)
	}
}

func bindTestInput(v map[string]any, out any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return contract.BindInput(b, out)
}
