package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestOutlookWriteValidation(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid write reached network") })
	c := context.Background()
	for _, in := range []SendMailIn{{Subject: "s", Body: "b"}, {To: []string{"Bad <user@example.com>"}, Subject: "s", Body: "b"}, {To: []string{"user@example.com"}, Subject: " ", Body: "b"}, {To: []string{"user@example.com"}, Subject: "s", Body: "b", BodyType: "markdown"}} {
		if _, e := a.send_mail(c, &in); e == nil {
			t.Fatal("invalid mail accepted")
		}
	}
	for _, in := range []CreateEventIn{{Subject: "s", Start: "2026-11-03T10:00:00", End: "2026-11-03T11:00:00Z"}, {Subject: "s", Start: "2026-11-03T11:00:00Z", End: "2026-11-03T10:00:00Z"}, {Subject: "s", Start: "2026-11-03T10:00:00Z", End: "2026-11-03T11:00:00Z", CalendarID: "../other"}} {
		if _, e := a.create_event(c, &in); e == nil {
			t.Fatal("invalid event accepted")
		}
	}
}
func TestAttachmentTypesSizeAndStorageFailure(t *testing.T) {
	for _, meta := range []string{`{"id":"a","@odata.type":"#microsoft.graph.itemAttachment"}`, `{"id":"a","@odata.type":"#microsoft.graph.referenceAttachment"}`, `{"id":"a","@odata.type":"#microsoft.graph.fileAttachment","size":16777217}`} {
		calls := 0
		a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
			if identity(t, w, r) {
				return
			}
			calls++
			if strings.HasSuffix(r.URL.Path, "/$value") {
				t.Fatal("unsupported attachment downloaded")
			}
			reply(w, meta)
		})
		if _, e := a.download_attachment(&fileCtx{Context: context.Background()}, &DownloadAttachmentIn{MessageID: "m", AttachmentID: "a"}); e == nil || calls != 1 {
			t.Fatal(e, calls)
		}
	}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/$value") {
			w.Write([]byte("abc"))
			return
		}
		reply(w, `{"id":"a","@odata.type":"#microsoft.graph.fileAttachment","name":"../../bad.txt","size":3}`)
	})
	ctx := &fileCtx{Context: context.Background()}
	o, e := a.download_attachment(ctx, &DownloadAttachmentIn{MessageID: "m", AttachmentID: "a"})
	if e != nil || strings.ContainsAny(o.File.Name, "/\\") {
		t.Fatal(o, e)
	}
	ctx.fail = true
	if _, e = a.download_attachment(ctx, &DownloadAttachmentIn{MessageID: "m", AttachmentID: "a"}); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
}
func TestMailAcceptanceIsNotAssumed(t *testing.T) {
	posts := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		posts++
		w.WriteHeader(200)
		reply(w, "{}")
	})
	if _, e := a.send_mail(context.Background(), &SendMailIn{To: []string{"a@example.com"}, Subject: "s", Body: "b"}); e == nil || posts != 1 {
		t.Fatal(e, posts)
	}
}
