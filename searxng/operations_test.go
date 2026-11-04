package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/searxng/schema"
	"net/http"
	"strings"
	"testing"
)

// Derived from the project's tests/unit/test_webapp.py test_search_json input and serializer.
const sourceExample = `{"query":"test","results":[{"title":"First Test","url":"http://first.test.xyz","content":"first test content","engine":"startpage"},{"title":"Second Test","url":"http://second.test.xyz","content":"second test content","engine":"youtube"}],"suggestions":[],"corrections":[],"unresponsive_engines":[]}`

func TestSearchGeneratedBinding(t *testing.T) {
	c, _ := fake(t, sourceExample, "application/json", 200, func(r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture-user" || p != "fixture-password" {
			t.Fatal("basic auth")
		}
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/prefix/search" || q.Get("q") != "test" || q.Get("format") != "json" || q.Get("safesearch") != "0" || q.Get("pageno") != "2" || q.Get("engines") != "startpage,youtube" || q.Get("time_range") != "day" {
			t.Fatal(r.URL)
		}
	})
	s := invoke(t, c, "search", `{"query":"test","options":{"safe_search":0,"page":2,"limit":1,"engines":["startpage","youtube"],"categories":["general"],"language":"en","time_range":"day"}}`, map[string]string{"username": "fixture-user", "password": "fixture-password"})
	if s.vars["clipped"] != true || s.vars["partial"] != false {
		t.Fatal(s.vars)
	}
	raw, _ := json.Marshal(s.vars)
	if !strings.Contains(string(raw), `"url":"http://first.test.xyz"`) || strings.Contains(string(raw), "second.test.xyz") {
		t.Fatal(string(raw))
	}
	c, _ = fake(t, sourceExample, "application/json", 200, func(r *http.Request) {
		if _, ok := r.URL.Query()["safesearch"]; ok {
			t.Fatal("omitted safe search became zero")
		}
	})
	if _, e := c.search(context.Background(), &SearchIn{Query: "test"}); e != nil {
		t.Fatal(e)
	}
}
func TestPartialAndConfig(t *testing.T) {
	body := strings.Replace(sourceExample, `"unresponsive_engines":[]`, `"unresponsive_engines":[["google","timeout"]]`, 1)
	c, _ := fake(t, body, "application/json", 200, nil)
	out, e := c.search(context.Background(), &SearchIn{Query: "test"})
	if e != nil || !out.Partial || out.EngineErrors[0].Engine != "google" {
		t.Fatal(out, e)
	}
	config := `{"instance_name":"Local search","version":"2026.11.2","categories":["general"],"engines":[{"name":"startpage","shortcut":"sp","categories":["general"],"enabled":true,"paging":true,"safesearch":true,"time_range_support":true}]}`
	c, _ = fake(t, config, "application/json", 200, func(r *http.Request) {
		if r.URL.Path != "/prefix/config" {
			t.Fatal(r.URL)
		}
	})
	s := invoke(t, c, "get_config", `{}`, nil)
	raw, _ := json.Marshal(s.vars)
	if !strings.Contains(string(raw), `"time_range_support":true`) {
		t.Fatal(string(raw))
	}
	c, _ = fake(t, "OK", "text/plain", 200, func(r *http.Request) {
		if r.URL.Path != "/prefix/healthz" {
			t.Fatal(r.URL)
		}
	})
	s = invoke(t, c, "health", `{}`, nil)
	if s.vars["healthy"] != true {
		t.Fatal(s.vars)
	}
	c, _ = fake(t, "login page", "text/html", 200, nil)
	if _, e := c.health(context.Background(), &HealthIn{}); e == nil {
		t.Fatal("invalid health")
	}
	c, _ = fake(t, `{}`, "application/json", 200, nil)
	if _, e := c.getConfig(context.Background(), &GetConfigIn{}); e == nil {
		t.Fatal("invalid config")
	}
}
func TestSearchInputAndResponseValidation(t *testing.T) {
	neg, large, zero, badSafe := -1, 1001, 0, 3
	empty, badTime := "", "week"
	for _, in := range []SearchIn{{Query: ""}, {Query: strings.Repeat("q", 4097)}, {Query: "q", Options: schema.SearchOptions{Page: &neg}}, {Query: "q", Options: schema.SearchOptions{Page: &large}}, {Query: "q", Options: schema.SearchOptions{Limit: &zero}}, {Query: "q", Options: schema.SearchOptions{SafeSearch: &badSafe}}, {Query: "q", Options: schema.SearchOptions{TimeRange: &badTime}}, {Query: "q", Options: schema.SearchOptions{Language: &empty}}, {Query: "q", Options: schema.SearchOptions{Engines: []string{"bad,name"}}}} {
		c, n := fake(t, sourceExample, "application/json", 200, nil)
		if _, e := c.search(context.Background(), &in); e == nil || *n != 0 {
			t.Fatal("bad input accepted", in)
		}
	}
	for _, body := range []string{`{}`, `{"query":"test","results":null}`, strings.Replace(sourceExample, `"unresponsive_engines":[]`, `"unresponsive_engines":["wrong"]`, 1)} {
		c, _ := fake(t, body, "application/json", 200, nil)
		if _, e := c.search(context.Background(), &SearchIn{Query: "test"}); e == nil {
			t.Fatal("invalid response", body)
		}
	}
	for _, cred := range []Cred{{Username: "only"}, {Password: "only"}, {Username: "bad:user", Password: "p"}, {Username: "u", Password: "bad\n"}} {
		c, n := fake(t, sourceExample, "application/json", 200, nil)
		c.cred = cred
		if _, e := c.search(context.Background(), &SearchIn{Query: "test"}); e == nil || *n != 0 {
			t.Fatal("invalid proxy credentials")
		}
	}
}
