package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/vikunja/schema"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type captureEntry struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Query    string          `json:"query"`
	Request  json.RawMessage `json:"request,omitempty"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response,omitempty"`
}

func TestLocalServiceAcceptance(t *testing.T) {
	target := os.Getenv("SOKEL_LIVE_VIKUNJA_URL")
	if target == "" {
		t.Skip("set an explicit isolated local acceptance URL")
	}
	u, e := url.Parse(target)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("requires isolated http://127.0.0.1:port")
	}
	token := os.Getenv("SOKEL_LIVE_VIKUNJA_TOKEN")
	if token == "" {
		t.Fatal("set the isolated test token")
	}
	c := newClient(Cred{AccessToken: token})
	c.baseURL = target
	captures := []captureEntry{}
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		res, e := http.DefaultTransport.RoundTrip(r)
		if e != nil {
			return nil, e
		}
		raw, e := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
		res.Body.Close()
		if e != nil {
			return nil, e
		}
		res.Body = io.NopCloser(bytes.NewReader(raw))
		captures = append(captures, captureEntry{r.Method, r.URL.Path, r.URL.RawQuery, json.RawMessage(b), res.StatusCode, json.RawMessage(raw)})
		return res, nil
	})
	t.Cleanup(func() {
		dir := os.Getenv("SOKEL_LIVE_FIXTURE_DIR")
		if dir == "" {
			return
		}
		abs, e := filepath.Abs(dir)
		if e != nil || (!strings.HasPrefix(abs, "/tmp/") && !strings.HasPrefix(abs, "/private/tmp/")) {
			t.Error("captures require /tmp directory")
			return
		}
		b, e := json.MarshalIndent(captures, "", "  ")
		if e != nil {
			t.Error(e)
			return
		}
		if e = os.WriteFile(filepath.Join(abs, "vikunja-operation-captures.json"), append(b, '\n'), 0600); e != nil {
			t.Error(e)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, e := c.health_check(ctx, &HealthCheckIn{})
	if e != nil || h == nil || !h.OK {
		t.Fatal(h, e)
	}
	title := fmt.Sprintf("sokel-wave7-%d", time.Now().UnixNano())
	project, e := c.create_project(ctx, &CreateProjectIn{Title: title, Description: "<p>Local acceptance</p>"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, e := c.request(ctx, "DELETE", "/api/v2/projects/"+project.Project.ID, nil, nil, 204)
		if e != nil {
			t.Error(e)
		}
	})
	p, e := c.get_project(ctx, &GetProjectIn{ProjectID: project.Project.ID})
	if e != nil || p.Project.Title != title {
		t.Fatal(p, e)
	}
	one := 1
	ps, e := c.list_projects(ctx, &ListProjectsIn{Page: schema.Page{Size: &one, Query: &title}})
	if e != nil || len(ps.Projects) != 1 || ps.Projects[0].ID != project.Project.ID {
		t.Fatal(ps, e)
	}
	description := "<p>Keep this description</p>"
	priority := 3
	due := "2027-01-01T12:00:00Z"
	first, e := c.create_task(ctx, &CreateTaskIn{ProjectID: project.Project.ID, Title: "First", Fields: schema.TaskOptions{Description: &description, Priority: &priority, DueDate: &due}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.create_task(ctx, &CreateTaskIn{ProjectID: project.Project.ID, Title: "Second"})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := c.list_tasks(ctx, &ListTasksIn{ProjectID: project.Project.ID, Page: schema.Page{Size: &one}})
	if e != nil || len(rows.Tasks) != 1 || !rows.HasMore {
		t.Fatal(rows, e)
	}
	last, e := c.list_tasks(ctx, &ListTasksIn{ProjectID: project.Project.ID, Page: schema.Page{Size: &one, Number: &rows.NextPage}})
	if e != nil || len(last.Tasks) != 1 || last.HasMore || last.Tasks[0].ID == rows.Tasks[0].ID {
		t.Fatal(last, e)
	}
	done := true
	changed, e := c.update_task(ctx, &UpdateTaskIn{TaskID: first.Task.ID, Fields: schema.TaskFields{Done: &done}})
	if e != nil || !changed.Task.Done {
		t.Fatal(changed, e)
	}
	done = false
	priority = 0
	due = "0001-01-01T00:00:00Z"
	changed, e = c.update_task(ctx, &UpdateTaskIn{TaskID: first.Task.ID, Fields: schema.TaskFields{Done: &done, Priority: &priority, DueDate: &due}})
	if e != nil || changed.Task.Done || changed.Task.Priority != 0 || changed.Task.Description != description {
		t.Fatal(changed, e)
	}
	got, e := c.get_task(ctx, &GetTaskIn{TaskID: first.Task.ID})
	if e != nil || got.Task.Done || got.Task.DueDate != due {
		t.Fatal(got, e)
	}
	comment, e := c.add_comment(ctx, &AddCommentIn{TaskID: first.Task.ID, Comment: "<p>Local acceptance comment</p>"})
	if e != nil || comment.Comment.ID == "" || !strings.Contains(comment.Comment.Comment, "Local acceptance") {
		t.Fatal(comment, e)
	}
}
