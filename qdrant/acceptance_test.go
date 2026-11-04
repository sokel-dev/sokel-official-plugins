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
		if r.Header.Get("api-key") != "secret-test" {
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
		wire(t, c, `GET`, `/collections`, ``, ``, `{"result":{"collections":[]},"status":"ok","time":0.001}`, 200)
		o, e := c.health_check(context.Background(), bind[HealthCheckIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
		if !o.OK {
			t.Fatal(o)
		}
	})
	t.Run("list_collections", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/collections`, ``, ``, `{"result":{"collections":[{"name":"books"}]},"status":"ok","time":0.001}`, 200)
		o, e := c.list_collections(context.Background(), bind[ListCollectionsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_collection", func(t *testing.T) {
		c := fresh()
		wire(t, c, `GET`, `/collections/books`, ``, ``, `{"result":{"config":{"params":{"vectors":{"distance":"Cosine","size":3}}},"indexed_vectors_count":0,"points_count":0,"status":"green"},"status":"ok","time":0.001}`, 200)
		o, e := c.get_collection(context.Background(), bind[GetCollectionIn](t, `{"collection":"books"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_collection", func(t *testing.T) {
		c := fresh()
		wire(t, c, `PUT`, `/collections/books`, `timeout=30`, `{"vectors":{"distance":"Cosine","size":3}}`, `{"result":true,"status":"ok","time":0.001}`, 200)
		o, e := c.create_collection(context.Background(), bind[CreateCollectionIn](t, `{"collection":"books","distance":"Cosine","size":3}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("query_points", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/collections/books/points/query`, `timeout=30`, `{"limit":10,"offset":0,"query":[0.9,0.1,0.1],"score_threshold":0,"with_payload":true,"with_vector":false}`, `{"result":{"points":[{"id":18446744073709551615,"payload":{"count":9007199254740993,"tag":"red"},"score":0.9}]},"status":"ok","time":0.001}`, 200)
		o, e := c.query_points(context.Background(), bind[QueryPointsIn](t, `{"collection":"books","options":{"score_threshold":0},"vector":[0.9,0.1,0.1]}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("upsert_points", func(t *testing.T) {
		c := fresh()
		wire(t, c, `PUT`, `/collections/books/points`, `timeout=30&wait=true`, `{"points":[{"id":18446744073709551615,"payload":{"count":9007199254740993,"flag":false},"vector":[0.9,0.1,0.1]}]}`, `{"result":{"operation_id":0,"status":"completed"},"status":"ok","time":0.001}`, 200)
		o, e := c.upsert_points(context.Background(), bind[UpsertPointsIn](t, `{"collection":"books","points":[{"id":"18446744073709551615","payload_json":"{\"count\":9007199254740993,\"flag\":false}","vector":[0.9,0.1,0.1]}]}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("retrieve_points", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/collections/books/points`, `timeout=30`, `{"ids":[18446744073709551615,"76874cce-1fb9-4e16-9b0b-f085ac06ed6f"],"with_payload":true,"with_vector":false}`, `{"result":[{"id":18446744073709551615,"payload":{"count":9007199254740993,"tag":"red"}}],"status":"ok","time":0.001}`, 200)
		o, e := c.retrieve_points(context.Background(), bind[RetrievePointsIn](t, `{"collection":"books","ids":["18446744073709551615","76874cce-1fb9-4e16-9b0b-f085ac06ed6f"]}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("scroll_points", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/collections/books/points/scroll`, `timeout=30`, `{"limit":1,"offset":0,"with_payload":true,"with_vector":false}`, `{"result":{"next_page_offset":null,"points":[{"id":18446744073709551615,"payload":{"count":9007199254740993,"tag":"red"}}]},"status":"ok","time":0.001}`, 200)
		o, e := c.scroll_points(context.Background(), bind[ScrollPointsIn](t, `{"collection":"books","options":{"limit":1,"offset":"0"}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("delete_points", func(t *testing.T) {
		c := fresh()
		wire(t, c, `POST`, `/collections/books/points/delete`, `timeout=30&wait=true`, `{"points":[18446744073709551615]}`, `{"result":{"operation_id":0,"status":"completed"},"status":"ok","time":0.001}`, 200)
		o, e := c.delete_points(context.Background(), bind[DeletePointsIn](t, `{"collection":"books","ids":["18446744073709551615"]}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
}
