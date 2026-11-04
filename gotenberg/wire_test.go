package main

import (
	"bytes"
	"github.com/sokel-dev/sokel-official-plugins/gotenberg/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func multipartParts(t *testing.T, r *http.Request) ([]string, map[string]string, []byte) {
	t.Helper()
	if r.Method != "POST" || r.GetBody != nil {
		t.Fatal("write replay or method")
	}
	raw, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	ct, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || ct != "multipart/form-data" {
		t.Fatal("multipart type")
	}
	mr := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	names := []string{}
	fields := map[string]string{}
	for {
		part, e := mr.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(part)
		if e != nil {
			t.Fatal(e)
		}
		if part.FileName() != "" {
			if part.FormName() != "files" {
				t.Fatal("wrong form name")
			}
			names = append(names, part.FileName())
			fields[part.FileName()] = string(data)
		} else {
			fields[part.FormName()] = string(data)
		}
	}
	return names, fields, raw
}
func TestHTMLWireAndFalse(t *testing.T) {
	no := false
	c := testClient(func(r *http.Request) (*http.Response, error) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user" || pass != "secret" || r.URL.String() != "http://convert.internal:3000/prefix/forms/chromium/convert/html" {
			t.Fatal("origin/auth")
		}
		names, fields, raw := multipartParts(t, r)
		if len(names) != 2 || names[0] != "index.html" || names[1] != "图\".png" || fields["landscape"] != "false" || fields["printBackground"] != "false" || fields["preferCssPageSize"] != "false" {
			t.Fatalf("parts %v %v", names, fields)
		}
		if bytes.Contains(raw, []byte("filename*=")) || !bytes.Contains(raw, []byte(`filename="图\".png"`)) {
			t.Fatal("unicode filename header")
		}
		if strings.Contains(string(raw), "secret") {
			t.Fatal("auth in form")
		}
		return response("application/pdf", testPDF), nil
	})
	ctx := ctxFor("png")
	out, e := c.htmlToPDF(ctx, &HTMLToPdfIn{HTML: "<img src='图&quot;.png'>", Assets: []*plugin.File{{ID: "asset", Name: "图\".png"}}, Options: schema.HtmlOptions{Landscape: &no, PrintBackground: &no, PreferCSSPageSize: &no}, OutputName: "报告.pdf"})
	if e != nil || out.File.Name != "报告.pdf" {
		t.Fatalf("%v %v", out, e)
	}
}
func TestOfficeWire(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/forms/libreoffice/convert" {
			t.Fatal(r.URL)
		}
		names, fields, raw := multipartParts(t, r)
		if len(names) != 1 || names[0] != "报告.docx" || fields[names[0]] != "office-bytes" || !bytes.Contains(raw, []byte(`filename="报告.docx"`)) {
			t.Fatal("office payload")
		}
		return response("application/pdf", testPDF), nil
	})
	out, e := c.officeToPDF(ctxFor("office-bytes"), &OfficeToPdfIn{File: &plugin.File{ID: "doc", Name: "报告.docx"}})
	if e != nil || out.File.Size != int64(len(testPDF)) {
		t.Fatalf("%v %v", out, e)
	}
}
func TestMergeOrdering(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/forms/pdfengines/merge" {
			t.Fatal(r.URL)
		}
		names, fields, _ := multipartParts(t, r)
		if strings.Join(names, ",") != "0001.pdf,0002.pdf" || fields["0001.pdf"] != testPDF+"first" || fields["0002.pdf"] != testPDF+"second" {
			t.Fatalf("%v %v", names, fields)
		}
		return response("application/pdf", testPDF), nil
	})
	_, e := c.mergePDFs(ctxFor(""), &MergePdfsIn{Files: []*plugin.File{{Name: "z.pdf", Data: []byte(testPDF + "first")}, {Name: "a.pdf", Data: []byte(testPDF + "second")}}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestGeneratedHTMLDispatch(t *testing.T) {
	t.Setenv("SOKEL_GOTENBERG_URL", "http://convert.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		_, fields, _ := multipartParts(t, r)
		if fields["printBackground"] != "false" || fields["图.png"] != "png" {
			t.Fatal(fields)
		}
		return response("application/pdf", testPDF), nil
	})
	vars, e := dispatch(t, "html_to_pdf", `{"html":"<h1>Hello</h1>","assets":[{"id":"asset","name":"图.png","size":3}],"options":{"print_background":false},"output_name":"out.pdf"}`, ctxFor("png"))
	if e != nil || vars["file"] == nil {
		t.Fatalf("%v %v", vars, e)
	}
}
func TestInvalidConversionInputs(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })
	for _, in := range []*HTMLToPdfIn{{}, {HTML: "\x00"}, {HTML: "x", OutputName: "../x.pdf"}, {HTML: "x", Assets: []*plugin.File{{Name: "index.html"}}}, {HTML: "x", Assets: make([]*plugin.File, 21)}, {HTML: strings.Repeat("x", (2<<20)+1)}} {
		if _, e := c.htmlToPDF(ctxFor(""), in); e == nil {
			t.Fatal("accepted invalid HTML")
		}
	}
	for _, in := range []*OfficeToPdfIn{{}, {File: &plugin.File{Name: "a.exe", ID: "x"}}, {File: &plugin.File{Name: "a.docx", ID: "x"}}} {
		if _, e := c.officeToPDF(ctxFor(""), in); e == nil {
			t.Fatal("accepted office input")
		}
	}
	for _, in := range []*MergePdfsIn{{}, {Files: []*plugin.File{{ID: "a", Name: "a.pdf"}, {ID: "b", Name: "b.pdf"}}}} {
		if _, e := c.mergePDFs(ctxFor("not-pdf"), in); e == nil {
			t.Fatal("accepted merge input")
		}
	}
}
func TestPDFValidationAndStorageFailure(t *testing.T) {
	for _, body := range []string{"", "not pdf", "%PDF-1.4\nincomplete"} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/pdf", body), nil })
		if _, e := c.htmlToPDF(ctxFor(""), &HTMLToPdfIn{HTML: "x"}); e == nil {
			t.Fatal("accepted invalid PDF")
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) { return response("application/pdf", testPDF), nil })
	ctx := ctxFor("")
	ctx.failUpload = true
	if _, e := c.htmlToPDF(ctx, &HTMLToPdfIn{HTML: "x"}); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}

