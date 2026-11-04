package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/meilisearch/schema"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCapturedServiceResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-service-captures.json")
	if e != nil {
		t.Fatal(e)
	}
	var entries []captureEntry
	if e = json.Unmarshal(b, &entries); e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	failed := false
	pending := false
	for _, r := range entries {
		parts := strings.Split(r.Path, "/")
		if r.Method == "DELETE" && len(parts) == 3 {
			continue
		}
		c := fresh()
		withReply(c, r.Status, string(r.Response))
		ctx := context.Background()
		kind := ""
		name := ""
		if len(parts) > 2 {
			name = parts[2]
		}
		switch {
		case r.Path == "/indexes" && r.Method == "GET":
			kind = "list"
			q, _ := url.ParseQuery(r.Query)
			l, _ := strconv.Atoi(q.Get("limit"))
			o, _ := strconv.Atoi(q.Get("offset"))
			_, e = c.list_indexes(ctx, &ListIndexesIn{Page: schema.Page{Limit: &l, Offset: &o}})
		case r.Path == "/indexes" && r.Method == "POST":
			kind = "create"
			var v struct {
				UID string `json:"uid"`
				Key string `json:"primaryKey"`
			}
			if e = json.Unmarshal(r.Request, &v); e != nil {
				t.Fatal(e)
			}
			_, e = c.create_index(ctx, &CreateIndexIn{IndexUID: v.UID, PrimaryKey: v.Key})
		case strings.HasPrefix(r.Path, "/tasks/"):
			kind = "task"
			var task *GetTaskOut
			task, e = c.get_task(ctx, &GetTaskIn{TaskUID: name})
			if e == nil {
				if task.Task.Status == "failed" {
					failed = true
					if task.Succeeded || !task.Finished || task.Task.ErrorCode == nil {
						t.Fatal(task)
					}
				}
				if !task.Finished {
					pending = true
					if task.Succeeded {
						t.Fatal(task)
					}
				}
			}
		case len(parts) == 3 && r.Method == "GET":
			kind = "metadata"
			_, e = c.get_index(ctx, &GetIndexIn{IndexUID: name})
		case strings.HasSuffix(r.Path, "/search"):
			kind = "search"
			var v struct {
				Query  string `json:"q"`
				Limit  int    `json:"limit"`
				Offset int    `json:"offset"`
			}
			if e = json.Unmarshal(r.Request, &v); e != nil {
				t.Fatal(e)
			}
			_, e = c.search(ctx, &SearchIn{IndexUID: name, Query: v.Query, Options: schema.SearchOptions{Limit: &v.Limit, Offset: &v.Offset}})
		case strings.HasSuffix(r.Path, "/documents") && r.Method == "POST":
			kind = "add"
			_, e = c.add_documents(ctx, &AddDocumentsIn{IndexUID: name, DocumentsJSON: string(r.Request)})
		case len(parts) == 5 && parts[3] == "documents" && r.Method == "GET":
			kind = "get"
			_, e = c.get_document(ctx, &GetDocumentIn{IndexUID: name, DocumentID: parts[4]})
		case len(parts) == 5 && parts[3] == "documents" && r.Method == "DELETE":
			kind = "delete"
			_, e = c.delete_document(ctx, &DeleteDocumentIn{IndexUID: name, DocumentID: parts[4]})
		default:
			t.Fatalf("unrecognized captured route %s %s", r.Method, r.Path)
		}
		if (e == nil) != (r.Status < 300) {
			t.Fatalf("%s %s: %v", r.Method, r.Path, e)
		}
		counts[kind]++
	}
	if len(counts) != 8 || !failed || !pending {
		t.Fatal("missing real response families or task states", counts, failed, pending)
	}
}
