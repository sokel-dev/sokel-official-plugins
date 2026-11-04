package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

const taskUUID = "12345678-1234-4234-8234-123456789abc"

func TestREDUploadAsyncUnicode(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/prefix/api/documents/post_document/" || r.Header.Get("Accept") != "application/json; version=10" || r.Header.Get("Authorization") != "Token secret" || r.GetBody != nil {
			t.Fatal("incorrect upload request")
		}
		if !strings.Contains(string(b), `filename="报告.pdf"`) || strings.Contains(string(b), "filename*") {
			t.Fatalf("filename bytes: %s", b)
		}
		return response("application/json", `"`+taskUUID+`"`), nil
	})
	out, e := c.uploadDocument(ctxFor("%PDF-test"), &UploadDocumentIn{File: &plugin.File{ID: "input", Name: "报告.pdf"}})
	if e != nil {
		t.Fatal(e)
	}
	if !out.Accepted || out.TaskID != taskUUID {
		t.Fatal(out)
	}
}
func TestREDTaskAPI10(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("task_id") != taskUUID {
			t.Fatal("missing task filter")
		}
		return response("application/json", `{"count":1,"next":null,"results":[{"task_id":"`+taskUUID+`","task_type":"consume_file","status":"success","related_document_ids":[9007199254740993]}]}`), nil
	})
	out, e := c.getIngestionTask(ctxFor(""), &GetIngestionTaskIn{TaskID: taskUUID})
	if e != nil {
		t.Fatal(e)
	}
	if !out.Found || !out.Task.Complete || !out.Task.Succeeded || out.Task.RelatedDocumentIDs[0] != "9007199254740993" {
		t.Fatal(out)
	}
}
