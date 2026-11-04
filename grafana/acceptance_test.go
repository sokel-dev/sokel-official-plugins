package main

import (
	"github.com/sokel-dev/sokel-official-plugins/grafana/schema"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestLocalServiceAcceptance(t *testing.T) {
	base := os.Getenv("SOKEL_TEST_GRAFANA_URL")
	if base == "" {
		t.Skip("Set the synthetic loopback Grafana acceptance environment to enable.")
	}
	u, e := url.Parse(base)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("Local acceptance requires a loopback fixture.")
	}
	token, uid, source := os.Getenv("SOKEL_TEST_GRAFANA_TOKEN"), os.Getenv("SOKEL_TEST_GRAFANA_DASHBOARD_UID"), os.Getenv("SOKEL_TEST_GRAFANA_DATASOURCE_UID")
	if token == "" || uid == "" || source == "" {
		t.Fatal("Missing synthetic fixture token/dashboard/data source configuration.")
	}
	c := newClient(base, Cred{Token: token})
	ctx := ctxFor("")
	health, e := c.healthCheck(ctx, &HealthCheckIn{})
	if e != nil || !health.OK {
		t.Fatalf("Health: %v %+v", e, health)
	}
	sources, e := c.listDataSources(ctx, &ListDataSourcesIn{})
	if e != nil || len(sources.DataSources) < 1 {
		t.Fatalf("Data source list: %v", e)
	}
	ds, e := c.getDataSource(ctx, &GetDataSourceIn{UID: source})
	if e != nil || ds.DataSource.UID != source {
		t.Fatalf("Data source get: %v", e)
	}
	dashboards, e := c.searchDashboards(ctx, &SearchDashboardsIn{})
	if e != nil || len(dashboards.Dashboards) < 1 {
		t.Fatalf("Dashboard search: %v", e)
	}
	dashboard, e := c.getDashboard(ctx, &GetDashboardIn{UID: uid})
	if e != nil || dashboard.UID != uid || dashboard.ModelJSON == "" {
		t.Fatalf("Dashboard get: %v", e)
	}
	if _, e = c.listAnnotations(ctx, &ListAnnotationsIn{}); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("SOKEL_TEST_GRAFANA_WRITE") != "1" {
		t.Log("Read-only local Grafana acceptance passed.")
		return
	}
	stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	text := "Sokel synthetic acceptance " + stamp
	created, e := c.createAnnotation(ctx, &CreateAnnotationIn{Text: text, Time: stamp, Options: schema.AnnotationOptions{DashboardUID: &uid, Tags: []string{"sokel-synthetic"}}})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := c.listAnnotations(ctx, &ListAnnotationsIn{Filter: schema.AnnotationFilter{DashboardUID: &uid}})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, row := range rows.Annotations {
		if row.ID == created.ID && row.Text == text {
			found = true
		}
	}
	if !found {
		t.Fatal("Created annotation was not returned by the dashboard filter.")
	}
	t.Log("Isolated Grafana acceptance passed; caller owns fixture cleanup.")
}
