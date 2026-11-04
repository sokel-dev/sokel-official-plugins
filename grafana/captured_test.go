package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestCapturedGrafanaResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-grafana-13.3.0.json")
	if e != nil {
		t.Fatal(e)
	}
	var rows []struct {
		Path   string          `json:"path"`
		Status int             `json:"status"`
		Body   json.RawMessage `json:"response"`
	}
	if e = json.Unmarshal(b, &rows); e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if row.Path == "/api/health" {
			continue
		}
		t.Run(row.Path, func(t *testing.T) {
			c := testClient(func(*http.Request) (*http.Response, error) {
				s := response("application/json", string(row.Body))
				s.StatusCode = row.Status
				return s, nil
			})
			ctx := ctxFor("")
			switch {
			case strings.HasPrefix(row.Path, "/api/search"):
				if _, e := c.searchDashboards(ctx, &SearchDashboardsIn{}); e != nil {
					t.Fatal(e)
				}
			case row.Path == "/api/datasources":
				if _, e := c.listDataSources(ctx, &ListDataSourcesIn{}); e != nil {
					t.Fatal(e)
				}
			case strings.HasPrefix(row.Path, "/api/datasources/uid/"):
				if _, e := c.getDataSource(ctx, &GetDataSourceIn{UID: "wave8-prometheus"}); e != nil {
					t.Fatal(e)
				}
			case strings.HasPrefix(row.Path, "/api/dashboards/uid/"):
				if _, e := c.getDashboard(ctx, &GetDashboardIn{UID: "wave8-synthetic"}); e != nil {
					t.Fatal(e)
				}
			case strings.HasPrefix(row.Path, "/api/annotations"):
				if _, e := c.listAnnotations(ctx, &ListAnnotationsIn{}); e != nil {
					t.Fatal(e)
				}
			default:
				t.Fatal("Unexpected capture path")
			}
		})
	}
}
