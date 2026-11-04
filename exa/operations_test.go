package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Documentation-derived examples and synthetic boundaries; no live account captures.
func TestDocumentedWireRequests(t *testing.T) {
	runCases(t, []wireCase{
		{"search", `{"query":"AI safety","options":{"num_results":2},"contents":{"text":false,"highlights":true,"max_age_hours":0}}`, "POST", "/search", `{"query":"AI safety","numResults":2,"contents":{"text":false,"highlights":true,"maxAgeHours":0}}`, `{"requestId":"req-1","results":[{"id":"https://example.com","url":"https://example.com","highlights":["excerpt"]}],"costDollars":{"total":0.005}}`, func(t *testing.T, v any) {
			if v.(*SearchOut).Cost.Total == nil {
				t.Fatal(v)
			}
		}},
		{"get_contents", `{"urls":["https://example.com"]}`, "POST", "/contents", `{"urls":["https://example.com"],"highlights":true}`, `{"requestId":"req-2","results":[{"id":"https://example.com","url":"https://example.com","text":"page"}],"statuses":[{"id":"https://example.com","status":"success","source":"cached"}]}`, nil},
		{"answer", `{"query":"What is Exa?","model":"exa","options":{"text":false}}`, "POST", "/answer", `{"query":"What is Exa?","model":"exa","text":false}`, `{"requestId":"req-3","answer":"A search API.","citations":[]}`, func(t *testing.T, v any) {
			if v.(*AnswerOut).Answer != "A search API." {
				t.Fatal(v)
			}
		}},
		{"agent_start", `{"query":"Research AI companies","effort":"auto","budget":{"max_cost_dollars":1}}`, "POST", "/agent/runs", `{"query":"Research AI companies","effort":"auto","budget":{"maxCostDollars":1}}`, `{"id":"agent_run_123","object":"agent_run","status":"queued","createdAt":"2026-11-03T00:00:00Z"}`, nil},
		{"agent_status", `{"run_id":"agent_run_123"}`, "GET", "/agent/runs/agent_run_123", "", `{"id":"agent_run_123","object":"agent_run","status":"completed","createdAt":"2026-11-03T00:00:00Z","output":{"text":"Research","structured":{"name":"Example"}},"costDollars":{"total":1}}`, func(t *testing.T, v any) {
			if v.(*AgentStatusOut).Structured["name"] != "Example" {
				t.Fatal(v)
			}
		}},
		{"health_check", `{}`, "GET", "/agent/runs?limit=1", "", `{"object":"list","data":[],"hasMore":false,"nextCursor":null}`, func(t *testing.T, v any) {
			if !v.(*HealthCheckOut).OK {
				t.Fatal(v)
			}
		}},
	}, []string{"SOKEL_EXA_BASE_URL"})
}
func TestBoundaries(t *testing.T) {
	runBoundaries(t, "SOKEL_EXA_BASE_URL", "search", `{"query":"test"}`, "")
}
func TestInvalidInputs(t *testing.T) {
	for _, x := range []struct{ op, in string }{{"search", `{"query":" "}`}, {"search", `{"query":"x","options":{"num_results":0}}`}, {"search", `{"query":"x","search_type":"lite","contents":{"max_age_hours":0}}`}, {"get_contents", `{"ids":["a"],"urls":["https://example.com"]}`}, {"get_contents", `{"urls":["file:///etc/passwd"]}`}, {"answer", `{"query":"x","model":"invented"}`}, {"answer", `{"query":"x","output_schema":{"type":"array"}}`}, {"agent_start", `{"query":"x","effort":"low","budget":{"max_cost_dollars":1}}`}, {"agent_start", `{"query":"x","budget":{"max_cost_dollars":0}}`}, {"agent_status", `{"run_id":"../account"}`}} {
		if _, e := invoke(context.Background(), "test-secret", x.op, x.in); e == nil {
			t.Fatalf("accepted %s %s", x.op, x.in)
		}
	}
}
func TestIncompleteResponsesAndHealth(t *testing.T) {
	inputs := map[string]string{"search": `{"query":"x"}`, "get_contents": `{"ids":["doc-1"]}`, "answer": `{"query":"x"}`, "agent_start": `{"query":"x"}`, "agent_status": `{"run_id":"run-1"}`}
	for op, in := range inputs {
		if _, e := invoke(context.Background(), "", op, in); e == nil {
			t.Fatal("missing key accepted")
		}
	}
	for _, body := range []string{`{}`, `{"error":"test-secret"}`, `null`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_EXA_BASE_URL", s.URL)
		for op, in := range inputs {
			if _, e := invoke(context.Background(), "test-secret", op, in); e == nil || strings.Contains(e.Error(), "test-secret") {
				t.Fatalf("%s error %v", op, e)
			}
		}
		v, e := invoke(context.Background(), "test-secret", "health_check", `{}`)
		if e != nil || v.(*HealthCheckOut).OK {
			t.Fatal("false health success")
		}
		s.Close()
	}
}

