package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/meilisearch/schema"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type captureEntry struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Query    string          `json:"query"`
	Request  json.RawMessage `json:"request,omitempty"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response"`
}

func localClient(t *testing.T) *client {
	t.Helper()
	target := os.Getenv("SOKEL_LIVE_MEILISEARCH_URL")
	if target == "" {
		t.Skip("set the explicit local acceptance URL to test an isolated service")
	}
	u, e := url.Parse(target)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("live tests require an isolated http://127.0.0.1:port URL")
	}
	c := newClient(Cred{APIKey: "wave6-local-test-only-key"})
	c.baseURL = target
	entries := []captureEntry{}
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		request, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(request))
		res, e := http.DefaultTransport.RoundTrip(r)
		if e != nil {
			return nil, e
		}
		reply, e := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
		res.Body.Close()
		if e != nil {
			return nil, e
		}
		res.Body = io.NopCloser(bytes.NewReader(reply))
		entries = append(entries, captureEntry{r.Method, r.URL.Path, r.URL.RawQuery, json.RawMessage(request), res.StatusCode, json.RawMessage(reply)})
		return res, nil
	})
	t.Cleanup(func() {
		folder := os.Getenv("SOKEL_LIVE_FIXTURE_DIR")
		if folder == "" {
			return
		}
		abs, e := filepath.Abs(folder)
		if e != nil || (!strings.HasPrefix(abs, "/tmp/") && !strings.HasPrefix(abs, "/private/tmp/")) {
			t.Error("fixture directory must be under /tmp")
			return
		}
		if e = os.MkdirAll(abs, 0700); e != nil {
			t.Error(e)
			return
		}
		b, e := json.MarshalIndent(entries, "", "  ")
		if e != nil {
			t.Error(e)
			return
		}
		if e = os.WriteFile(filepath.Join(abs, "meilisearch-operation-captures.json"), append(b, '\n'), 0600); e != nil {
			t.Error(e)
		}
	})
	return c
}
func waitLocalTask(t *testing.T, c *client, ctx context.Context, id string) *GetTaskOut {
	t.Helper()
	for i := 0; i < 100; i++ {
		v, e := c.get_task(ctx, &GetTaskIn{TaskUID: id})
		if e != nil {
			t.Fatal(e)
		}
		if v.Finished {
			return v
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("local task did not finish within bounded acceptance polling")
	return nil
}
func TestLocalServiceAcceptance(t *testing.T) {
	c := localClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := fmt.Sprintf("sokel_wave6_%d", time.Now().UnixNano())
	h, e := c.health_check(ctx, &HealthCheckIn{})
	if e != nil || !h.OK {
		t.Fatal(h, e)
	}
	created, e := c.create_index(ctx, &CreateIndexIn{IndexUID: name, PrimaryKey: "id"})
	if e != nil || created.Task.Status != "enqueued" {
		t.Fatal(created, e)
	}
	if v := waitLocalTask(t, c, ctx, created.Task.UID); !v.Succeeded {
		t.Fatal(v)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := c.call(cleanup, "DELETE", "/indexes/"+name, nil, nil, 202); e != nil {
			t.Error(e)
		}
	})
	index, e := c.get_index(ctx, &GetIndexIn{IndexUID: name})
	if e != nil || index.Index.PrimaryKey == nil || *index.Index.PrimaryKey != "id" {
		t.Fatal(index, e)
	}
	list, e := c.list_indexes(ctx, &ListIndexesIn{Page: schema.Page{Limit: ptr(1)}})
	if e != nil || len(list.Indexes) != 1 {
		t.Fatal(list, e)
	}
	add, e := c.add_documents(ctx, &AddDocumentsIn{IndexUID: name, DocumentsJSON: `[{"id":"one","title":"Book one","available":false},{"id":9007199254740993,"title":"Book two"}]`})
	if e != nil {
		t.Fatal(e)
	}
	if v := waitLocalTask(t, c, ctx, add.Task.UID); !v.Succeeded {
		t.Fatal(v)
	}
	doc, e := c.get_document(ctx, &GetDocumentIn{IndexUID: name, DocumentID: "9007199254740993"})
	if e != nil || !strings.Contains(doc.DocumentJSON, "9007199254740993") {
		t.Fatal(doc, e)
	}
	found, e := c.search(ctx, &SearchIn{IndexUID: name, Query: "Book", Options: schema.SearchOptions{Limit: ptr(1), Offset: ptr(0)}})
	if e != nil || len(found.Hits) != 1 || !found.MayHaveMore {
		t.Fatal(found, e)
	}
	next, e := c.search(ctx, &SearchIn{IndexUID: name, Query: "Book", Options: schema.SearchOptions{Limit: ptr(1), Offset: &found.NextOffset}})
	if e != nil || len(next.Hits) != 1 || next.Hits[0].JSON == found.Hits[0].JSON {
		t.Fatal(next, e)
	}
	invalid, e := c.add_documents(ctx, &AddDocumentsIn{IndexUID: name, DocumentsJSON: `[{"title":"missing primary key"}]`})
	if e != nil {
		t.Fatal(e)
	}
	if v := waitLocalTask(t, c, ctx, invalid.Task.UID); v.Succeeded || v.Task.Status != "failed" || v.Task.ErrorCode == nil {
		t.Fatal(v)
	}
	deleted, e := c.delete_document(ctx, &DeleteDocumentIn{IndexUID: name, DocumentID: "one"})
	if e != nil {
		t.Fatal(e)
	}
	if v := waitLocalTask(t, c, ctx, deleted.Task.UID); !v.Succeeded {
		t.Fatal(v)
	}
	if _, e := c.get_document(ctx, &GetDocumentIn{IndexUID: name, DocumentID: "one"}); e == nil {
		t.Fatal("document remains after completed deletion")
	}
}
