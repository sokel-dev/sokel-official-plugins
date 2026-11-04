package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"testing"
)

func document() *plugin.File { return &plugin.File{ID: "f_doc", Name: "sample.txt"} }
func TestExtractTextProtocol(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/tika/json/text" || r.Method != "PUT" {
			t.Fatalf("request %v", r)
		}
		return response("application/json", `{"Content-Type":"text/plain","tk:content":"hello","author":["A","B"]}`), nil
	})
	out, e := c.extractText(ctxFor("hello"), &ExtractTextIn{File: document()})
	if e != nil || out == nil || out.Text != "hello" {
		t.Fatalf("text: %v %v", out, e)
	}
}
func TestRecursiveProtocol(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return response("application/json", `[{"Content-Type":"text/plain","tk:content":"hello"},{"Content-Type":"text/plain","tk:content":"child"}]`), nil
	})
	out, e := c.extractRecursive(ctxFor("hello"), &ExtractRecursiveIn{File: document()})
	if e != nil || out == nil || len(out.Documents) != 2 {
		t.Fatalf("recursive: %v %v", out, e)
	}
}
func TestDetectProtocol(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/detect" {
			t.Fatal(r.URL)
		}
		return response("text/plain", "text/plain"), nil
	})
	out, e := c.detectType(ctxFor("hello"), &DetectTypeIn{File: document()})
	if e != nil || out == nil || out.MediaType != "text/plain" {
		t.Fatalf("detect: %v %v", out, e)
	}
}