func TestStructuredAnswersPartialFetchesAndFailedAgents(t *testing.T) {
	runCases(t, []wireCase{
		{"answer", `{"query":"Company name","output_schema":{"type":"object","properties":{"name":{"type":"string"}}}}`, "POST", "/answer", `{"query":"Company name","outputSchema":{"type":"object","properties":{"name":{"type":"string"}}}}`, `{"requestId":"r","answer":{"name":"Example"},"citations":[]}`, func(t *testing.T, v any) {
			if v.(*AnswerOut).AnswerData["name"] != "Example" || v.(*AnswerOut).Answer != "" {
				t.Fatal(v)
			}
		}},
		{"get_contents", `{"ids":["doc-1"],"options":{"text":true,"highlights":false,"max_age_hours":-1}}`, "POST", "/contents", `{"ids":["doc-1"],"text":true,"highlights":false,"maxAgeHours":-1}`, `{"requestId":"r","results":[],"statuses":[{"id":"doc-1","status":"error","error":{"tag":"test-secret","httpStatusCode":404}}]}`, func(t *testing.T, v any) {
			o := v.(*GetContentsOut)
			if len(o.Results) != 0 || o.Statuses[0].Status != "error" || strings.Contains(*o.Statuses[0].Error.Tag, "test-secret") {
				t.Fatal(o)
			}
		}},
		{"agent_status", `{"run_id":"agent_run_failed"}`, "GET", "/agent/runs/agent_run_failed", "", `{"id":"agent_run_failed","object":"agent_run","status":"failed","createdAt":"2026-11-03T00:00:00Z","error":{"code":"test-secret","message":"test-secret"}}`, func(t *testing.T, v any) {
			o := v.(*AgentStatusOut)
			if o.Run.Status != "failed" || strings.Contains(o.ErrorCode, "test-secret") {
				t.Fatal(o)
			}
		}},
		{"search", `{"query":"Example"}`, "POST", "/search", `{"query":"Example","contents":{"highlights":true}}`, `{"requestId":"r","results":[]}`, nil},
	}, []string{"SOKEL_EXA_BASE_URL"})
}
func TestAdditionalValidation(t *testing.T) {
	for _, x := range []struct{ op, in string }{{"search", `{"query":"x","start_published_date":"yesterday"}`}, {"search", `{"query":"x","end_published_date":"tomorrow"}`}, {"search", `{"query":"x","start_published_date":"2026-11-03T00:00:00Z","end_published_date":"2026-11-01T00:00:00Z"}`}, {"search", `{"query":"x","include_domains":[""]}`}, {"get_contents", `{"ids":[""]}`}, {"agent_status", `{"run_id":".."}`}, {"agent_start", `{"query":"x","effort":"auto","budget":{"max_duration_seconds":300}}`}, {"agent_start", `{"query":"x","effort":"ultra","budget":{"max_duration_seconds":299}}`}} {
		if _, e := invoke(context.Background(), "test-secret", x.op, x.in); e == nil {
			t.Fatal(x)
		}
	}
	v, e := invoke(context.Background(), "", "health_check", `{}`)
	if e != nil || v.(*HealthCheckOut).OK {
		t.Fatal("missing credentials accepted")
	}
}
