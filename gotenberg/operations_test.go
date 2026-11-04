package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"testing"
)

const testPDF = "%PDF-1.4\nsynthetic fixture\n%%EOF\n"

func TestHealthProtocol(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/health" || r.Method != "GET" {
			t.Fatalf("request %v", r)
		}
		return response("application/json", `{"status":"up","details":{"chromium":{"status":"up"}}}`), nil
	})
	out, e := c.healthCheck(ctxFor(""), &HealthCheckIn{})
	if e != nil || out == nil || !out.OK {
		t.Fatalf("health: %v %v", out, e)
	}
}
func TestHTMLCreatesPlatformPDF(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/forms/chromium/convert/html" {
			t.Fatal(r.URL)
		}
		return response("application/pdf", testPDF), nil
	})
	ctx := ctxFor("")
	out, e := c.htmlToPDF(ctx, &HTMLToPdfIn{HTML: "<h1>Hello</h1>"})
	if e != nil || out == nil || out.File.Mime != "application/pdf" || string(ctx.stored) != testPDF {
		t.Fatalf("convert: %v %v", out, e)
	}
}
func TestMergeCreatesPlatformPDF(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) { return response("application/pdf", testPDF), nil })
	ctx := ctxFor(testPDF)
	out, e := c.mergePDFs(ctx, &MergePdfsIn{Files: []*plugin.File{{ID: "a", Name: "first.pdf"}, {ID: "b", Name: "second.pdf"}}})
	if e != nil || out == nil || out.File.Size != int64(len(testPDF)) {
		t.Fatalf("merge: %v %v", out, e)
	}
}
