package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDocumentedWireOperations(t *testing.T) {
	cases := []struct {
		op, path, method, body string
		in                     any
	}{
		{"get_build_info", "/api/v1/status/buildinfo", "GET", `{"version":"3.15.1","revision":"abc","buildDate":"20261021"}`, map[string]any{}},
		{"query", "/api/v1/query", "POST", `{"resultType":"vector","result":[{"metric":{"job":"local"},"value":[1700000000.125,"NaN"]}]}`, map[string]any{"query": "up", "options": map[string]any{"limit": 1, "time": "2026-11-04T00:00:00Z"}}},
		{"query_range", "/api/v1/query_range", "POST", `{"resultType":"matrix","result":[{"metric":{},"values":[[1700000000.125,"+Inf"]]}]}`, map[string]any{"query": "up", "start": "2026-11-04T00:00:00Z", "end": "2026-11-04T00:01:00Z", "step_seconds": "15"}},
		{"list_label_names", "/api/v1/labels", "GET", `["__name__","job"]`, map[string]any{}},
		{"list_label_values", "/api/v1/label/job/values", "GET", `["local"]`, map[string]any{"label": "job"}},
		{"find_series", "/api/v1/series", "GET", `[{"__name__":"up","job":"local"}]`, map[string]any{"options": map[string]any{"matches": []string{"up"}}}},
		{"list_alerts", "/api/v1/alerts", "GET", `{"alerts":[{"labels":{"alertname":"Synthetic"},"annotations":{"summary":"test"},"state":"firing","activeAt":"2026-11-04T00:00:00Z","value":"1"}]}`, map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			calls := 0
			s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/prefix"+tc.path || r.Method != tc.method || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if tc.method == "POST" {
					if e := r.ParseForm(); e != nil {
						t.Error(e)
					}
					if r.Form.Get("query") != "up" || r.Form.Get("timeout") != "30s" {
						t.Errorf("query form %v", r.Form)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"status":"success","warnings":["fixture warning"],"infos":["fixture info"],"data":`+tc.body+`}`)
			}))
			defer s.Close()
			t.Setenv("SOKEL_PROMETHEUS_URL", s.URL+"/prefix")
			v, e := dispatch(t, tc.op, tc.in, map[string]string{"bearer_token": "fixture"})
			if e != nil {
				t.Fatal(e)
			}
			if calls != 1 {
				t.Fatalf("calls %d", calls)
			}
			if len(v) == 0 {
				t.Fatal("empty output")
			}
			if tc.op == "query" && !strings.Contains(v["result_json"].(string), "NaN") {
				t.Fatal("sample value lost")
			}
		})
	}
}
