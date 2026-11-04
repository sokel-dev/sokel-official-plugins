package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCapturedServiceResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-service-captures.json")
	if e != nil {
		t.Fatal(e)
	}
	var rows []capturedExchange
	if e = json.Unmarshal(b, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) < 9 {
		t.Fatal("missing captures")
	}
	for _, r := range rows {
		t.Run(r.Path, func(t *testing.T) {
			u, _ := url.Parse(r.Path)
			q := u.Query()
			if r.Method == "POST" {
				q, _ = url.ParseQuery(r.Request)
			}
			op := ""
			in := map[string]any{}
			options := map[string]any{}
			if v := q.Get("limit"); v != "" {
				n, _ := strconv.Atoi(v)
				options["limit"] = n
			}
			if v := q["match[]"]; len(v) > 0 {
				options["matches"] = v
			}
			for _, k := range []string{"start", "end", "time"} {
				if v := q.Get(k); v != "" {
					options[k] = v
				}
			}
			switch u.Path {
			case "/api/v1/status/buildinfo":
				op = "get_build_info"
			case "/api/v1/query":
				op = "query"
				in["query"] = q.Get("query")
				in["options"] = options
			case "/api/v1/query_range":
				op = "query_range"
				in = map[string]any{"query": q.Get("query"), "start": q.Get("start"), "end": q.Get("end"), "step_seconds": q.Get("step"), "options": map[string]any{"limit": options["limit"]}}
			case "/api/v1/labels":
				op = "list_label_names"
				in["options"] = options
			case "/api/v1/series":
				op = "find_series"
				in["options"] = options
			case "/api/v1/alerts":
				op = "list_alerts"
			default:
				if strings.HasPrefix(u.Path, "/api/v1/label/") {
					op = "list_label_values"
					in["label"] = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/api/v1/label/"), "/values")
					in["options"] = options
				} else {
					t.Fatalf("unexpected capture %s", u.Path)
				}
			}
			s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != r.Method || req.URL.Path != u.Path {
					t.Error("capture route mismatch")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(r.Status)
				io.WriteString(w, string(r.Response))
			}))
			defer s.Close()
			t.Setenv("SOKEL_PROMETHEUS_URL", s.URL)
			if _, e := dispatch(t, op, in, nil); e != nil {
				t.Fatal(e)
			}
		})
	}
}
