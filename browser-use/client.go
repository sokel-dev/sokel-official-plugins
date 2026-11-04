package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func runPath(id string) (string, error) {
	if !uuid.MatchString(id) {
		return "", errors.New("Provide a Browser Use V4 run UUID.")
	}
	return "/api/v4/runs/" + id, nil
}
func state(s string) (bool, error) {
	switch s {
	case "queued", "dispatching", "running":
		return false, nil
	case "completed", "failed", "cancelled":
		return true, nil
	}
	return false, errors.New("Browser Use returned a missing or unsupported run status.")
}
func pageLimit(n, def, max int64) (int64, error) {
	if n == 0 {
		return def, nil
	}
	if n < 1 || n > max {
		return 0, errors.New("Page size is outside the documented plugin range.")
	}
	return n, nil
}

type runWire struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"sessionId"`
	WorkspaceID  string          `json:"workspaceId"`
	Status       string          `json:"status"`
	Model        string          `json:"model"`
	Result       string          `json:"result"`
	Error        string          `json:"error"`
	Output       json.RawMessage `json:"output"`
	TotalCostUSD string          `json:"totalCostUsd"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

func (w runWire) convert() (Run, error) {
	terminal, e := state(w.Status)
	if e != nil {
		return Run{}, e
	}
	if !uuid.MatchString(w.ID) || !uuid.MatchString(w.SessionID) || w.Model == "" {
		return Run{}, errors.New("Browser Use returned incomplete run metadata.")
	}
	out := "null"
	if len(w.Output) > 0 {
		out = string(w.Output)
	}
	return Run{ID: w.ID, SessionID: w.SessionID, WorkspaceID: w.WorkspaceID, Status: w.Status, Terminal: terminal, Model: w.Model, Result: w.Result, Error: w.Error, OutputJSON: out, TotalCostUsd: w.TotalCostUSD, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}, nil
}
func (c *client) healthCheck(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := c.listRuns(ctx, &ListRunsIn{Limit: 1})
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Browser Use V4 authenticated read succeeded. No browser run was created."}, nil
}
func (c *client) createRun(ctx context.Context, in *CreateRunIn) (*CreateRunOut, error) {
	if strings.TrimSpace(in.Task) == "" || len(in.Task) > 65536 {
		return nil, errors.New("Task must contain 1 to 65536 bytes.")
	}
	if math.IsNaN(in.MaxCostUsd) || math.IsInf(in.MaxCostUsd, 0) || in.MaxCostUsd <= 0 {
		return nil, errors.New("Run cost cap must be a positive finite USD amount.")
	}
	b := map[string]any{"task": in.Task, "maxCostUsd": in.MaxCostUsd}
	if in.Model != "" {
		if !tokenOK(in.Model, 128) {
			return nil, errors.New("Provide a valid model name or leave it empty.")
		}
		b["model"] = in.Model
	}
	for k, id := range map[string]string{"sessionId": in.SessionID, "workspaceId": in.WorkspaceID} {
		if id != "" {
			if !uuid.MatchString(id) {
				return nil, errors.New("Session and workspace IDs must be V4 UUIDs.")
			}
			b[k] = id
		}
	}
	if in.OutputSchema != nil {
		b["outputSchema"] = in.OutputSchema
	}
	raw, e := c.request(ctx, "POST", "/api/v4/runs", nil, b)
	if e != nil {
		return nil, e
	}
	var w runWire
	if e = decode(raw, &w); e != nil {
		return nil, e
	}
	if _, e = state(w.Status); e != nil {
		return nil, e
	}
	if !uuid.MatchString(w.ID) || !uuid.MatchString(w.SessionID) || !uuid.MatchString(w.WorkspaceID) || w.Model == "" {
		return nil, errors.New("Browser Use returned an incomplete creation receipt. Inspect existing runs before retrying.")
	}
	return &CreateRunOut{Run: RunReceipt{ID: w.ID, SessionID: w.SessionID, WorkspaceID: w.WorkspaceID, Status: w.Status, Model: w.Model}}, nil
}
func (c *client) readRun(ctx context.Context, method, id, suffix string) (Run, error) {
	p, e := runPath(id)
	if e != nil {
		return Run{}, e
	}
	b, e := c.request(ctx, method, p+suffix, nil, nil)
	if e != nil {
		return Run{}, e
	}
	var w runWire
	if e = decode(b, &w); e != nil {
		return Run{}, e
	}
	if w.ID != id {
		return Run{}, errors.New("Browser Use returned a different run ID.")
	}
	return w.convert()
}
func (c *client) getRun(ctx context.Context, in *GetRunIn) (*GetRunOut, error) {
	r, e := c.readRun(ctx, "GET", in.RunID, "")
	if e != nil {
		return nil, e
	}
	return &GetRunOut{Run: r}, nil
}
func (c *client) cancelRun(ctx context.Context, in *CancelRunIn) (*CancelRunOut, error) {
	r, e := c.readRun(ctx, "POST", in.RunID, "/cancel")
	if e != nil {
		return nil, e
	}
	return &CancelRunOut{Run: r}, nil
}
func (c *client) getRunStatus(ctx context.Context, in *GetRunStatusIn) (*GetRunStatusOut, error) {
	p, e := runPath(in.RunID)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", p+"/status", nil, nil)
	if e != nil {
		return nil, e
	}
	var w struct {
		Status string `json:"status"`
	}
	if e = decode(b, &w); e != nil {
		return nil, e
	}
	terminal, e := state(w.Status)
	if e != nil {
		return nil, e
	}
	return &GetRunStatusOut{Status: w.Status, Terminal: terminal}, nil
}
func (c *client) listRuns(ctx context.Context, in *ListRunsIn) (*ListRunsOut, error) {
	limit, e := pageLimit(in.Limit, 50, 100)
	if e != nil {
		return nil, e
	}
	if !cursorOK(in.Cursor) {
		return nil, errors.New("Invalid page cursor.")
	}
	q := url.Values{"limit": {strconv.FormatInt(limit, 10)}}
	if in.Cursor != "" {
		q.Set("cursor", in.Cursor)
	}
	if in.SessionID != "" {
		if !uuid.MatchString(in.SessionID) {
			return nil, errors.New("Session ID must be a V4 UUID.")
		}
		q.Set("sessionId", in.SessionID)
	}
	b, e := c.request(ctx, "GET", "/api/v4/runs", q, nil)
	if e != nil {
		return nil, e
	}
	var w struct {
		Runs       []runWire `json:"runs"`
		NextCursor string    `json:"nextCursor"`
		HasMore    bool      `json:"hasMore"`
	}
	if e = decode(b, &w); e != nil {
		return nil, e
	}
	if w.Runs == nil || len(w.Runs) > int(limit) || !cursorOK(w.NextCursor) || (w.HasMore && (w.NextCursor == "" || w.NextCursor == in.Cursor)) {
		return nil, errors.New("Browser Use returned an invalid run page or non-advancing cursor.")
	}
	out := &ListRunsOut{Runs: []Run{}, NextCursor: w.NextCursor, HasMore: w.HasMore}
	for _, v := range w.Runs {
		r, e := v.convert()
		if e != nil {
			return nil, e
		}
		out.Runs = append(out.Runs, r)
	}
	return out, nil
}
func (c *client) listRunEvents(ctx context.Context, in *ListRunEventsIn) (*ListRunEventsOut, error) {
	p, e := runPath(in.RunID)
	if e != nil {
		return nil, e
	}
	limit, e := pageLimit(in.Limit, 200, 1000)
	if e != nil {
		return nil, e
	}
	if in.After < 0 {
		return nil, errors.New("Event cursor must be zero or positive.")
	}
	q := url.Values{"after": {strconv.FormatInt(in.After, 10)}, "limit": {strconv.FormatInt(limit, 10)}, "include_output": {strconv.FormatBool(in.IncludeOutput)}}
	b, e := c.request(ctx, "GET", p+"/events", q, nil)
	if e != nil {
		return nil, e
	}
	var w struct {
		Events []struct {
			RunID string         `json:"runId"`
			ID    int64          `json:"id"`
			TS    string         `json:"ts"`
			Type  string         `json:"type"`
			Data  map[string]any `json:"data"`
		} `json:"events"`
		NextAfter *int64 `json:"nextAfter"`
		HasMore   bool   `json:"hasMore"`
	}
	if e = decode(b, &w); e != nil {
		return nil, e
	}
	if w.Events == nil || len(w.Events) > int(limit) {
		return nil, errors.New("Browser Use returned an invalid event page.")
	}
	out := &ListRunEventsOut{Events: []RunEvent{}, NextAfter: in.After, HasMore: w.HasMore}
	last := in.After
	for _, v := range w.Events {
		if v.RunID != in.RunID || v.ID <= last || v.Type == "" || v.Data == nil {
			return nil, errors.New("Browser Use returned invalid or out-of-order run events.")
		}
		last = v.ID
		out.Events = append(out.Events, RunEvent{RunID: v.RunID, ID: v.ID, Timestamp: v.TS, Type: v.Type, Data: v.Data})
	}
	if w.NextAfter != nil {
		out.NextAfter = *w.NextAfter
	} else {
		out.NextAfter = last
	}
	if out.NextAfter < last || (w.HasMore && out.NextAfter <= in.After) {
		return nil, errors.New("Browser Use returned a non-advancing event cursor.")
	}
	return out, nil
}
