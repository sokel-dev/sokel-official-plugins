package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/todoist/schema"
	"net/url"
	"regexp"
	"time"
)

func (a *api) health(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	b, e := a.call(ctx, "GET", "/user", nil, nil, 200)
	if e == nil {
		var u struct {
			ID string `json:"id"`
		}
		e = decode(b, &u)
		if e == nil && u.ID == "" {
			e = badResponse()
		}
	}
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Todoist user identity verified"}, nil
}

var cursorPattern = regexp.MustCompile(`^[0-9a-zA-Z_-]+\.[0-9a-zA-Z_-]+$`)

func todoistPage(limit int, cursor string) (url.Values, error) {
	if cursor != "" && !cursorPattern.MatchString(cursor) {
		return nil, errors.New("invalid Todoist pagination cursor; reuse the previous next_cursor")
	}
	return pageQuery(limit, 200, cursor, "cursor")
}
func validCursor(cursor, prior string) error {
	if len(cursor) > 4096 || (cursor != "" && (!cursorPattern.MatchString(cursor) || cursor == prior)) {
		return badResponse()
	}
	return nil
}
func (a *api) listProjects(ctx context.Context, in *ListProjectsIn) (*ListProjectsOut, error) {
	q, e := todoistPage(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/projects", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Results []schema.Project `json:"results"`
		Next    string           `json:"next_cursor"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Results == nil {
		return nil, badResponse()
	}
	for _, p := range v.Results {
		if p.ID == "" {
			return nil, badResponse()
		}
	}
	if e = validCursor(v.Next, in.Cursor); e != nil {
		return nil, e
	}
	return &ListProjectsOut{Projects: v.Results, NextCursor: v.Next, HasMore: v.Next != ""}, nil
}
func (a *api) getProject(ctx context.Context, in *GetProjectIn) (*GetProjectOut, error) {
	if e := requireID(in.ProjectID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/projects/"+in.ProjectID, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var p schema.Project
	if e = decode(b, &p); e != nil {
		return nil, e
	}
	if p.ID == "" {
		return nil, badResponse()
	}
	return &GetProjectOut{Project: p}, nil
}
func (a *api) createProject(ctx context.Context, in *CreateProjectIn) (*CreateProjectOut, error) {
	if e := requiredText(in.Name, 120); e != nil {
		return nil, e
	}
	if in.Details.ParentID != nil {
		if e := requireID(*in.Details.ParentID); e != nil {
			return nil, e
		}
	}
	if in.Details.ViewStyle != nil && *in.Details.ViewStyle != "list" && *in.Details.ViewStyle != "board" && *in.Details.ViewStyle != "calendar" && *in.Details.ViewStyle != "table" {
		return nil, errors.New("view style must be list, board, calendar or table")
	}
	v := struct {
		Name string `json:"name"`
		schema.ProjectDetails
	}{in.Name, in.Details}
	b, e := a.call(ctx, "POST", "/projects", nil, v, 200)
	if e != nil {
		return nil, e
	}
	var p schema.Project
	if e = decode(b, &p); e != nil {
		return nil, e
	}
	if p.ID == "" {
		return nil, badResponse()
	}
	return &CreateProjectOut{Project: p}, nil
}
func (a *api) listTasks(ctx context.Context, in *ListTasksIn) (*ListTasksOut, error) {
	q, e := todoistPage(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	for _, v := range []struct{ k, s string }{{"project_id", in.ProjectID}, {"section_id", in.SectionID}, {"parent_id", in.ParentID}} {
		if v.s != "" {
			if e = requireID(v.s); e != nil {
				return nil, e
			}
			q.Set(v.k, v.s)
		}
	}
	if in.Label != "" {
		if e = requiredText(in.Label, 512); e != nil {
			return nil, e
		}
		q.Set("label", in.Label)
	}
	b, e := a.call(ctx, "GET", "/tasks", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Results []schema.Task `json:"results"`
		Next    string        `json:"next_cursor"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Results == nil {
		return nil, badResponse()
	}
	for _, task := range v.Results {
		if task.ID == "" {
			return nil, badResponse()
		}
	}
	if e = validCursor(v.Next, in.Cursor); e != nil {
		return nil, e
	}
	return &ListTasksOut{Tasks: v.Results, NextCursor: v.Next, HasMore: v.Next != ""}, nil
}
func taskOutput(b []byte) (schema.Task, error) {
	var v schema.Task
	if e := decode(b, &v); e != nil {
		return v, e
	}
	if v.ID == "" {
		return v, badResponse()
	}
	return v, nil
}
func (a *api) getTask(ctx context.Context, in *GetTaskIn) (*GetTaskOut, error) {
	if e := requireID(in.TaskID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/tasks/"+in.TaskID, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	v, e := taskOutput(b)
	if e != nil {
		return nil, e
	}
	return &GetTaskOut{Task: v}, nil
}
func validateTaskFields(priority *int, labels *[]string, date *string, datetime **string, order *int) error {
	if priority != nil && (*priority < 1 || *priority > 4) {
		return errors.New("priority must be between 1 and 4")
	}
	if labels != nil {
		if *labels == nil {
			return errors.New("labels must be an array; use an empty array to clear labels")
		}
		if len(*labels) > 100 {
			return errors.New("at most 100 labels are supported")
		}
		for _, s := range *labels {
			if e := requiredText(s, 512); e != nil {
				return e
			}
		}
	}
	if date != nil && *datetime != nil {
		return errors.New("provide either due_date or due_datetime")
	}
	if date != nil {
		if _, e := time.Parse("2006-01-02", *date); e != nil {
			return errors.New("due_date must use YYYY-MM-DD")
		}
	}
	if *datetime != nil {
		v, e := time.Parse(time.RFC3339, **datetime)
		if e != nil {
			return errors.New("due_datetime must be RFC 3339 with an explicit UTC offset")
		}
		s := v.UTC().Format(time.RFC3339)
		*datetime = &s
	}
	if order != nil && (int64(*order) < -2147483648 || int64(*order) > 2147483647) {
		return errors.New("order must fit a signed 32-bit integer")
	}
	return nil
}
func (a *api) createTask(ctx context.Context, in *CreateTaskIn) (*CreateTaskOut, error) {
	if e := requiredText(in.Content, 10000); e != nil {
		return nil, e
	}
	v := in.Details
	for _, id := range []*string{v.ProjectID, v.SectionID, v.ParentID} {
		if id != nil {
			if e := requireID(*id); e != nil {
				return nil, e
			}
		}
	}
	if e := validateTaskFields(v.Priority, v.Labels, v.DueDate, &v.DueDatetime, v.Order); e != nil {
		return nil, e
	}
	payload := struct {
		Content string `json:"content"`
		schema.TaskDetails
	}{in.Content, v}
	b, e := a.call(ctx, "POST", "/tasks", nil, payload, 200)
	if e != nil {
		return nil, e
	}
	task, e := taskOutput(b)
	if e != nil {
		return nil, e
	}
	return &CreateTaskOut{Task: task}, nil
}
func (a *api) updateTask(ctx context.Context, in *UpdateTaskIn) (*UpdateTaskOut, error) {
	if e := requireID(in.TaskID); e != nil {
		return nil, e
	}
	v := in.Changes
	if v.Content != nil {
		if e := requiredText(*v.Content, 10000); e != nil {
			return nil, e
		}
	}
	if e := validateTaskFields(v.Priority, v.Labels, v.DueDate, &v.DueDatetime, v.ChildOrder); e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(v)
	if string(raw) == "{}" {
		return nil, errors.New("provide at least one changed field")
	}
	b, e := a.call(ctx, "POST", "/tasks/"+in.TaskID, nil, v, 200)
	if e != nil {
		return nil, e
	}
	task, e := taskOutput(b)
	if e != nil {
		return nil, e
	}
	return &UpdateTaskOut{Task: task}, nil
}
func (a *api) closeTask(ctx context.Context, in *CloseTaskIn) (*CloseTaskOut, error) {
	if e := requireID(in.TaskID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "POST", "/tasks/"+in.TaskID+"/close", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return nil, badResponse()
	}
	return &CloseTaskOut{Accepted: true}, nil
}
