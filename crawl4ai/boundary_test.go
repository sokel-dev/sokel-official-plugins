package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/crawl4ai/schema"
	"net/http"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func TestRestrictedCrawlOptions(t *testing.T) {
	for _, u := range []string{"", "file:///etc/passwd", "raw:<h1>x</h1>", "javascript:alert(1)", "https://user:pass@example.com/", "https://example.com/x y", "https://example.com/\r", strings.Repeat("x", 8193)} {
		if pageURL(u) {
			t.Fatal(u)
		}
	}
	for _, urls := range [][]string{nil, {}, {"https://example.com/", "https://example.com/"}, {"https://example.com/", "https://b.example/", "https://c.example/", "https://d.example/", "https://e.example/", "https://f.example/"}} {
		if _, e := crawlBody(urls, schema.CrawlOptions{}); e == nil {
			t.Fatal(urls)
		}
	}
	for _, o := range []schema.CrawlOptions{{PageTimeoutMS: ptr(0)}, {PageTimeoutMS: ptr(60001)}, {WordCountThreshold: ptr(-1)}, {WordCountThreshold: ptr(10001)}, {CSSSelector: ptr("x\x00")}, {CSSSelector: ptr(strings.Repeat("x", 2049))}} {
		if _, e := crawlBody([]string{"https://example.com/"}, o); e == nil {
			t.Fatal(o)
		}
	}
	b, e := crawlBody([]string{"https://example.com/"}, schema.CrawlOptions{})
	if e != nil || b.CrawlerConfig.PageTimeout != 30000 || b.CrawlerConfig.Stream || b.CrawlerConfig.OnlyText != nil {
		t.Fatal(b, e)
	}
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("bad input sent"); return nil, nil })
	if _, e = c.crawl(context.Background(), &CrawlIn{}); e == nil {
		t.Fatal("empty URLs")
	}
	if _, e = c.submit_crawl(context.Background(), &SubmitCrawlIn{}); e == nil {
		t.Fatal("empty URLs")
	}
	if _, e = c.markdown(context.Background(), &MarkdownIn{URL: "file:///etc/passwd"}); e == nil {
		t.Fatal("file URL")
	}
	for _, id := range []string{"", "../secret", "crawl_a1b2c3d4?token=x", "llm_1234", "https://example.com/", "crawl_a1b2c3d"} {
		if _, e = c.get_task(context.Background(), &GetTaskIn{TaskID: id}); e == nil {
			t.Fatal(id)
		}
	}
}
func TestTaskLifecycleAndPartialFailure(t *testing.T) {
	c := fresh()
	id := "crawl_a1b2c3d4"
	for _, status := range []string{"processing", "failed"} {
		withReply(c, 200, `{"task_id":"`+id+`","status":"`+status+`","error":"synthetic-test-token internal_trace"}`)
		o, e := c.get_task(context.Background(), &GetTaskIn{TaskID: id})
		if e != nil || o.Completed || o.AllSucceeded || len(o.Results) != 0 || strings.Contains(o.Message, "synthetic-test-token") {
			t.Fatal(o, e)
		}
	}
	for _, raw := range []string{`{"task_id":"crawl_a1b2c3d5","status":"processing"}`, `{"task_id":"crawl_a1b2c3d4","status":"unknown"}`, `{"task_id":"crawl_a1b2c3d4","status":"completed"}`, `{"task_id":123,"status":"completed"}`, `not-json`} {
		withReply(c, 200, raw)
		if _, e := c.get_task(context.Background(), &GetTaskIn{TaskID: id}); e == nil {
			t.Fatal(raw)
		}
	}
	raw := `{"success":true,"results":[{"url":"https://example.com/","success":false,"status_code":403,"error_message":"synthetic-test-token stack trace","metadata":{"title":"hidden error"},"markdown":null}]}`
	withReply(c, 200, raw)
	o, e := c.crawl(context.Background(), &CrawlIn{Urls: []string{"https://example.com/"}})
	if e != nil || o.AllSucceeded || o.Results[0].Success || o.Results[0].Markdown != nil || o.Results[0].Title != nil {
		t.Fatal(o, e)
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "synthetic-test-token") {
		t.Fatal("trace leaked")
	}
	withReply(c, 200, `{"task_id":"`+id+`","status":"completed","result":`+raw+`}`)
	job, e := c.get_task(context.Background(), &GetTaskIn{TaskID: id})
	if e != nil || !job.Completed || job.AllSucceeded {
		t.Fatal(job, e)
	}
	r, all, e := parseResults([]byte(strings.Replace(raw, `"success":false`, `"success":true`, 1)), 1)
	if e != nil || all || r[0].Success {
		t.Fatal("HTTP error marked success", r, e)
	}
	r, _, e = parseResults([]byte(strings.Replace(raw, `https://example.com/`, `internal_trace synthetic-test-token`, 1)), 1)
	if e != nil || r[0].URL != "" {
		t.Fatal("failed URL leaked", r, e)
	}
}
func TestMalformedResultsAndReceipts(t *testing.T) {
	raw := fixtures(t)[1].Response
	for _, b := range []string{`null`, `{}`, `{"success":false,"results":[]}`, `{"success":true,"results":[]}`, `{"success":true,"results":[{}]}`, strings.Replace(raw, `"success":true`, `"success":false`, 1), strings.Replace(raw, `"status_code":200`, `"status_code":600`, 1), strings.Replace(raw, `https://example.com/`, `not-a-url`, 1), strings.Replace(raw, `"raw_markdown":`, `"unknown":`, 1), strings.Replace(raw, `"success":true`, `"success":"yes"`, 1)} {
		if _, _, e := parseResults([]byte(b), 1); e == nil {
			t.Fatal("invalid result", b)
		}
	}
	if _, _, e := parseResults([]byte(raw), 2); e == nil {
		t.Fatal("missing page")
	}
	c := fresh()
	for _, raw := range []string{`{}`, `{"task_id":"../bad"}`, `{"task_id":123}`, `bad`} {
		withReply(c, 202, raw)
		if _, e := c.submit_crawl(context.Background(), &SubmitCrawlIn{Urls: []string{"https://example.com/"}}); e == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"success":false,"url":"https://example.com/","filter":"raw","markdown":"x"}`, `{"success":true,"url":"https://other.example/","filter":"raw","markdown":"x"}`, `{"success":true,"url":"https://example.com/","filter":"llm","markdown":"x"}`, `{"success":true,"url":"https://example.com/","filter":"raw","markdown":42}`, `bad`} {
		withReply(c, 200, raw)
		if _, e := c.markdown(context.Background(), &MarkdownIn{URL: "https://example.com/"}); e == nil {
			t.Fatal(raw)
		}
	}
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		var b map[string]string
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil || b["c"] != "1" || b["f"] != "raw" {
			t.Fatal(b, e)
		}
		return httpResponse(200, `{"success":true,"url":"https://example.com/","filter":"raw","markdown":""}`), nil
	})
	if _, e := c.markdown(context.Background(), &MarkdownIn{URL: "https://example.com/", UseCache: true}); e != nil {
		t.Fatal(e)
	}
}
