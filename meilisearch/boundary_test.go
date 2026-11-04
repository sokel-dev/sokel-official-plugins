package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/meilisearch/schema"
	"net/http"
	"strings"
	"testing"
)

const indexFixture = `{"uid":"books","primaryKey":"id","createdAt":"2024-08-08T17:05:55Z","updatedAt":"2024-08-08T17:05:55Z"}`

func TestOffsetPagination(t *testing.T) {
	c := fresh()
	withReply(c, 200, `{"results":[`+indexFixture+`],"limit":1,"offset":0,"total":2}`)
	o, e := c.list_indexes(context.Background(), &ListIndexesIn{Page: schema.Page{Limit: ptr(1)}})
	if e != nil || !o.HasMore || o.NextOffset != 1 {
		t.Fatal(o, e)
	}
	for _, s := range []string{`{"results":[],"limit":1,"offset":0,"total":2}`, `{"results":[],"limit":1,"offset":1,"total":1}`, `{"results":[],"limit":2,"offset":0,"total":0}`, `{"results":[` + indexFixture + `,` + indexFixture + `],"limit":1,"offset":0,"total":2}`} {
		c := fresh()
		withReply(c, 200, s)
		if _, e := c.list_indexes(context.Background(), &ListIndexesIn{Page: schema.Page{Limit: ptr(1)}}); e == nil {
			t.Fatal(s)
		}
	}
	c = fresh()
	withReply(c, 200, `{"hits":[{}],"offset":0,"limit":1,"estimatedTotalHits":1,"processingTimeMs":0}`)
	search, e := c.search(context.Background(), &SearchIn{IndexUID: "books", Options: schema.SearchOptions{Limit: ptr(1), Attributes: []string{}}})
	if e != nil || !search.MayHaveMore || search.NextOffset != 1 || search.Hits[0].JSON != "{}" {
		t.Fatal(search, e)
	}
	c = fresh()
	withReply(c, 200, `{"hits":[],"offset":1,"limit":1,"estimatedTotalHits":1,"processingTimeMs":0}`)
	search, e = c.search(context.Background(), &SearchIn{IndexUID: "books", Options: schema.SearchOptions{Limit: ptr(1), Offset: ptr(1)}})
	if e != nil || search.MayHaveMore {
		t.Fatal(search, e)
	}
}
func TestTaskStatesAndReceipt(t *testing.T) {
	date := `"2024-08-08T17:05:55Z"`
	for _, tc := range []struct {
		status, finish, err string
		ok, done, success   bool
	}{{"enqueued", "null", "null", true, false, false}, {"processing", "null", "null", true, false, false}, {"succeeded", date, "null", true, true, true}, {"failed", date, `{"code":"missing_document_id","message":"secret-test"}`, true, true, false}, {"canceled", date, "null", true, true, false}, {"weird", "null", "null", false, false, false}, {"succeeded", "null", "null", false, false, false}, {"succeeded", date, `{"code":"error"}`, false, false, false}, {"failed", date, "null", false, false, false}} {
		c := fresh()
		withReply(c, 200, `{"uid":0,"indexUid":null,"status":"`+tc.status+`","type":"documentAdditionOrUpdate","enqueuedAt":`+date+`,"startedAt":null,"finishedAt":`+tc.finish+`,"error":`+tc.err+`}`)
		v, e := c.get_task(context.Background(), &GetTaskIn{TaskUID: "0"})
		if (e == nil) != tc.ok {
			t.Fatal(tc, e)
		}
		if tc.ok && (v.Finished != tc.done || v.Succeeded != tc.success) {
			t.Fatal(v)
		}
		if e != nil && strings.Contains(e.Error(), "secret-test") {
			t.Fatal(e)
		}
	}
	for _, s := range []string{`{"taskUid":0,"indexUid":null,"status":"enqueued","type":"indexCreation","enqueuedAt":"2024-08-08T17:05:55Z"}`, `{"taskUid":9007199254740993,"indexUid":"books","status":"enqueued","type":"indexCreation","enqueuedAt":"2024-08-08T17:05:55Z"}`} {
		if _, e := taskReceipt([]byte(s), "books", "indexCreation"); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"-1", "01", "1.0", "1e3", "18446744073709551616", "../1"} {
		if numericID(s) == nil {
			t.Fatal(s)
		}
	}
}
func TestSearchOptionsAndDocumentPrecision(t *testing.T) {
	c := fresh()
	wire(t, c, "POST", "/indexes/books/search", "", `{"attributesToRetrieve":[],"filter":"available = false","limit":1,"offset":0,"q":"","sort":["year:desc"]}`, `{"hits":[{"id":9007199254740993,"available":false}],"offset":0,"limit":1,"estimatedTotalHits":1,"processingTimeMs":0}`, 200)
	o, e := c.search(context.Background(), bind[SearchIn](t, `{"index_uid":"books","options":{"limit":1,"offset":0,"filter":"available = false","sort":["year:desc"],"attributes_to_retrieve":[]}}`))
	if e != nil || !strings.Contains(o.Hits[0].JSON, "9007199254740993") || !strings.Contains(o.Hits[0].JSON, "false") {
		t.Fatal(o, e)
	}
	c = fresh()
	withReply(c, 200, `{"id":9007199254740993,"code":"a user field","error":false}`)
	d, e := c.get_document(context.Background(), &GetDocumentIn{IndexUID: "books", DocumentID: "9007199254740993"})
	if e != nil || !strings.Contains(d.DocumentJSON, "9007199254740993") {
		t.Fatal(d, e)
	}
}
func TestInvalidBusinessInputsNoRequest(t *testing.T) {
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	for _, id := range []string{"../x", "a/b", "https://evil", "a?b", "", "a%2fb"} {
		if _, e := indexPath(id); e == nil {
			t.Fatal(id)
		}
	}
	for _, opts := range []schema.SearchOptions{{Limit: ptr(0)}, {Offset: ptr(-1)}, {Filter: ptr("\x00")}, {Sort: []string{""}}} {
		if _, e := c.search(context.Background(), &SearchIn{IndexUID: "books", Options: opts}); e == nil {
			t.Fatal(opts)
		}
	}
	for _, body := range []string{"{}", "null", "[]", "[null]", "[{}]", "[{\"id\":1}] {}", strings.Repeat("a", maxRequest+1)} {
		if _, e := c.add_documents(context.Background(), &AddDocumentsIn{IndexUID: "books", DocumentsJSON: body}); e == nil {
			t.Fatal(body)
		}
	}
	if _, e := c.create_index(context.Background(), &CreateIndexIn{IndexUID: "books", PrimaryKey: " "}); e == nil {
		t.Fatal("blank primary key")
	}
}
