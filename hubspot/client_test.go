package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListCursorAndProperties(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/crm/v3/objects/contacts" || r.URL.Query().Get("after") != "opaque+/=" || r.URL.Query().Get("properties") != "email,firstname" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("incorrect wire request")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"id":"512","properties":{"email":"x@example.com"},"createdAt":"2019-10-30T03:30:17.883Z","updatedAt":"2019-10-30T03:30:17.883Z","archived":false}],"paging":{"next":{"after":"next+/="}}}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, cred: Cred{Token: "test-secret"}, http: s.Client()}
	out, e := c.listRecords(context.Background(), &ListRecordsIn{ObjectType: "contacts", After: "opaque+/=", Properties: []string{"email", "firstname"}})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Records) != 1 || out.Records[0].CreatedAt == "" || out.NextAfter != "next+/=" {
		t.Fatalf("bad result %+v", out)
	}
}
func TestCreatePropertyBytes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]map[string]string
		if json.NewDecoder(r.Body).Decode(&v) != nil || r.Method != "POST" || v["properties"]["firstname"] != "你好" || v["properties"]["email"] != "" {
			t.Error("incorrect create body")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"512","properties":{},"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, cred: Cred{Token: "test-secret"}, http: s.Client()}
	if _, e := c.createRecord(context.Background(), &CreateRecordIn{ObjectType: "contacts", Properties: map[string]any{"firstname": "你好", "email": ""}}); e != nil {
		t.Fatal(e)
	}
}