func TestHealthDoesNotInventSuccess(t *testing.T) {
	for _, body := range []string{`{"status":"down"}`, `{"status":"unknown"}`, `{}`, `not-json`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", body), nil })
		out, e := c.healthCheck(ctxFor(""), &HealthCheckIn{})
		if e != nil || out.OK || out.Message == "" {
			t.Fatalf("%v %v", out, e)
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) {
		r := response("text/plain", "private")
		r.StatusCode = 503
		return r, nil
	})
	out, e := c.healthCheck(ctxFor(""), &HealthCheckIn{})
	if e != nil || out.OK || strings.Contains(out.Message, "private") {
		t.Fatalf("%v %v", out, e)
	}
}
func TestTotalInputCapsAndAssetCollision(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) {
		t.Fatal("oversize upload reached server")
		return nil, nil
	})
	data := append([]byte("%PDF-"), bytes.Repeat([]byte("x"), 17<<20)...)
	files := []*plugin.File{{Name: "a.pdf", Data: data}, {Name: "b.pdf", Data: data}}
	if _, e := c.mergePDFs(ctxFor(""), &MergePdfsIn{Files: files}); e == nil {
		t.Fatal("merge total cap")
	}
	if _, e := c.htmlToPDF(ctxFor(""), &HTMLToPdfIn{HTML: "x", Assets: files}); e == nil {
		t.Fatal("asset total cap")
	}
	if _, e := c.htmlToPDF(ctxFor("x"), &HTMLToPdfIn{HTML: "x", Assets: []*plugin.File{{ID: "a", Name: "A.png"}, {ID: "b", Name: "a.png"}}}); e == nil {
		t.Fatal("asset collision")
	}
}
func TestPDFWrongMIMEFailsBeforeStorage(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) { return response("application/zip", testPDF), nil })
	ctx := ctxFor("")
	if _, e := c.htmlToPDF(ctx, &HTMLToPdfIn{HTML: "x"}); e == nil || ctx.stored != nil {
		t.Fatal("wrong MIME accepted")
	}
}
