package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(fn func(*http.Request) (int, string)) *api {
	a := newAPI("https://example.supabase.co", "sb_secret_test", "")
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		s, b := fn(r)
		return &http.Response{StatusCode: s, Header: http.Header{"Content-Type": {"application/json"}, "Content-Range": {"20-21/30"}}, Body: io.NopCloser(strings.NewReader(b)), Request: r}, nil
	})
	return a
}
func TestSelectPaginationFiltersAndNewKeys(t *testing.T) {
	a := fakeAPI(func(r *http.Request) (int, string) {
		if r.Method != "GET" || r.URL.Path != "/rest/v1/items" || r.Header.Get("apikey") != "sb_secret_test" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept-Profile") != "public" {
			t.Fatal(r.Method, r.URL, r.Header)
		}
		q := r.URL.Query()
		if q.Get("offset") != "20" || q.Get("limit") != "2" || q.Get("name") != "eq.A&B" || q.Get("select") != "id,name" || q.Get("order") != "id.asc" {
			t.Fatal(q)
		}
		return 206, `[{"id":21,"name":"A&B"},{"id":22,"name":"A&B"}]`
	})
	out, e := a.selectRows(context.Background(), &SelectRowsIn{Table: "items", Select: "id,name", Order: "id.asc", Offset: 20, Limit: 2, Filters: []RowFilter{{Column: "name", Operator: "eq", Value: "A&B"}}})
	if e != nil {
		t.Fatal(e)
	}
	if out.Count != 2 || out.NextOffset != 22 || out.ContentRange != "20-21/30" {
		t.Fatal(out)
	}
}
func TestMutationFiltersAndRPCScalar(t *testing.T) {
	var reqs []string
	a := fakeAPI(func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, r.Method+" "+r.URL.String()+" "+r.Header.Get("Prefer")+" "+string(b))
		if strings.Contains(r.URL.Path, "/rpc/") {
			return 200, `42`
		}
		if r.Header.Get("Content-Profile") != "app" {
			t.Fatal(r.Header)
		}
		return 201, `[{"id":1,"active":false,"name":null}]`
	})
	c := context.Background()
	rows := []any{map[string]any{"id": 1, "active": false, "name": nil}}
	if _, e := a.insertRows(c, &InsertRowsIn{Table: "items", Schema: "app", Rows: rows}); e != nil {
		t.Fatal(e)
	}
	if _, e := a.upsertRows(c, &UpsertRowsIn{Table: "items", Schema: "app", Rows: rows, OnConflict: "id"}); e != nil {
		t.Fatal(e)
	}
	filters := []RowFilter{{Column: "id", Operator: "eq", Value: "1"}}
	if _, e := a.updateRows(c, &UpdateRowsIn{Table: "items", Schema: "app", Filters: filters, Values: map[string]any{"active": false, "name": nil}}); e != nil {
		t.Fatal(e)
	}
	if _, e := a.deleteRows(c, &DeleteRowsIn{Table: "items", Schema: "app", Filters: filters}); e != nil {
		t.Fatal(e)
	}
	r, e := a.callFunction(c, &CallFunctionIn{Function: "count_items", Arguments: map[string]any{"active": false}})
	if e != nil || r.ResultJSON != "42" {
		t.Fatal(r, e)
	}
	all := strings.Join(reqs, "\n")
	for _, want := range []string{"POST https://example.supabase.co/rest/v1/items?on_conflict=id", "resolution=merge-duplicates", "PATCH https://example.supabase.co/rest/v1/items?id=eq.1", "DELETE https://example.supabase.co/rest/v1/items?id=eq.1", `"active":false`, `"name":null`} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %s: %s", want, all)
		}
	}
}
func TestStorageAndSignedURL(t *testing.T) {
	a := fakeAPI(func(r *http.Request) (int, string) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		if r.Method != "POST" {
			t.Fatal(r.Method)
		}
		if strings.Contains(r.URL.Path, "/list/") {
			if b["offset"] != float64(3) || b["prefix"] != "folder" {
				t.Fatal(b)
			}
			return 200, `[{"name":"a.txt","id":"id","created_at":"2026-01-01","updated_at":"2026-01-02"}]`
		}
		if r.URL.EscapedPath() != "/storage/v1/object/sign/files/folder/a%20b.txt" || b["expiresIn"] != float64(60) {
			t.Fatal(r.URL, b)
		}
		return 200, `{"signedURL":"/object/sign/files/folder/a%20b.txt?token=signed"}`
	})
	out, e := a.listObjects(context.Background(), &ListObjectsIn{Bucket: "files", Prefix: "folder", Offset: 3})
	if e != nil || out.NextOffset != 4 || len(out.Objects) != 1 {
		t.Fatal(out, e)
	}
	u, e := a.signedURL(context.Background(), &CreateSignedURLIn{Bucket: "files", Path: "folder/a b.txt", ExpiresIn: 60})
	if e != nil || u.SignedURL != "https://example.supabase.co/storage/v1/object/sign/files/folder/a%20b.txt?token=signed" {
		t.Fatal(u, e)
	}
}
func TestInvalidMutationsNeverSend(t *testing.T) {
	a := fakeAPI(func(*http.Request) (int, string) { t.Fatal("unexpected request"); return 200, "[]" })
	c := context.Background()
	for _, fn := range []func() error{func() error {
		_, e := a.updateRows(c, &UpdateRowsIn{Table: "items", Values: map[string]any{"x": 1}})
		return e
	}, func() error { _, e := a.deleteRows(c, &DeleteRowsIn{Table: "items"}); return e }, func() error { _, e := a.selectRows(c, &SelectRowsIn{Table: "../rpc/x"}); return e }, func() error {
		_, e := a.selectRows(c, &SelectRowsIn{Table: "items", Filters: []RowFilter{{Column: "limit", Operator: "eq", Value: "1"}}})
		return e
	}, func() error { _, e := a.insertRows(c, &InsertRowsIn{Table: "items", Rows: []any{1}}); return e }, func() error { _, e := a.signedURL(c, &CreateSignedURLIn{Bucket: "files", Path: "../a"}); return e }} {
		if fn() == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
