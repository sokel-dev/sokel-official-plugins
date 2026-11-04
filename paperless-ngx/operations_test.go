package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/paperless-ngx/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDocumentAndTagPages(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("query") != "合同" {
			t.Fatal("query missing")
		}
		return response("application/json", `{"count":2,"next":"http://service.internal:3000/prefix/api/documents/?page=2&page_size=50&query=%E5%90%88%E5%90%8C","results":[{"id":9007199254740993,"title":"合同","tags":[2],"correspondent":3,"document_type":4,"created":"1960-02-01"}]}`), nil
	})
	out, e := c.listDocuments(ctxFor(""), &ListDocumentsIn{Query: "合同"})
	if e != nil || out.NextPage != 2 || out.Documents[0].ID != "9007199254740993" || *out.Documents[0].CorrespondentID != "3" {
		t.Fatal(out, e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"count":1,"next":null,"results":[{"id":1,"name":"Inbox","color":"#ffffff","is_inbox_tag":false}]}`), nil
	})
	tags, e := c.listTags(ctxFor(""), &ListTagsIn{})
	if e != nil || tags.NextPage != 0 || tags.Tags[0].IsInboxTag {
		t.Fatal(tags, e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"id":1,"title":"Doc","tags":[],"correspondent":null}`), nil
	})
	doc, e := c.getDocument(ctxFor(""), &GetDocumentIn{DocumentID: "1"})
	if e != nil || doc.Document.CorrespondentID != nil {
		t.Fatal(doc, e)
	}
}
func TestGeneratedMetadataClearsTagsAndPreservesFalse(t *testing.T) {
	t.Setenv("SOKEL_PAPERLESS_URL", "http://service.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	calls := 0
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.GetBody != nil || r.Header.Get("Authorization") != "Token secret" || r.Header.Get("Accept") != "application/json; version=10" {
			t.Fatal("auth, version or replay")
		}
		if r.Method == "PATCH" {
			if string(b) != `{"correspondent":9007199254740993,"tags":[],"title":""}` {
				t.Fatalf("metadata wire: %s", b)
			}
			return response("application/json", `{"id":1,"title":"","tags":[],"correspondent":9007199254740993}`), nil
		}
		if !strings.Contains(string(b), `"is_inbox_tag":false`) || !strings.Contains(string(b), `"color":"#00aaFF"`) {
			t.Fatal(string(b))
		}
		res := response("application/json", `{"id":9007199254740993,"name":"Archive","color":"#00aaFF","is_inbox_tag":false}`)
		res.StatusCode = 201
		return res, nil
	})
	for _, tc := range []struct{ op, in string }{{"update_document", `{"document_id":"1","metadata":{"title":"","tags":[],"correspondent_id":"9007199254740993"}}`}, {"create_tag", `{"name":"Archive","options":{"color":"#00aaFF","is_inbox_tag":false}}`}} {
		vars, e := dispatch(t, tc.op, tc.in, ctxFor(""))
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(vars)
		if strings.Contains(string(b), "secret") || !strings.Contains(string(b), "9007199254740993") {
			t.Fatalf("generated output: %s", b)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestUploadMetadataAndFiles(t *testing.T) {
	title, correspondent, docType := "报告", "2", "3"
	calls := 0
	c := testClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if e := r.ParseMultipartForm(2 << 20); e != nil {
			t.Fatal(e)
		}
		if r.FormValue("title") != title || r.FormValue("correspondent") != "2" || r.FormValue("document_type") != "3" || len(r.MultipartForm.Value["tags"]) != 2 {
			t.Fatal("metadata fields missing")
		}
		return response("application/json", `"`+taskUUID+`"`), nil
	})
	in := &UploadDocumentIn{File: &plugin.File{ID: "file", Name: `报告".pdf`, Size: 3, Mime: "application/pdf"}, Options: schema.UploadOptions{Title: &title, CorrespondentID: &correspondent, DocumentTypeID: &docType, Tags: []string{"1", "2"}}}
	out, e := c.uploadDocument(ctxFor("PDF"), in)
	if e != nil || !out.Accepted {
		t.Fatal(out, e)
	}
	for _, f := range []*plugin.File{nil, {ID: "f", Name: "../x"}, {URL: "http://evil/secret", Name: "a.pdf"}, {ID: "f", Name: "a", Size: maxFile + 1}, {ID: "f", Name: "a", Size: 10}} {
		if _, e := c.uploadDocument(ctxFor("PDF"), &UploadDocumentIn{File: f}); e == nil {
			t.Fatal("invalid file accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid file reached server")
	}
	ctx := ctxFor("PDF")
	ctx.failFetch = true
	if _, e := c.uploadDocument(ctx, in); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"task_id":"`+taskUUID+`"}`), nil
	})
	if _, e := c.uploadDocument(ctxFor("PDF"), in); e == nil {
		t.Fatal("wrong upload receipt accepted")
	}
}
func TestTaskLifecycleAndNotFound(t *testing.T) {
	for _, status := range []string{"pending", "started", "success", "failure", "revoked"} {
		c := testClient(func(*http.Request) (*http.Response, error) {
			return response("application/json", `{"count":1,"next":null,"results":[{"task_id":"`+taskUUID+`","task_type":"consume_file","status":"`+status+`","related_document_ids":[],"date_done":null}]}`), nil
		})
		o, e := c.getIngestionTask(ctxFor(""), &GetIngestionTaskIn{TaskID: taskUUID})
		if e != nil || !o.Found || o.Task.Succeeded != (status == "success") || o.Task.Complete != (status != "pending" && status != "started") {
			t.Fatal(o, e)
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"count":0,"next":null,"results":[]}`), nil
	})
	if o, e := c.getIngestionTask(ctxFor(""), &GetIngestionTaskIn{TaskID: taskUUID}); e != nil || o.Found {
		t.Fatal(o, e)
	}
	for _, raw := range []string{`[]`, `{"count":1,"next":null,"results":[]}`, `{"count":1,"next":null,"results":[{"task_id":"` + taskUUID + `","task_type":"consume_file","status":"SUCCESS"}]}`, `{"count":1,"next":null,"results":[{"task_id":"different","task_type":"consume_file","status":"pending"}]}`, `{"count":1,"next":null,"results":[{"task_id":"` + taskUUID + `","task_type":"mail_fetch","status":"pending"}]}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", raw), nil })
		if _, e := c.getIngestionTask(ctxFor(""), &GetIngestionTaskIn{TaskID: taskUUID}); e == nil {
			t.Fatal("invalid task shape accepted")
		}
	}
}
func TestDownloadPlatformStorage(t *testing.T) {
	f := false
	version := "3"
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/api/documents/1/download/" || r.URL.Query().Get("original") != "false" || r.URL.Query().Get("version") != "3" {
			t.Fatal("download options missing")
		}
		res := response("application/pdf", "%PDF-document")
		res.Header.Set("Content-Disposition", `attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf`)
		return res, nil
	})
	ctx := ctxFor("")
	in := &DownloadDocumentIn{DocumentID: "1", Options: schema.DownloadOptions{Original: &f, VersionID: &version}}
	o, e := c.downloadDocument(ctx, in)
	if e != nil || o.File.ID != "stored" || o.File.Name != "报告.pdf" || string(ctx.stored) != "%PDF-document" {
		t.Fatal(o, e)
	}
	ctx.failUpload = true
	if _, e := c.downloadDocument(ctx, in); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	for _, h := range []string{"", `inline; filename="a.pdf"`, `attachment; filename="../x"`} {
		c := testClient(func(*http.Request) (*http.Response, error) {
			s := response("text/html", "Login page")
			s.Header.Set("Content-Disposition", h)
			return s, nil
		})
		if _, e := c.downloadDocument(ctxFor(""), &DownloadDocumentIn{DocumentID: "1"}); e == nil {
			t.Fatal("invalid download accepted")
		}
	}
}
func TestPaperlessErrorsAndValidation(t *testing.T) {
	bad := testClient(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input made request"); return nil, nil })
	zero := 0
	if _, e := bad.listDocuments(ctxFor(""), &ListDocumentsIn{Pagination: schema.PageOptions{PageSize: &zero}}); e == nil {
		t.Fatal("zero page size")
	}
	if _, e := bad.getDocument(ctxFor(""), &GetDocumentIn{DocumentID: "1/2"}); e == nil {
		t.Fatal("path injection")
	}
	if _, e := bad.updateDocument(ctxFor(""), &UpdateDocumentIn{DocumentID: "1"}); e == nil {
		t.Fatal("empty patch")
	}
	dup := []string{"1", "1"}
	if _, e := bad.updateDocument(ctxFor(""), &UpdateDocumentIn{DocumentID: "1", Metadata: schema.Metadata{Tags: &dup}}); e == nil {
		t.Fatal("duplicate tags")
	}
	color := "red"
	if _, e := bad.createTag(ctxFor(""), &CreateTagIn{Name: "Tag", Options: schema.TagOptions{Color: &color}}); e == nil {
		t.Fatal("bad color")
	}
	if _, e := bad.getIngestionTask(ctxFor(""), &GetIngestionTaskIn{TaskID: "notuuid"}); e == nil {
		t.Fatal("bad task")
	}
	for _, raw := range []string{`null`, `{}`, `{"count":0,"results":[]}`, `{"count":-1,"next":null,"results":[]}`, `{"count":1,"next":"http://evil/?token=secret","results":[]}`, `{"count":0,"next":null,"results":[{"id":1}]}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", raw), nil })
		if _, e := c.listDocuments(ctxFor(""), &ListDocumentsIn{}); e == nil {
			t.Fatalf("bad shape accepted %s", raw)
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) {
		s := response("application/json", `{"count":0,"next":null,"results":[]}`)
		s.Header.Set("X-Api-Version", "9")
		return s, nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || o.OK {
		t.Fatal(o, e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"count":0,"next":null,"results":[]}`), nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || !o.OK {
		t.Fatal(o, e)
	}
}
