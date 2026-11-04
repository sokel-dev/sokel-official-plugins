package main

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalServiceAcceptance(t *testing.T) {
	base := os.Getenv("SOKEL_TEST_PROMETHEUS_URL")
	if base == "" {
		t.Skip("set SOKEL_TEST_PROMETHEUS_URL for isolated local acceptance")
	}
	u, e := url.Parse(base)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("local acceptance requires a loopback service")
	}
	t.Setenv("SOKEL_PROMETHEUS_URL", base)
	captureLocalHTTP(t, "prometheus")
	call := func(op string, in any) map[string]any {
		v, e := dispatch(t, op, in, nil)
		if e != nil {
			t.Fatalf("%s: %v", op, e)
		}
		return v
	}
	v := call("get_build_info", map[string]any{})
	if v["version"] == "" {
		t.Fatal("missing version")
	}
	query := os.Getenv("SOKEL_TEST_PROMETHEUS_QUERY")
	if query == "" {
		query = "sokel_wave8_gauge"
	}
	v = call("query", map[string]any{"query": query})
	if !strings.Contains(v["result_json"].(string), `"7"`) {
		t.Fatal("synthetic gauge missing")
	}
	now := time.Now().UTC()
	call("query_range", map[string]any{"query": query, "start": now.Add(-30 * time.Second).Format(time.RFC3339), "end": now.Format(time.RFC3339), "step_seconds": "5"})
	call("list_label_names", map[string]any{"options": map[string]any{"matches": []string{query}}})
	call("list_label_values", map[string]any{"label": "fixture", "options": map[string]any{"matches": []string{query}}})
	call("find_series", map[string]any{"options": map[string]any{"matches": []string{query}}})
	call("list_alerts", map[string]any{})
	v = call("query", map[string]any{"query": "0/0"})
	if v["result_type"] != "scalar" || !strings.Contains(v["result_json"].(string), "NaN") {
		t.Fatal("NaN scalar lost")
	}
	v = call("query", map[string]any{"query": `"synthetic"`})
	if v["result_type"] != "string" {
		t.Fatal("string query lost")
	}
	t.Log("all seven generated operations plus scalar NaN and string results passed")
}
