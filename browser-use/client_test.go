package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, fn func(*http.Request) (int, string)) *client {
	t.Helper()
	c := newClient(Cred{APIKey: "fixture-key"})
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.browser-use.com" || r.Header.Get("X-Browser-Use-API-Key") != "fixture-key" || r.Header.Get("Authorization") != "" {
			t.Error("Incorrect origin or authentication")
		}
		status, body := fn(r)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"17"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return c
}

const runID = "11111111-1111-4111-8111-111111111111"
const sessionID = "22222222-2222-4222-8222-222222222222"

func TestCreateRunWireAndReceipt(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/api/v4/runs" || string(b) != `{"maxCostUsd":0.5,"task":"Read 中文"}` {
			t.Errorf("request %s %s %s", r.Method, r.URL.Path, b)
		}
		return 200, `{"id":"` + runID + `","sessionId":"` + sessionID + `","workspaceId":"33333333-3333-4333-8333-333333333333","status":"queued","model":"service-model","eventsUrl":"https://api.browser-use.com/api/v4/runs/` + runID + `/events"}`
	})
	out, e := c.createRun(context.Background(), &CreateRunIn{Task: "Read 中文", MaxCostUsd: 0.5})
	if e != nil {
		t.Fatal(e)
	}
	if out.Run.ID != runID || out.Run.Status != "queued" {
		t.Fatalf("receipt %+v", out)
	}
}
func TestListRunsPreservesOpaqueCursor(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("cursor") != "a+/=?" || r.URL.Query().Get("limit") != "3" {
			t.Error("cursor changed")
		}
		return 200, `{"runs":[],"nextCursor":"next+/=","hasMore":true}`
	})
	out, e := c.listRuns(context.Background(), &ListRunsIn{Cursor: "a+/=?", Limit: 3})
	if e != nil {
		t.Fatal(e)
	}
	if out.NextCursor != "next+/=" || !out.HasMore {
		t.Fatalf("page %+v", out)
	}
}
func TestStatusTerminalAndResultJSON(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) { return 200, `{"status":"completed"}` })
	s, e := c.getRunStatus(context.Background(), &GetRunStatusIn{RunID: runID})
	if e != nil || !s.Terminal {
		t.Fatalf("status %v %v", s, e)
	}
	c = fixture(t, func(r *http.Request) (int, string) {
		return 200, `{"id":"` + runID + `","sessionId":"` + sessionID + `","status":"completed","model":"m","result":"done","output":[0,false,"中文"],"totalCostUsd":"0.123"}`
	})
	out, e := c.getRun(context.Background(), &GetRunIn{RunID: runID})
	if e != nil {
		t.Fatal(e)
	}
	if out.Run.OutputJSON != `[0,false,"中文"]` || !out.Run.Terminal || out.Run.TotalCostUsd != "0.123" {
		t.Fatalf("result %+v", out)
	}
}
