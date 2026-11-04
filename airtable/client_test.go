package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecordsPaginationAndQuery(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/app123/tbl123" || q.Get("offset") != "itr/rec+=" || q.Get("pageSize") != "100" || q.Get("filterByFormula") != "{城市}='上海'" || q.Get("fields[]") != "城市" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("wrong wire request: %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"records":[{"id":"rec123","createdTime":"2022-09-08T16:34:34.000Z","fields":{"城市":"上海"}}],"offset":"next"}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, token: "secret", http: s.Client()}
	out, err := c.records(context.Background(), &AirtableRecordsIn{BaseID: "app123", TableID: "tbl123", Offset: "itr/rec+=", FilterFormula: "{城市}='上海'", Fields: []string{"城市"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Records) != 1 || out.Offset != "next" {
		t.Fatalf("bad output %+v", out)
	}
}
func TestCreateRecordBytes(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		fs, ok := b["fields"].(map[string]any)
		if !ok || r.Method != "POST" || r.URL.Path != "/app123/tbl123" || fs["Count"] != float64(0) || b["typecast"] != true {
			t.Errorf("wrong request %+v", b)
		}
		io.WriteString(w, `{"id":"rec123","createdTime":"2022-09-08T16:34:34.000Z","fields":{"Count":0}}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, token: "secret", http: s.Client()}
	out, err := c.create(context.Background(), &AirtableCreateIn{BaseID: "app123", TableID: "tbl123", Fields: map[string]any{"Count": 0}, Typecast: true})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Record.ID != "rec123" {
		t.Fatal("missing create receipt")
	}
}
