package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRawUploadWire(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		b, e := io.ReadAll(r.Body)
		if e != nil || string(b) != "hello\x00world" || r.Method != "PUT" || r.GetBody != nil || r.URL.Path != "/prefix/meta" {
			t.Fatalf("request %v bytes=%q", r, b)
		}
		if r.Header.Get("Content-Type") != "application/pdf" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Disposition") != `attachment; filename="文档\".pdf"` {
			t.Fatal(r.Header)
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != "secret" {
			t.Fatal("auth")
		}
		return response("application/json", `{"Content-Type":"application/pdf","dc:creator":["A","B"],"page-count":0,"flag":false,"number":9007199254740993,"structure":{"count":9007199254740993}}`), nil
	})
	out, e := c.extractMetadata(ctxFor("hello\x00world"), &ExtractMetadataIn{File: &plugin.File{ID: "doc", Name: "文档\".pdf", Mime: "application/pdf"}})
	if e != nil {
		t.Fatal(e)
	}
	got := map[string][]string{}
	for _, f := range out.Metadata {
		got[f.Name] = f.Values
	}
	if got["page-count"][0] != "0" || got["flag"][0] != "false" || got["number"][0] != "9007199254740993" || got["structure"][0] != `{"count":9007199254740993}` || len(got["dc:creator"]) != 2 {
		t.Fatal(got)
	}
}
func TestPartialResultsFail(t *testing.T) {
	for _, key := range []string{"tk:exception:container-exception", "tk:exception:embedded-exception", "tk:exception:write-limit-reached", "tk:exception:task-deadline-reached", "tk:warn:truncated-metadata"} {
		t.Run(key, func(t *testing.T) {
			b, _ := json.Marshal(map[string]any{"Content-Type": "text/plain", "tk:content": "partial", key: "private failure detail"})
			c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", string(b)), nil })
			out, e := c.extractText(ctxFor("x"), &ExtractTextIn{File: document()})
			if e == nil || out != nil || strings.Contains(e.Error(), "private") {
				t.Fatalf("%v %v", out, e)
			}
		})
	}
}
func TestMalformedAndLegacyMetadata(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{}`, `{"error":"private"}`, `{"Content-Type":"text/plain","X-TIKA:content":"x"}`, `{"Content-Type":"text/plain","tk:content":["x"]}`, `{"Content-Type":"text/plain"} {}`, `{"Content-Type":"bad"}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", body), nil })
		if _, e := c.extractText(ctxFor("x"), &ExtractTextIn{File: document()}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestEmptyExtractedTextIsValid(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"Content-Type":"image/png","tk:exception:write-limit-reached":"false"}`), nil
	})
	out, e := c.extractText(ctxFor("png"), &ExtractTextIn{File: document()})
	if e != nil || out.Text != "" {
		t.Fatalf("%v %v", out, e)
	}
}
func TestGeneratedRecursiveDispatch(t *testing.T) {
	t.Setenv("SOKEL_TIKA_URL", "http://convert.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/rmeta/text" {
			t.Fatal(r.URL)
		}
		return response("application/json", `[{"Content-Type":"text/plain","tk:content":"hello","id":"9007199254740993"}]`), nil
	})
	vars, e := dispatch(t, "extract_recursive", `{"file":{"id":"doc","name":"a.txt","size":1}}`, ctxFor("x"))
	if e != nil || vars["documents"] == nil {
		t.Fatalf("%v %v", vars, e)
	}
	b, _ := json.Marshal(vars)
	if !strings.Contains(string(b), "9007199254740993") {
		t.Fatal(string(b))
	}
}
func TestRecursiveRejectsChildFailure(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `{}`, `[{"Content-Type":"text/plain"},{"Content-Type":"text/plain","tk:exception:embedded-exception":"private"}]`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", body), nil })
		if _, e := c.extractRecursive(ctxFor("x"), &ExtractRecursiveIn{File: document()}); e == nil {
			t.Fatal("partial or missing result")
		}
	}
}
func TestDetectRejectsMalformed(t *testing.T) {
	for _, body := range []string{"", "not a mime", "text/plain\r\nAuthorization: secret"} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("text/plain", body), nil })
		if _, e := c.detectType(ctxFor("x"), &DetectTypeIn{File: document()}); e == nil {
			t.Fatal("bad media type")
		}
	}
}
