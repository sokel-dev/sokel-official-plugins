package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/vikunja/schema"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func positiveID(s string) bool {
	n, e := strconv.ParseInt(s, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == s
}
func projectID(s string) bool {
	n, e := strconv.ParseInt(s, 10, 64)
	return e == nil && n != 0 && strconv.FormatInt(n, 10) == s
}
func projectResource(id string) (string, error) {
	if !projectID(id) {
		return "", errors.New("Project ID must be a nonzero signed 64-bit decimal string; negative IDs identify virtual projects.")
	}
	return "/api/v2/projects/" + id, nil
}
func number(s json.Number, zero bool) (int64, error) {
	n, e := strconv.ParseInt(s.String(), 10, 64)
	if e != nil || n < 0 || (!zero && n == 0) || strconv.FormatInt(n, 10) != s.String() {
		return 0, errors.New("Vikunja returned an invalid numeric identifier or count.")
	}
	return n, nil
}
func textValue(s string, max int, empty bool) bool {
	return utf8.ValidString(s) && len(s) <= max && !strings.ContainsRune(s, 0) && (empty || strings.TrimSpace(s) != "")
}
func resource(kind, id string) (string, error) {
	if !positiveID(id) {
		return "", errors.New("Resource IDs must be positive signed 64-bit decimal strings.")
	}
	return "/api/v2/" + kind + "/" + id, nil
}
func projectValue(b []byte, want string) (schema.Project, error) {
	var v struct {
		ID          json.Number `json:"id"`
		Title       string      `json:"title"`
		Description string      `json:"description"`
		Archived    *bool       `json:"is_archived"`
	}
	if decodeJSON(b, &v) != nil || v.Title == "" || v.Archived == nil {
		return schema.Project{}, errors.New("Vikunja returned incomplete project metadata.")
	}
	if !projectID(v.ID.String()) {
		return schema.Project{}, errors.New("Vikunja returned an invalid project ID.")
	}
	if want != "" && v.ID.String() != want {
		return schema.Project{}, errors.New("Vikunja returned a different project ID.")
	}
	return schema.Project{ID: v.ID.String(), Title: v.Title, Description: v.Description, IsArchived: *v.Archived}, nil
}
func taskValue(b []byte, want, project string) (schema.Task, error) {
	var v struct {
		ID          json.Number `json:"id"`
		ProjectID   json.Number `json:"project_id"`
		Title       string      `json:"title"`
		Description string      `json:"description"`
		Done        *bool       `json:"done"`
		Priority    *int        `json:"priority"`
		DueDate     string      `json:"due_date"`
	}
	if decodeJSON(b, &v) != nil || v.Title == "" || v.Done == nil || v.Priority == nil {
		return schema.Task{}, errors.New("Vikunja returned incomplete task metadata.")
	}
	if _, e := number(v.ID, false); e != nil {
		return schema.Task{}, e
	}
	if _, e := number(v.ProjectID, false); e != nil {
		return schema.Task{}, e
	}
	if (want != "" && v.ID.String() != want) || (positiveID(project) && v.ProjectID.String() != project) {
		return schema.Task{}, errors.New("Vikunja returned a different task or project ID.")
	}
	if _, e := time.Parse(time.RFC3339Nano, v.DueDate); e != nil {
		return schema.Task{}, errors.New("Vikunja returned an invalid task due date.")
	}
	return schema.Task{ID: v.ID.String(), ProjectID: v.ProjectID.String(), Title: v.Title, Description: v.Description, Done: *v.Done, Priority: *v.Priority, DueDate: v.DueDate}, nil
}
func pageQuery(p schema.Page) (url.Values, int, int, error) {
	page, size := 1, 50
	if p.Number != nil {
		page = *p.Number
	}
	if p.Size != nil {
		size = *p.Size
	}
	if page < 1 || page > 1000000 || size < 1 || size > 1000 {
		return nil, 0, 0, errors.New("Page must be 1–1000000 and requested size 1–1000.")
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(size)}}
	if p.Query != nil {
		if !textValue(*p.Query, 16384, true) {
			return nil, 0, 0, errors.New("Search text must be valid UTF-8 within 16 KiB.")
		}
		q.Set("q", *p.Query)
	}
	return q, page, size, nil
}
func pageValue(b []byte, page, size int, projects bool) ([]json.RawMessage, string, int, error) {
	var v struct {
		Items      []json.RawMessage `json:"items"`
		Total      json.Number       `json:"total"`
		Page       *int              `json:"page"`
		PerPage    *int              `json:"per_page"`
		TotalPages *int              `json:"total_pages"`
	}
	if decodeJSON(b, &v) != nil || v.Page == nil || v.PerPage == nil || v.TotalPages == nil || *v.Page != page || *v.PerPage < 1 || *v.PerPage > size || *v.TotalPages < 0 {
		return nil, "", 0, errors.New("Vikunja returned invalid pagination metadata.")
	}
	if requireKeys(b, "page", "per_page", "total", "total_pages") != nil {
		return nil, "", 0, errors.New("Vikunja returned incomplete pagination metadata.")
	}
	var fields map[string]json.RawMessage
	_ = decodeJSON(b, &fields)
	if _, ok := fields["items"]; !ok {
		return nil, "", 0, errors.New("Vikunja omitted the result list.")
	}
	total, e := number(v.Total, true)
	if e != nil {
		return nil, "", 0, e
	}
	expected := total / int64(*v.PerPage)
	if total%int64(*v.PerPage) != 0 {
		expected++
	}
	if int64(*v.TotalPages) != expected {
		return nil, "", 0, errors.New("Vikunja returned an inconsistent page count.")
	}
	regularCount := len(v.Items)
	if projects {
		for _, raw := range v.Items {
			var id struct {
				ID json.Number `json:"id"`
			}
			if decodeJSON(raw, &id) != nil || !projectID(id.ID.String()) {
				return nil, "", 0, errors.New("Vikunja returned an invalid project ID.")
			}
			if strings.HasPrefix(id.ID.String(), "-") {
				regularCount--
			}
		}
	}
	if regularCount > *v.PerPage {
		return nil, "", 0, errors.New("Vikunja returned more ordinary results than the page size.")
	}
	next := 0
	if page < *v.TotalPages {
		if regularCount == 0 || page == 1000000 {
			return nil, "", 0, errors.New("Vikunja pagination cannot advance safely.")
		}
		next = page + 1
	}
	if int64(regularCount) > total {
		return nil, "", 0, errors.New("Vikunja returned an inconsistent result count.")
	}
	return v.Items, v.Total.String(), next, nil
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	one := 1
	_, e := c.list_projects(ctx, &ListProjectsIn{Page: schema.Page{Size: &one}})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Vikunja accepted the token and returned a project page."}, nil
}
func (c *client) list_projects(ctx context.Context, in *ListProjectsIn) (*ListProjectsOut, error) {
	q, page, size, e := pageQuery(in.Page)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", "/api/v2/projects", q, nil, 200)
	if e != nil {
		return nil, e
	}
	rows, total, next, e := pageValue(b, page, size, true)
	if e != nil {
		return nil, e
	}
	out := &ListProjectsOut{Projects: []schema.Project{}, Total: total, NextPage: next, HasMore: next != 0}
	seen := map[string]bool{}
	for _, raw := range rows {
		p, e := projectValue(raw, "")
		if e != nil {
			return nil, e
		}
		if seen[p.ID] {
			return nil, errors.New("Vikunja returned a repeated project ID.")
		}
		seen[p.ID] = true
		out.Projects = append(out.Projects, p)
	}
	return out, nil
}
func (c *client) get_project(ctx context.Context, in *GetProjectIn) (*GetProjectOut, error) {
	p, e := projectResource(in.ProjectID)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", p, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	v, e := projectValue(b, in.ProjectID)
	if e != nil {
		return nil, e
	}
	return &GetProjectOut{Project: v}, nil
}
func (c *client) create_project(ctx context.Context, in *CreateProjectIn) (*CreateProjectOut, error) {
	if !textValue(in.Title, 250, false) || !textValue(in.Description, 256<<10, true) {
		return nil, errors.New("Provide a title within 250 bytes and optional description HTML within 256 KiB.")
	}
	body := map[string]string{"title": in.Title}
	if in.Description != "" {
		body["description"] = in.Description
	}
	b, e := c.request(ctx, "POST", "/api/v2/projects", nil, body, 201)
	if e != nil {
		return nil, e
	}
	v, e := projectValue(b, "")
	if e != nil {
		return nil, e
	}
	if !positiveID(v.ID) {
		return nil, errors.New("Vikunja returned a virtual project for creation.")
	}
	return &CreateProjectOut{Project: v}, nil
}
func (c *client) list_tasks(ctx context.Context, in *ListTasksIn) (*ListTasksOut, error) {
	p, e := projectResource(in.ProjectID)
	if e != nil {
		return nil, e
	}
	q, page, size, e := pageQuery(in.Page)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", p+"/tasks", q, nil, 200)
	if e != nil {
		return nil, e
	}
	rows, total, next, e := pageValue(b, page, size, false)
	if e != nil {
		return nil, e
	}
	out := &ListTasksOut{Tasks: []schema.Task{}, Total: total, NextPage: next, HasMore: next != 0}
	seen := map[string]bool{}
	for _, raw := range rows {
		v, e := taskValue(raw, "", in.ProjectID)
		if e != nil {
			return nil, e
		}
		if seen[v.ID] {
			return nil, errors.New("Vikunja returned a repeated task ID.")
		}
		seen[v.ID] = true
		out.Tasks = append(out.Tasks, v)
	}
	return out, nil
}
func (c *client) get_task(ctx context.Context, in *GetTaskIn) (*GetTaskOut, error) {
	p, e := resource("tasks", in.TaskID)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", p, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	v, e := taskValue(b, in.TaskID, "")
	if e != nil {
		return nil, e
	}
	return &GetTaskOut{Task: v}, nil
}
func taskBody(f schema.TaskFields) (map[string]any, error) {
	body := map[string]any{}
	if f.Title != nil {
		if !textValue(*f.Title, 250, false) {
			return nil, errors.New("Task title must be nonempty valid UTF-8 within 250 bytes.")
		}
		body["title"] = *f.Title
	}
	if f.Description != nil {
		if !textValue(*f.Description, 256<<10, true) {
			return nil, errors.New("Task description must be valid HTML text within 256 KiB.")
		}
		body["description"] = *f.Description
	}
	if f.Done != nil {
		body["done"] = *f.Done
	}
	if f.Priority != nil {
		if *f.Priority < 0 || *f.Priority > 5 {
			return nil, errors.New("Task priority must be 0–5.")
		}
		body["priority"] = *f.Priority
	}
	if f.DueDate != nil {
		if _, e := time.Parse(time.RFC3339Nano, *f.DueDate); e != nil {
			return nil, errors.New("Due date must be an RFC3339 timestamp; use 0001-01-01T00:00:00Z to clear it.")
		}
		body["due_date"] = *f.DueDate
	}
	return body, nil
}
func (c *client) create_task(ctx context.Context, in *CreateTaskIn) (*CreateTaskOut, error) {
	p, e := resource("projects", in.ProjectID)
	if e != nil {
		return nil, e
	}
	f := in.Fields
	body, e := taskBody(schema.TaskFields{Title: &in.Title, Description: f.Description, Done: f.Done, Priority: f.Priority, DueDate: f.DueDate})
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "POST", p+"/tasks", nil, body, 201)
	if e != nil {
		return nil, e
	}
	v, e := taskValue(b, "", in.ProjectID)
	if e != nil {
		return nil, e
	}
	return &CreateTaskOut{Task: v}, nil
}
func (c *client) update_task(ctx context.Context, in *UpdateTaskIn) (*UpdateTaskOut, error) {
	p, e := resource("tasks", in.TaskID)
	if e != nil {
		return nil, e
	}
	body, e := taskBody(in.Fields)
	if e != nil {
		return nil, e
	}
	if len(body) == 0 {
		return nil, errors.New("Provide at least one task field to change.")
	}
	b, e := c.request(ctx, "PATCH", p, nil, body, 200)
	if e != nil {
		return nil, e
	}
	v, e := taskValue(b, in.TaskID, "")
	if e != nil {
		return nil, e
	}
	return &UpdateTaskOut{Task: v}, nil
}
func (c *client) add_comment(ctx context.Context, in *AddCommentIn) (*AddCommentOut, error) {
	p, e := resource("tasks", in.TaskID)
	if e != nil {
		return nil, e
	}
	if !textValue(in.Comment, 256<<10, false) {
		return nil, errors.New("Comment HTML must be nonempty valid UTF-8 within 256 KiB.")
	}
	b, e := c.request(ctx, "POST", p+"/comments", nil, map[string]string{"comment": in.Comment}, 201)
	if e != nil {
		return nil, e
	}
	var v struct {
		ID      json.Number `json:"id"`
		Comment string      `json:"comment"`
		Created string      `json:"created"`
	}
	if decodeJSON(b, &v) != nil || v.Comment == "" {
		return nil, errors.New("Vikunja returned an invalid comment receipt.")
	}
	if _, e = number(v.ID, false); e != nil {
		return nil, e
	}
	if _, e = time.Parse(time.RFC3339Nano, v.Created); e != nil {
		return nil, errors.New("Vikunja returned an invalid comment timestamp.")
	}
	return &AddCommentOut{Comment: schema.Comment{ID: v.ID.String(), Comment: v.Comment, Created: v.Created}}, nil
}
