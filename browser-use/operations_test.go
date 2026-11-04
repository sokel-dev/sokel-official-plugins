package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

const summaryFixture = `{"id":"` + runID + `","sessionId":"` + sessionID + `","workspaceId":"33333333-3333-4333-8333-333333333333","status":"completed","model":"m","result":"ok","output":{"x":false},"totalCostUsd":"0"}`

func TestEventDataLargeIntegerThroughGeneratedSink(t *testing.T) {
	c := fixture(t, func(*http.Request) (int, string) {
		return 200, `{"events":[{"runId":"` + runID + `","id":1,"type":"tool.output","data":{"large":9007199254740993,"fraction":0.125}}],"nextAfter":1,"hasMore":false}`
	})
	v := invoke(t, c, "list_run_events", `{"run_id":"`+runID+`","include_output":true}`)
	b, err := json.Marshal(v)
	if err != nil || !strings.Contains(string(b), `"large":9007199254740993`) || !strings.Contains(string(b), `"fraction":0.125`) {
		t.Fatalf("event data lost numeric precision: %s %v", b, err)
	}
}

func TestAllGeneratedOperations(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			return 200, `{"status":"running"}`
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			if r.Method != "POST" {
				t.Error("cancel method")
			}
			return 200, summaryFixture
		case strings.HasSuffix(r.URL.Path, "/events"):
			if r.URL.Query().Get("include_output") != "false" || r.URL.Query().Get("after") != "2" {
				t.Error("event query")
			}
			return 200, `{"events":[{"runId":"` + runID + `","id":3,"ts":"2026-11-04T00:00:00Z","type":"run.completed","data":{"x":false}}],"nextAfter":3,"hasMore":false}`
		case r.URL.Path == "/api/v4/runs":
			if r.Method == "POST" {
				b, _ := io.ReadAll(r.Body)
				var v map[string]any
				json.Unmarshal(b, &v)
				if v["sessionId"] != sessionID || v["outputSchema"] == nil || v["model"] != "bu-fast" {
					t.Error("optional fields lost")
				}
				return 200, summaryFixture
			}
			return 200, `{"runs":[` + summaryFixture + `],"hasMore":false}`
		default:
			return 200, summaryFixture
		}
	})
	if v := invoke(t, c, "health_check", `{}`); v["ok"] != true {
		t.Fatal(v)
	}
	invoke(t, c, "create_run", `{"task":"Read only","max_cost_usd":1,"model":"bu-fast","session_id":"`+sessionID+`","workspace_id":"33333333-3333-4333-8333-333333333333","output_schema":{"type":"object"}}`)
	invoke(t, c, "get_run", `{"run_id":"`+runID+`"}`)
	invoke(t, c, "cancel_run", `{"run_id":"`+runID+`"}`)
	invoke(t, c, "list_runs", `{"session_id":"`+sessionID+`"}`)
	if v := invoke(t, c, "get_run_status", `{"run_id":"`+runID+`"}`); v["terminal"] != false {
		t.Fatal(v)
	}
	if v := invoke(t, c, "list_run_events", `{"run_id":"`+runID+`","after":2,"include_output":false}`); v["next_after"] != int64(3) {
		t.Fatal(v)
	}
}
func TestInvalidInputsPreventNetwork(t *testing.T) {
	c := fixture(t, func(*http.Request) (int, string) { t.Fatal("unexpected network"); return 500, "" })
	ctx := context.Background()
	for _, v := range []CreateRunIn{{Task: ""}, {Task: strings.Repeat("x", 65537), MaxCostUsd: 1}, {Task: "x", MaxCostUsd: 0}, {Task: "x", MaxCostUsd: -1}, {Task: "x", MaxCostUsd: math.NaN()}, {Task: "x", MaxCostUsd: math.Inf(1)}, {Task: "x", MaxCostUsd: 1, Model: "bad model"}, {Task: "x", MaxCostUsd: 1, SessionID: "bad"}, {Task: "x", MaxCostUsd: 1, WorkspaceID: "bad"}} {
		if _, e := c.createRun(ctx, &v); e == nil {
			t.Fatal("invalid create")
		}
	}
	for _, id := range []string{"", "../x", "%2f", "https://other.example"} {
		if _, e := c.getRun(ctx, &GetRunIn{RunID: id}); e == nil {
			t.Fatal("invalid run")
		}
		if _, e := c.getRunStatus(ctx, &GetRunStatusIn{RunID: id}); e == nil {
			t.Fatal("invalid status")
		}
		if _, e := c.listRunEvents(ctx, &ListRunEventsIn{RunID: id}); e == nil {
			t.Fatal("invalid events")
		}
	}
	for _, v := range []ListRunsIn{{Limit: -1}, {Limit: 101}, {Cursor: "x\n"}, {SessionID: "bad"}} {
		if _, e := c.listRuns(ctx, &v); e == nil {
			t.Fatal("invalid list")
		}
	}
	for _, v := range []ListRunEventsIn{{RunID: runID, After: -1}, {RunID: runID, Limit: 1001}} {
		if _, e := c.listRunEvents(ctx, &v); e == nil {
			t.Fatal("invalid events")
		}
	}
}
func TestMalformedPagesAndReceipts(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{`null`, `{}`, `{"runs":null}`, `{"runs":[],"hasMore":true}`, `{"runs":[],"hasMore":true,"nextCursor":"same"}`, `{"runs":[{}]}`, `{"runs":[],"nextCursor":"x\n"}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := c.listRuns(ctx, &ListRunsIn{Cursor: "same"}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	for _, body := range []string{`{}`, `{"status":"unknown"}`, `{"id":"wrong","status":"completed"}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := c.getRun(ctx, &GetRunIn{RunID: runID}); e == nil {
			t.Fatal("bad run")
		}
		if _, e := c.createRun(ctx, &CreateRunIn{Task: "x", MaxCostUsd: 1}); e == nil {
			t.Fatal("bad receipt")
		}
	}
	for _, body := range []string{`{}`, `{"events":[],"hasMore":true}`, `{"events":[],"nextAfter":1}`, `{"events":[{"runId":"` + runID + `","id":2,"type":"x","data":{}}]}`, `{"events":[{"runId":"other","id":3,"type":"x","data":{}}]}`, `{"events":[{"runId":"` + runID + `","id":3,"type":"x"}]}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := c.listRunEvents(ctx, &ListRunEventsIn{RunID: runID, After: 2}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	c := fixture(t, func(*http.Request) (int, string) { return 200, `{"events":[],"nextAfter":null,"hasMore":false}` })
	v, e := c.listRunEvents(ctx, &ListRunEventsIn{RunID: runID, After: 9})
	if e != nil || v.NextAfter != 9 {
		t.Fatal(v, e)
	}
	c = fixture(t, func(*http.Request) (int, string) { return 401, `{"detail":"fixture-key"}` })
	v2, e := c.healthCheck(ctx, &HealthCheckIn{})
	if e != nil || v2.OK {
		t.Fatal("false health")
	}
	for _, s := range []string{"queued", "dispatching", "running", "completed", "failed", "cancelled"} {
		if _, e := state(s); e != nil {
			t.Fatal(e)
		}
	}
}
