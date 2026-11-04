package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/asana/schema"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const taskFields = "gid,name,notes,completed,due_on,assignee.gid,assignee.name,created_at,modified_at,permalink_url"

type envelope struct {
	Data json.RawMessage `json:"data"`
	Next *struct {
		Offset string `json:"offset"`
	} `json:"next_page"`
}

func (a *api) data(c context.Context, method, path string, q url.Values, body, out any) (*envelope, error) {
	var r envelope
	if body != nil {
		body = map[string]any{"data": body}
	}
	if e := a.json(c, method, path, q, body, &r); e != nil {
		return nil, e
	}
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return nil, fmt.Errorf("Asana response is missing data")
	}
	if e := json.Unmarshal(r.Data, out); e != nil {
		return nil, fmt.Errorf("Asana returned invalid data")
	}
	return &r, nil
}
func pageQuery(in schema.PageOptions) (url.Values, error) {
	limit := 50
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100")
	}
	if len(in.Offset) > 8192 {
		return nil, fmt.Errorf("pagination token exceeds 8192 characters")
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if in.Offset != "" {
		q.Set("offset", in.Offset)
	}
	return q, nil
}
func nextPage(r *envelope, current string) (string, error) {
	if r.Next == nil {
		return "", nil
	}
	if r.Next.Offset == "" || r.Next.Offset == current {
		return "", fmt.Errorf("Asana pagination token is missing or did not advance")
	}
	return r.Next.Offset, nil
}
func (a *api) HealthCheck(c context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r schema.Named
	_, e := a.data(c, "GET", "/users/me", url.Values{"opt_fields": {"gid,name"}}, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.GID == "" {
		return &HealthCheckOut{Message: "Asana did not return an authenticated user"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Asana credential is valid; each operation checks resource access"}, nil
}
func (a *api) named(c context.Context, path string, q url.Values, current string) ([]schema.Named, string, error) {
	var items []schema.Named
	r, e := a.data(c, "GET", path, q, nil, &items)
	if e != nil {
		return nil, "", e
	}
	if items == nil {
		return nil, "", fmt.Errorf("Asana response is missing a collection")
	}
	for _, v := range items {
		if v.GID == "" {
			return nil, "", fmt.Errorf("Asana returned a resource without a GID")
		}
	}
	next, e := nextPage(r, current)
	return items, next, e
}
func (a *api) ListWorkspaces(c context.Context, in *ListWorkspacesIn) (*ListWorkspacesOut, error) {
	q, e := pageQuery(in.Page)
	if e != nil {
		return nil, e
	}
	q.Set("opt_fields", "gid,name")
	v, next, e := a.named(c, "/workspaces", q, in.Page.Offset)
	if e != nil {
		return nil, e
	}
	return &ListWorkspacesOut{Workspaces: v, NextOffset: next, HasMore: next != ""}, nil
}
func (a *api) ListProjects(c context.Context, in *ListProjectsIn) (*ListProjectsOut, error) {
	if e := validID(in.WorkspaceGid); e != nil {
		return nil, e
	}
	q, e := pageQuery(in.Page)
	if e != nil {
		return nil, e
	}
	q.Set("workspace", in.WorkspaceGid)
	q.Set("opt_fields", "gid,name")
	v, next, e := a.named(c, "/projects", q, in.Page.Offset)
	if e != nil {
		return nil, e
	}
	return &ListProjectsOut{Projects: v, NextOffset: next, HasMore: next != ""}, nil
}
func (a *api) ListTasks(c context.Context, in *ListTasksIn) (*ListTasksOut, error) {
	if e := validID(in.ProjectGid); e != nil {
		return nil, e
	}
	q, e := pageQuery(in.Page)
	if e != nil {
		return nil, e
	}
	q.Set("project", in.ProjectGid)
	q.Set("opt_fields", taskFields)
	var items []schema.Task
	r, e := a.data(c, "GET", "/tasks", q, nil, &items)
	if e != nil {
		return nil, e
	}
	if items == nil {
		return nil, fmt.Errorf("Asana response is missing tasks")
	}
	for _, v := range items {
		if v.GID == "" {
			return nil, fmt.Errorf("Asana returned a task without a GID")
		}
	}
	next, e := nextPage(r, in.Page.Offset)
	if e != nil {
		return nil, e
	}
	return &ListTasksOut{Tasks: items, NextOffset: next, HasMore: next != ""}, nil
}
func (a *api) task(c context.Context, method, path string, body any) (schema.Task, error) {
	var v schema.Task
	_, e := a.data(c, method, path, url.Values{"opt_fields": {taskFields}}, body, &v)
	if e == nil && v.GID == "" {
		e = fmt.Errorf("Asana response is missing a task GID")
	}
	return v, e
}
func (a *api) GetTask(c context.Context, in *GetTaskIn) (*GetTaskOut, error) {
	if e := validID(in.TaskGid); e != nil {
		return nil, e
	}
	v, e := a.task(c, "GET", "/tasks/"+in.TaskGid, nil)
	if e != nil {
		return nil, e
	}
	return &GetTaskOut{Task: v}, nil
}
func taskChanges(in schema.TaskChanges) (map[string]any, error) {
	b := map[string]any{}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return nil, fmt.Errorf("task name must not be empty")
		}
		b["name"] = *in.Name
	}
	if in.Notes != nil {
		b["notes"] = *in.Notes
	}
	if in.Completed != nil {
		b["completed"] = *in.Completed
	}
	if in.Assignee != nil {
		if *in.Assignee == "" {
			b["assignee"] = nil
		} else {
			if *in.Assignee != "me" {
				if e := validID(*in.Assignee); e != nil {
					return nil, e
				}
			}
			b["assignee"] = *in.Assignee
		}
	}
	if in.DueOn != nil {
		if *in.DueOn == "" {
			b["due_on"] = nil
		} else {
			if _, e := time.Parse("2006-01-02", *in.DueOn); e != nil {
				return nil, fmt.Errorf("due date must be YYYY-MM-DD")
			}
			b["due_on"] = *in.DueOn
		}
	}
	return b, nil
}
func (a *api) CreateTask(c context.Context, in *CreateTaskIn) (*CreateTaskOut, error) {
	if e := validID(in.WorkspaceGid); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Name) == "" || in.Details.Name != nil {
		return nil, fmt.Errorf("supply name once as the top-level task name")
	}
	for _, p := range in.ProjectGids {
		if e := validID(p); e != nil {
			return nil, e
		}
	}
	b, e := taskChanges(in.Details)
	if e != nil {
		return nil, e
	}
	b["workspace"] = in.WorkspaceGid
	b["name"] = in.Name
	if len(in.ProjectGids) > 0 {
		b["projects"] = in.ProjectGids
	}
	v, e := a.task(c, "POST", "/tasks", b)
	if e != nil {
		return nil, e
	}
	return &CreateTaskOut{Task: v}, nil
}
func (a *api) UpdateTask(c context.Context, in *UpdateTaskIn) (*UpdateTaskOut, error) {
	if e := validID(in.TaskGid); e != nil {
		return nil, e
	}
	b, e := taskChanges(in.Changes)
	if e != nil {
		return nil, e
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("at least one changed field is required")
	}
	v, e := a.task(c, "PUT", "/tasks/"+in.TaskGid, b)
	if e != nil {
		return nil, e
	}
	return &UpdateTaskOut{Task: v}, nil
}
func (a *api) AddComment(c context.Context, in *AddCommentIn) (*AddCommentOut, error) {
	if e := validID(in.TaskGid); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, fmt.Errorf("comment text is required")
	}
	var v schema.Story
	_, e := a.data(c, "POST", "/tasks/"+in.TaskGid+"/stories", url.Values{"opt_fields": {"gid,text,created_at,resource_subtype"}}, map[string]any{"text": in.Text}, &v)
	if e != nil {
		return nil, e
	}
	if v.GID == "" {
		return nil, fmt.Errorf("Asana response is missing a story GID")
	}
	return &AddCommentOut{Story: v}, nil
}
