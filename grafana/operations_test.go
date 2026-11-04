package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/grafana/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGeneratedSearchFalseAndModelPrecision(t *testing.T) {
	t.Setenv("SOKEL_GRAFANA_URL", "http://service.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("auth")
		}
		if strings.HasSuffix(r.URL.Path, "/api/search") {
			q := r.URL.Query()
			if q.Get("starred") != "false" || q.Get("type") != "dash-db" || len(q["tag"]) != 2 || q.Get("folderUIDs") != "folder" {
				t.Fatal("query fields")
			}
			return response("application/json", `[{"uid":"dash","title":"Dashboard","url":"/d/dash","type":"dash-db","isStarred":false,"tags":[]}]`), nil
		}
		return response("application/json", `{"dashboard":{"id":9007199254740993,"uid":"dash","title":"Dashboard","version":2,"panels":[{"pluginNumber":9007199254740993}]},"meta":{"private":"ignored"}}`), nil
	})
	v, e := dispatch(t, "search_dashboards", `{"query":"load","options":{"starred":false,"tags":["a","b"],"folder_uids":["folder"],"limit":1}}`, ctxFor(""))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"next_page":2`) {
		t.Fatal(string(b))
	}
	v, e = dispatch(t, "get_dashboard", `{"uid":"dash"}`, ctxFor(""))
	if e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(v)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "private") || !strings.Contains(string(b), "9007199254740993") {
		t.Fatal(string(b))
	}
}
func TestGeneratedAnnotationCamelCaseAndZero(t *testing.T) {
	t.Setenv("SOKEL_GRAFANA_URL", "http://service.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			for _, part := range []string{`"dashboardUID":"dash"`, `"panelId":0`, `"timeEnd":1507037197340`, `"tags":["deploy"]`} {
				if !strings.Contains(string(b), part) {
					t.Fatalf("missing %s: %s", part, b)
				}
			}
			if r.GetBody != nil {
				t.Fatal("replay")
			}
			return response("application/json", `{"id":9007199254740993,"message":"Annotation added"}`), nil
		}
		q := r.URL.Query()
		if q.Get("from") != "0" || q.Get("panelId") != "0" || q.Get("dashboardUID") != "dash" || q.Get("type") != "annotation" || len(q["tags"]) != 2 {
			t.Fatal("annotation filter")
		}
		return response("application/json", `[{"id":9007199254740993,"dashboardUID":"dash","panelId":0,"time":1507037197339,"timeEnd":1507037197340,"text":"Deploy","tags":["deploy"]}]`), nil
	})
	for _, tc := range []struct{ op, in string }{{"create_annotation", `{"text":"Deploy","time":"1507037197339","options":{"dashboard_uid":"dash","panel_id":"0","time_end":"1507037197340","tags":["deploy"]}}`}, {"list_annotations", `{"filter":{"from":"0","to":"1507037197340","dashboard_uid":"dash","panel_id":"0","tags":["a","b"],"limit":1}}`}} {
		v, e := dispatch(t, tc.op, tc.in, ctxFor(""))
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "9007199254740993") {
			t.Fatal(string(b))
		}
		if tc.op == "list_annotations" && !strings.Contains(string(b), `"limit_reached":true`) {
			t.Fatal(string(b))
		}
	}
}
func TestHealthSourcesAndShapeFailures(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/api/search") {
			return response("application/json", `[]`), nil
		}
		return response("application/json", `{"uid":"source","name":"Metrics","type":"prometheus","password":"secret","secureJsonFields":{"token":true}}`), nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || !o.OK {
		t.Fatal(o, e)
	}
	if o, e := c.getDataSource(ctxFor(""), &GetDataSourceIn{UID: "source"}); e != nil || o.DataSource.UID != "source" {
		t.Fatal(o, e)
	}
	for _, raw := range []string{`null`, `{}`, `[{"uid":"x"}]`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", raw), nil })
		if _, e := c.listDataSources(ctxFor(""), &ListDataSourcesIn{}); e == nil {
			t.Fatal("invalid source list")
		}
	}
	for _, raw := range []string{`{}`, `{"dashboard":null}`, `{"dashboard":{"uid":"other","title":"D","version":1}}`, `{"dashboard":{"uid":"dash","title":"D"}}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", raw), nil })
		if _, e := c.getDashboard(ctxFor(""), &GetDashboardIn{UID: "dash"}); e == nil {
			t.Fatal("invalid dashboard")
		}
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		s := response("application/json", `{"secret":"token"}`)
		s.StatusCode = 401
		return s, nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || o.OK || strings.Contains(o.Message, "secret") {
		t.Fatal(o, e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"id":1,"message":"Failed"}`), nil
	})
	if _, e := c.createAnnotation(ctxFor(""), &CreateAnnotationIn{Text: "text", Time: "1"}); e == nil {
		t.Fatal("false receipt")
	}
}
func TestInputValidation(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input made request"); return nil, nil })
	zero := 0
	id := "0"
	start, end := "100", "99"
	uid := "dash"
	if _, e := c.searchDashboards(ctxFor(""), &SearchDashboardsIn{Options: schema.SearchOptions{Limit: &zero}}); e == nil {
		t.Fatal("zero limit")
	}
	if _, e := c.getDataSource(ctxFor(""), &GetDataSourceIn{UID: "../bad"}); e == nil {
		t.Fatal("path")
	}
	if _, e := c.listAnnotations(ctxFor(""), &ListAnnotationsIn{Filter: schema.AnnotationFilter{From: &start, To: &end}}); e == nil {
		t.Fatal("range")
	}
	if _, e := c.listAnnotations(ctxFor(""), &ListAnnotationsIn{Filter: schema.AnnotationFilter{PanelID: &id}}); e == nil {
		t.Fatal("unscoped panel")
	}
	for _, in := range []*CreateAnnotationIn{{Text: "", Time: "1"}, {Text: "t", Time: "0"}, {Text: "t", Time: "-1"}, {Text: "t", Time: "100", Options: schema.AnnotationOptions{TimeEnd: &end}}, {Text: "t", Time: "100", Options: schema.AnnotationOptions{PanelID: &id}}, {Text: "t", Time: "100", Options: schema.AnnotationOptions{DashboardUID: &uid, Tags: []string{""}}}} {
		if _, e := c.createAnnotation(ctxFor(""), in); e == nil {
			t.Fatal("create annotation validation")
		}
	}
}
