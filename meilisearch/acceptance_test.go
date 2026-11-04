package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func bind[T any](t *testing.T, s string) *T {
	t.Helper()
	var x T
	if e := contract.BindInput(json.RawMessage(s), &x); e != nil {
		t.Fatal(e)
	}
	return &x
}
func fresh() *client {
	c := newClient(Cred{APIKey: "secret-test"})
	c.baseURL = "http://127.0.0.1:12345/prefix"
	return c
}
func ptr[T any](v T) *T { return &v }
func wire(t *testing.T, c *client, method, path, query, body, reply string, status int) {
	t.Helper()
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != method || r.URL.Path != "/prefix"+path || r.URL.RawQuery != query || r.URL.Host != "127.0.0.1:12345" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL)
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != body {
			t.Fatalf("body %s want %s", b, body)
		}
		if r.Header.Get("Authorization") != "Bearer secret-test" {
			t.Fatal("auth header")
		}
		if strings.Contains(r.URL.String(), "secret-test") {
			t.Fatal("credential in URL")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})
}
func TestDocumentedWireRequests(t *testing.T) {
	t.Run("health_check", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/indexes`, `limit=1&offset=0`, ``, `{"limit":1,"offset":0,"results":[],"total":0}`, 200)
		o, e := c.health_check(context.Background(), bind[HealthCheckIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
		if !o.OK {
			t.Fatal(o)
		}
	})
	t.Run("list_indexes", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/indexes`, `limit=20&offset=0`, ``, `{"limit":20,"offset":0,"results":[{"createdAt":"2024-08-08T17:05:55.791772Z","primaryKey":"id","uid":"movies","updatedAt":"2024-08-08T17:05:55.791772Z"}],"total":1}`, 200)
		o, e := c.list_indexes(context.Background(), bind[ListIndexesIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_index", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/indexes/movies`, ``, ``, `{"createdAt":"2024-08-08T17:05:55.791772Z","primaryKey":"id","uid":"movies","updatedAt":"2024-08-08T17:05:55.791772Z"}`, 200)
		o, e := c.get_index(context.Background(), bind[GetIndexIn](t, `{"index_uid":"movies"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_index", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/indexes`, ``, `{"primaryKey":"id","uid":"movies"}`, `{"enqueuedAt":"2024-08-08T17:05:55.791772Z","indexUid":"movies","status":"enqueued","taskUid":0,"type":"indexCreation"}`, 202)
		o, e := c.create_index(context.Background(), bind[CreateIndexIn](t, `{"index_uid":"movies","primary_key":"id"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("search", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/indexes/movies/search`, ``, `{"limit":20,"offset":0,"q":"ninja"}`, `{"estimatedTotalHits":1,"hits":[{"available":false,"id":9007199254740993,"title":"Movie"}],"limit":20,"offset":0,"processingTimeMs":0,"query":"ninja"}`, 200)
		o, e := c.search(context.Background(), bind[SearchIn](t, `{"index_uid":"movies","options":{"offset":0},"query":"ninja"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_document", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/indexes/movies/documents/9007199254740993`, ``, ``, `{"id":9007199254740993,"title":"Movie"}`, 200)
		o, e := c.get_document(context.Background(), bind[GetDocumentIn](t, `{"document_id":"9007199254740993","index_uid":"movies"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("add_documents", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/indexes/movies/documents`, `primaryKey=id`, `[{"id":9007199254740993,"available":false}]`, `{"enqueuedAt":"2024-08-08T17:05:55.791772Z","indexUid":"movies","status":"enqueued","taskUid":0,"type":"documentAdditionOrUpdate"}`, 202)
		o, e := c.add_documents(context.Background(), bind[AddDocumentsIn](t, `{"documents_json":"[{\"id\":9007199254740993,\"available\":false}]","index_uid":"movies","primary_key":"id"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("delete_document", func(t *testing.T) {
		c := fresh()
		wire(t, c, `DELETE`, `/indexes/movies/documents/9007199254740993`, ``, ``, `{"enqueuedAt":"2024-08-08T17:05:55.791772Z","indexUid":"movies","status":"enqueued","taskUid":0,"type":"documentDeletion"}`, 202)
		o, e := c.delete_document(context.Background(), bind[DeleteDocumentIn](t, `{"document_id":"9007199254740993","index_uid":"movies"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_task", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/tasks/0`, ``, ``, `{"enqueuedAt":"2024-08-08T17:05:55.791772Z","error":null,"finishedAt":"2024-08-08T17:05:55.791772Z","indexUid":"movies","startedAt":"2024-08-08T17:05:55.791772Z","status":"succeeded","type":"indexCreation","uid":0}`, 200)
		o, e := c.get_task(context.Background(), bind[GetTaskIn](t, `{"task_uid":"0"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
		if !o.Finished || !o.Succeeded || o.Task.UID != "0" {
			t.Fatal(o)
		}
	})
}
