package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Published wire shapes and synthetic boundaries, not live account captures.
func TestDocumentedWireRequests(t *testing.T) {
	runCases(t, []wireCase{
		{"run_actor", `{"actor_id":"user~scraper","input":{"startUrls":[{"url":"https://example.com"}]},"options":{"max_items":0,"max_total_charge_usd":1}}`, "POST", "/v2/actors/user~scraper/runs?maxItems=0&maxTotalChargeUsd=1&restartOnError=false&waitForFinish=0", `{"startUrls":[{"url":"https://example.com"}]}`, `{"data":{"id":"run1","actId":"actor1","status":"READY","startedAt":"2026-11-03T00:00:00Z","defaultDatasetId":"data1"}}`, nil},
		{"get_run", `{"run_id":"run1"}`, "GET", "/v2/actor-runs/run1", "", `{"data":{"id":"run1","actId":"actor1","status":"SUCCEEDED","startedAt":"2026-11-03T00:00:00Z","defaultDatasetId":"data1"}}`, func(t *testing.T, v any) {
			if *v.(*GetRunOut).Run.DefaultDatasetID != "data1" {
				t.Fatal(v)
			}
		}},
		{"list_runs", `{"actor_id":"actor1","page":{"offset":0,"limit":2,"desc":false}}`, "GET", "/v2/actors/actor1/runs?desc=0&limit=2&offset=0", "", `{"data":{"total":3,"offset":0,"limit":2,"count":1,"items":[{"id":"run1","actId":"actor1","status":"FAILED","startedAt":"2026-11-03T00:00:00Z"}]}}`, func(t *testing.T, v any) {
			o := v.(*ListRunsOut)
			if !o.HasMore || o.NextOffset != 1 {
				t.Fatal(o)
			}
		}},
		{"get_dataset", `{"dataset_id":"data1"}`, "GET", "/v2/datasets/data1", "", `{"data":{"id":"data1","name":"demo","itemCount":3,"cleanItemCount":2,"createdAt":"2026-11-03T00:00:00Z","modifiedAt":"2026-11-03T00:00:00Z"}}`, nil},
		{"health_check", `{}`, "GET", "/v2/users/me", "", `{"data":{"id":"user1","username":"demo"}}`, func(t *testing.T, v any) {
			if !v.(*HealthCheckOut).OK {
				t.Fatal(v)
			}
		}},
	}, []string{"SOKEL_APIFY_BASE_URL"})
	t.Run("list_dataset_items", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.RequestURI() != "/v2/datasets/data1/items?format=json&limit=2&offset=2" || r.Header.Get("Authorization") != "Bearer test-secret" {
				t.Errorf("unexpected wire %s %s", r.Method, r.URL.RequestURI())
			}
			for k, v := range map[string]string{"Content-Type": "application/json", "X-Apify-Pagination-Offset": "2", "X-Apify-Pagination-Limit": "2", "X-Apify-Pagination-Count": "1", "X-Apify-Pagination-Total": "3"} {
				w.Header().Set(k, v)
			}
			io.WriteString(w, `[{"title":"Example","nested":{"score":1}}]`)
		}))
		defer s.Close()
		t.Setenv("SOKEL_APIFY_BASE_URL", s.URL)
		v, e := invoke(context.Background(), "test-secret", "list_dataset_items", `{"dataset_id":"data1","page":{"offset":2,"limit":2}}`)
		if e != nil {
			t.Fatal(e)
		}
		o := v.(*ListDatasetItemsOut)
		if o.Count != 1 || o.NextOffset != 3 || o.HasMore {
			t.Fatal(o)
		}
	})
}
func TestBoundaries(t *testing.T) {
	runBoundaries(t, "SOKEL_APIFY_BASE_URL", "run_actor", `{"actor_id":"actor1","input":{}}`, "")
}
func TestInvalidAndIncomplete(t *testing.T) {
	for _, x := range []struct{ op, in string }{{"run_actor", `{"actor_id":"https://evil.example/path"}`}, {"get_run", `{"run_id":"../users/me"}`}, {"list_runs", `{"actor_id":"a","page":{"limit":0}}`}, {"run_actor", `{"actor_id":"a","options":{"max_items":-1}}`}, {"list_dataset_items", `{"dataset_id":"a","page":{"offset":-1}}`}} {
		if _, e := invoke(context.Background(), "test-secret", x.op, x.in); e == nil {
			t.Fatal(x)
		}
	}
	inputs := map[string]string{"run_actor": `{"actor_id":"a"}`, "get_run": `{"run_id":"r"}`, "list_runs": `{"actor_id":"a"}`, "get_dataset": `{"dataset_id":"d"}`, "list_dataset_items": `{"dataset_id":"d"}`}
	for op, in := range inputs {
		if _, e := invoke(context.Background(), "", op, in); e == nil {
			t.Fatal("missing token accepted")
		}
	}
	for _, body := range []string{`{}`, `{"data":null}`, `{"error":{"message":"test-secret"}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_APIFY_BASE_URL", s.URL)
		for op, in := range inputs {
			if _, e := invoke(context.Background(), "test-secret", op, in); e == nil || strings.Contains(e.Error(), "test-secret") {
				t.Fatalf("%s %v", op, e)
			}
		}
		v, e := invoke(context.Background(), "test-secret", "health_check", `{}`)
		if e != nil || v.(*HealthCheckOut).OK {
			t.Fatal("false health success")
		}
		s.Close()
	}
}

func TestDatasetPaginationAndInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body, count, total string
		wantError                bool
	}{{"partial", "[{\"value\":1}]", "1", "3", false}, {"final_empty", "[]", "0", "0", false}, {"empty_before_total", "[]", "0", "3", true}, {"count_mismatch", "[]", "1", "3", true}, {"missing_total", "[]", "0", "", true}, {"non_object", "[null]", "1", "1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Apify-Pagination-Offset", "0")
				w.Header().Set("X-Apify-Pagination-Limit", "100")
				w.Header().Set("X-Apify-Pagination-Count", tc.count)
				w.Header().Set("X-Apify-Pagination-Total", tc.total)
				io.WriteString(w, tc.body)
			}))
			defer s.Close()
			t.Setenv("SOKEL_APIFY_BASE_URL", s.URL)
			v, e := invoke(context.Background(), "test-secret", "list_dataset_items", `{"dataset_id":"data1"}`)
			if (e != nil) != tc.wantError {
				t.Fatalf("unexpected error %v", e)
			}
			if e == nil {
				if v.(*ListDatasetItemsOut).HasMore != (tc.name == "partial") {
					t.Fatal(v)
				}
			}
		})
	}
}
func TestEmptyInputAndFailedRun(t *testing.T) {
	runCases(t, []wireCase{{"run_actor", `{"actor_id":"actor1"}`, "POST", "/v2/actors/actor1/runs?restartOnError=false&waitForFinish=0", `{}`, `{"data":{"id":"run1","actId":"actor1","status":"READY","startedAt":"2026-11-03T00:00:00Z"}}`, nil}, {"get_run", `{"run_id":"run1"}`, "GET", "/v2/actor-runs/run1", "", `{"data":{"id":"run1","actId":"actor1","status":"FAILED","startedAt":"2026-11-03T00:00:00Z","statusMessage":"test-secret"}}`, func(t *testing.T, v any) {
		o := v.(*GetRunOut)
		if o.Run.Status != "FAILED" || strings.Contains(*o.Run.StatusMessage, "test-secret") {
			t.Fatal(o)
		}
	}}}, []string{"SOKEL_APIFY_BASE_URL"})
	v, e := invoke(context.Background(), "", "health_check", `{}`)
	if e != nil || v.(*HealthCheckOut).OK {
		t.Fatal("missing credentials health success")
	}
}
