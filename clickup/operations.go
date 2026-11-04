package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/clickup/schema"
	"net/url"
	"strconv"
	"strings"
)

func (a *api) HealthCheck(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		User *schema.User `json:"user"`
	}
	e := a.json(ctx, "GET", "/user", nil, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.User == nil || r.User.ID == 0 {
		return &HealthCheckOut{Message: "ClickUp did not return an authenticated user"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "ClickUp credential is valid; each operation checks resource access"}, nil
}
func (a *api) named(ctx context.Context, path, key string) ([]schema.Named, error) {
	var raw map[string]json.RawMessage
	if e := a.json(ctx, "GET", path, nil, nil, &raw); e != nil {
		return nil, e
	}
	var items []schema.Named
	if e := json.Unmarshal(raw[key], &items); e != nil || items == nil {
		return nil, fmt.Errorf("ClickUp response is missing %s", key)
	}
	for _, v := range items {
		if v.ID == "" {
			return nil, fmt.Errorf("ClickUp returned a resource without an ID")
		}
	}
	return items, nil
}
func (a *api) ListWorkspaces(c context.Context, _ *ListWorkspacesIn) (*ListWorkspacesOut, error) {
	v, e := a.named(c, "/team", "teams")
	if e != nil {
		return nil, e
	}
	return &ListWorkspacesOut{Workspaces: v}, nil
}
func (a *api) ListSpaces(c context.Context, in *ListSpacesIn) (*ListSpacesOut, error) {
	if e := validID(in.WorkspaceID); e != nil {
		return nil, e
	}
	v, e := a.named(c, "/team/"+in.WorkspaceID+"/space", "spaces")
	if e != nil {
		return nil, e
	}
	return &ListSpacesOut{Spaces: v}, nil
}
func (a *api) ListFolders(c context.Context, in *ListFoldersIn) (*ListFoldersOut, error) {
	if e := validID(in.SpaceID); e != nil {
		return nil, e
	}
	v, e := a.named(c, "/space/"+in.SpaceID+"/folder", "folders")
	if e != nil {
		return nil, e
	}
	return &ListFoldersOut{Folders: v}, nil
}
func (a *api) ListLists(c context.Context, in *ListListsIn) (*ListListsOut, error) {
	if e := validID(in.ParentID); e != nil {
		return nil, e
	}
	if in.ParentType != "space" && in.ParentType != "folder" {
		return nil, fmt.Errorf("parent type must be space or folder")
	}
	v, e := a.named(c, "/"+in.ParentType+"/"+in.ParentID+"/list", "lists")
	if e != nil {
		return nil, e
	}
	return &ListListsOut{Lists: v}, nil
}
func (a *api) ListTasks(c context.Context, in *ListTasksIn) (*ListTasksOut, error) {
	if e := validID(in.ListID); e != nil {
		return nil, e
	}
	page := 0
	if in.Options.Page != nil {
		page = *in.Options.Page
	}
	if page < 0 || page > 1000000 {
		return nil, fmt.Errorf("page must be between 0 and 1000000")
	}
	q := url.Values{"page": {strconv.Itoa(page)}}
	for key, v := range map[string]*bool{"archived": in.Options.Archived, "include_closed": in.Options.IncludeClosed, "include_timl": in.Options.IncludeTiml, "subtasks": in.Options.Subtasks} {
		if v != nil {
			q.Set(key, strconv.FormatBool(*v))
		}
	}
	var r struct {
		Tasks    []schema.Task `json:"tasks"`
		LastPage *bool         `json:"last_page"`
	}
	if e := a.json(c, "GET", "/list/"+in.ListID+"/task", q, nil, &r); e != nil {
		return nil, e
	}
	if r.Tasks == nil {
		return nil, fmt.Errorf("ClickUp response is missing tasks")
	}
	for _, t := range r.Tasks {
		if t.ID == "" {
			return nil, fmt.Errorf("ClickUp returned a task without an ID")
		}
	}
	more := len(r.Tasks) == 100
	if r.LastPage != nil {
		more = !*r.LastPage
	}
	if more && len(r.Tasks) == 0 {
		return nil, fmt.Errorf("ClickUp pagination made no progress")
	}
	next := 0
	if more {
		next = page + 1
	}
	return &ListTasksOut{Tasks: r.Tasks, HasMore: more, NextPage: next}, nil
}
func (a *api) task(c context.Context, method, path string, body any) (schema.Task, error) {
	var r schema.Task
	e := a.json(c, method, path, nil, body, &r)
	if e == nil && r.ID == "" {
		e = fmt.Errorf("ClickUp response is missing a task ID")
	}
	return r, e
}
func (a *api) GetTask(c context.Context, in *GetTaskIn) (*GetTaskOut, error) {
	if e := validID(in.TaskID); e != nil {
		return nil, e
	}
	v, e := a.task(c, "GET", "/task/"+in.TaskID, nil)
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
	if in.Description != nil {
		v := *in.Description
		if v == "" {
			v = " "
		}
		b["description"] = v
	}
	if in.Status != nil {
		if strings.TrimSpace(*in.Status) == "" {
			return nil, fmt.Errorf("status must not be empty")
		}
		b["status"] = *in.Status
	}
	if in.Priority != nil {
		if *in.Priority < 1 || *in.Priority > 4 {
			return nil, fmt.Errorf("priority must be between 1 and 4")
		}
		b["priority"] = *in.Priority
	}
	if in.DueDate != nil {
		if *in.DueDate <= 0 || *in.DueDate > 9007199254740991 {
			return nil, fmt.Errorf("due date must be a positive, exactly representable millisecond timestamp")
		}
		b["due_date"] = *in.DueDate
		b["due_date_time"] = false
	}
	if in.DueDateTime != nil {
		if in.DueDate == nil {
			return nil, fmt.Errorf("include due time requires a due date")
		}
		b["due_date_time"] = *in.DueDateTime
	}
	if in.Archived != nil {
		b["archived"] = *in.Archived
	}
	return b, nil
}
func (a *api) CreateTask(c context.Context, in *CreateTaskIn) (*CreateTaskOut, error) {
	if e := validID(in.ListID); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Name) == "" || in.Details.Name != nil {
		return nil, fmt.Errorf("supply name once as the top-level task name")
	}
	if in.Details.Archived != nil {
		return nil, fmt.Errorf("archived is supported only when updating a task")
	}
	b, e := taskChanges(in.Details)
	if e != nil {
		return nil, e
	}
	b["name"] = in.Name
	v, e := a.task(c, "POST", "/list/"+in.ListID+"/task", b)
	if e != nil {
		return nil, e
	}
	return &CreateTaskOut{Task: v}, nil
}
func (a *api) UpdateTask(c context.Context, in *UpdateTaskIn) (*UpdateTaskOut, error) {
	if e := validID(in.TaskID); e != nil {
		return nil, e
	}
	b, e := taskChanges(in.Changes)
	if e != nil {
		return nil, e
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("at least one changed field is required")
	}
	v, e := a.task(c, "PUT", "/task/"+in.TaskID, b)
	if e != nil {
		return nil, e
	}
	return &UpdateTaskOut{Task: v}, nil
}
func (a *api) AddComment(c context.Context, in *AddCommentIn) (*AddCommentOut, error) {
	if e := validID(in.TaskID); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, fmt.Errorf("comment text is required")
	}
	var r struct {
		ID json.RawMessage `json:"id"`
	}
	e := a.json(c, "POST", "/task/"+in.TaskID+"/comment", nil, map[string]any{"comment_text": in.Text, "notify_all": in.NotifyAll}, &r)
	if e != nil {
		return nil, e
	}
	var id string
	if len(r.ID) > 0 && r.ID[0] == '"' {
		_ = json.Unmarshal(r.ID, &id)
	} else {
		// Preserve integer precision but reject booleans, null, fractions and exponents.
		for _, digit := range r.ID {
			if digit < '0' || digit > '9' {
				return nil, fmt.Errorf("ClickUp response has an invalid comment ID")
			}
		}
		id = string(r.ID)
	}
	if e = validID(id); e != nil || id == "null" {
		return nil, fmt.Errorf("ClickUp response is missing a valid comment ID")
	}
	return &AddCommentOut{CommentID: id}, nil
}
