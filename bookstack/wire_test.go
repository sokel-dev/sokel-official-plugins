package main

import (
	"github.com/sokel-dev/sokel-official-plugins/bookstack/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestREDListOffset(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/api/books" || r.URL.Query().Get("sort") != "+id" || r.Header.Get("Authorization") != "Token tokenid:secret" {
			t.Fatal("incorrect request")
		}
		return response("application/json", `{"data":[{"id":9007199254740993,"name":"Guide","slug":"guide"}],"total":2}`), nil
	})
	o, e := c.listEntities(ctxFor(""), &ListEntitiesIn{Kind: "books"})
	if e != nil {
		t.Fatal(e)
	}
	if o.NextOffset != 1 || o.Entities[0].ID != "9007199254740993" {
		t.Fatal(o)
	}
}
func TestREDUpdateZeroAndEmptyTags(t *testing.T) {
	z := 0
	tags := []schema.Tag{}
	c := testClient(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || r.GetBody != nil || !strings.Contains(string(b), `"priority":0`) || !strings.Contains(string(b), `"tags":[]`) {
			t.Fatalf("wire: %s", b)
		}
		return response("application/json", `{"id":1,"book_id":2,"name":"Page","html":"","revision_count":2,"tags":[]}`), nil
	})
	if _, e := c.updatePage(ctxFor(""), &UpdatePageIn{PageID: "1", Changes: schema.PageChanges{Priority: &z, Tags: &tags}}); e != nil {
		t.Fatal(e)
	}
}
