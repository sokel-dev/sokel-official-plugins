package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestDriveListPagingAndUploadBytes(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if r.URL.Query().Get("pageToken") != "p2" || r.URL.Query().Get("q") != "name contains 'report'" {
				t.Error("pagination/query missing")
			}
			reply(w, `{"files":[{"id":"f","name":"report.pdf","mimeType":"application/pdf","size":"3"}],"nextPageToken":"p3"}`)
			return
		}
		if r.URL.Path != "/upload/drive/v3/files" || r.URL.Query().Get("uploadType") != "multipart" {
			t.Error("upload endpoint")
		}
		mt, p, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || mt != "multipart/related" {
			t.Fatal("multipart related required")
		}
		reader := multipart.NewReader(r.Body, p["boundary"])
		part, e := reader.NextPart()
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(part)
		if !strings.Contains(string(b), `"parents":["folder"]`) {
			t.Errorf("metadata %s", b)
		}
		part, e = reader.NextPart()
		if e != nil {
			t.Fatal(e)
		}
		b, _ = io.ReadAll(part)
		if string(b) != "pdf" || part.Header.Get("Content-Type") != "application/pdf" {
			t.Error("binary payload")
		}
		reply(w, `{"id":"created","name":"report.pdf","mimeType":"application/pdf"}`)
	})
	o, e := a.list(context.Background(), &ListFilesIn{Query: "name contains 'report'", PageToken: "p2", PageSize: 20})
	if e != nil || o.NextPageToken != "p3" || o.Files[0].Size != "3" {
		t.Fatalf("list %v %v", o, e)
	}
	u, e := a.upload(context.Background(), "report.pdf", "application/pdf", "folder", []byte("pdf"))
	if e != nil || u.FileID != "created" {
		t.Fatalf("upload %v %v", u, e)
	}
}
func TestDriveDownloadExportAndLimits(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/export") {
			if r.URL.Query().Get("mimeType") != "application/pdf" {
				t.Error("export MIME")
			}
			_, _ = w.Write([]byte("export"))
			return
		}
		if r.URL.Query().Get("alt") == "media" {
			_, _ = w.Write([]byte("blob"))
			return
		}
		reply(w, `{"id":"f","name":"file.bin","mimeType":"application/octet-stream"}`)
	})
	b, n, m, e := a.download(context.Background(), "f")
	if e != nil || string(b) != "blob" || n != "file.bin" || m != "application/octet-stream" {
		t.Fatalf("download %s %s %s %v", b, n, m, e)
	}
	b, e = a.export(context.Background(), "doc", "application/pdf")
	if e != nil || string(b) != "export" {
		t.Fatalf("export %s %v", b, e)
	}
	if _, e = a.upload(context.Background(), "large", "text/plain", "", make([]byte, 5*1024*1024+1)); e == nil {
		t.Fatal("oversize upload accepted")
	}
}
func TestDriveChangesDrainBeforeCommit(t *testing.T) {
	pages := 0
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "startPageToken") {
			reply(w, `{"startPageToken":"start"}`)
			return
		}
		pages++
		switch r.URL.Query().Get("pageToken") {
		case "start":
			reply(w, `{"changes":[{"fileId":"a","time":"2026-01-01T00:00:00Z","file":{"id":"a","name":"A"}}],"nextPageToken":"second"}`)
		case "second":
			reply(w, `{"changes":[{"fileId":"b","time":"2026-01-01T00:00:01Z","removed":true}],"newStartPageToken":"end"}`)
		default:
			t.Error("wrong cursor")
		}
	})
	cur, e := a.poll(context.Background(), Cred{}, "", func(string, *FileChangedEvent) error { t.Fatal("baseline emitted"); return nil })
	if e != nil || cur != "start" {
		t.Fatalf("start %q %v", cur, e)
	}
	events := []string{}
	next, e := a.poll(context.Background(), Cred{}, cur, func(_ string, e *FileChangedEvent) error { events = append(events, e.FileID); return nil })
	if e != nil || next != "end" || len(events) != 2 || pages != 2 {
		t.Fatalf("drain %q %v %v", next, events, e)
	}
	if _, e = a.poll(context.Background(), Cred{}, cur, func(string, *FileChangedEvent) error { return errors.New("failed") }); e == nil {
		t.Fatal("must not commit failed event")
	}
}
func TestDriveRejectsUnsafeFilesAndPropagatesFailures(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, `{"id":"f","name":"a.txt","mimeType":"text/plain"}`)
	})
	ctx := &fileCtx{Context: context.Background()}
	if _, e := uploadWith(ctx, a, &UploadFileIn{}); e == nil {
		t.Fatal("nil file accepted")
	}
	if _, e := uploadWith(ctx, a, &UploadFileIn{File: &plugin.File{Size: maxUpload + 1}}); e == nil {
		t.Fatal("oversized file fetched")
	}
	if ctx.fetched != 0 {
		t.Fatal("validation must happen before file fetch")
	}
	ctx.fail = true
	if _, e := uploadWith(ctx, a, &UploadFileIn{File: &plugin.File{Name: "a.txt"}}); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatalf("fetch error %v", e)
	}
	if _, e := downloadWith(ctx, a, &DownloadFileIn{FileID: "f", Filename: "../../safe.txt"}); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatalf("store error %v", e)
	}
	if _, e := exportWith(ctx, a, &ExportFileIn{FileID: "f", MimeType: "application/pdf", Filename: "x.pdf"}); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatalf("export store error %v", e)
	}
	if _, e := exportWith(ctx, a, &ExportFileIn{}); e == nil {
		t.Fatal("missing export name accepted")
	}
	if outputName("../../safe.txt", "fallback") != "safe.txt" || outputName("", "fallback") != "fallback" {
		t.Fatal("unsafe file name")
	}
	for _, args := range []struct{ name, mime string }{{"", "text/plain"}, {"x", "invalid mime"}} {
		if _, e := a.upload(context.Background(), args.name, args.mime, "", nil); e == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	if _, e := a.export(context.Background(), "f", "invalid mime"); e == nil {
		t.Fatal("invalid MIME accepted")
	}
	if _, e := a.list(context.Background(), &ListFilesIn{PageSize: -1}); e == nil {
		t.Fatal("negative page size accepted")
	}
}
func TestDriveRejectsWorkspaceBinaryDownloadAndMissingCursor(t *testing.T) {
	body := `{"mimeType":"application/vnd.google-apps.document"}`
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
	if _, _, _, e := a.download(context.Background(), "doc"); e == nil {
		t.Fatal("Workspace binary download accepted")
	}
	body = `{"mimeType":"text/plain","size":"999999999"}`
	if _, _, _, e := a.download(context.Background(), "large"); e == nil {
		t.Fatal("large download accepted")
	}
	body = `{}`
	if _, e := a.poll(context.Background(), Cred{}, "", func(string, *FileChangedEvent) error { return nil }); e == nil {
		t.Fatal("missing baseline token accepted")
	}
	if _, e := a.poll(context.Background(), Cred{}, "old", func(string, *FileChangedEvent) error { return nil }); e == nil {
		t.Fatal("missing final cursor accepted")
	}
}
