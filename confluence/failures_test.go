package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPaginationLinksCannotChangeOriginOrResource(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("link must not be fetched") })
	for _, next := range []string{"https://evil.invalid/wiki/api/v2/pages?cursor=x", "//evil.invalid/wiki/api/v2/pages?cursor=x", "https://test-secret@demo.atlassian.net/wiki/api/v2/pages?cursor=x", "/wiki/api/v2/spaces?cursor=x", "/wiki/api/v2/pages?limit=2", "/wiki/api/v2/pages?cursor=x&cursor=y", "/wiki/api/v2/pages?cursor=x#fragment"} {
		if _, e := a.cursor(v2+"/pages", nil, next); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("%s %v", next, e)
		}
	}
	if c, e := a.cursor(v2+"/pages", nil, "https://demo.atlassian.net/wiki/api/v2/pages?cursor=x%2By"); e != nil || c != "x+y" {
		t.Fatal(c, e)
	}
}
func TestConcurrentUpdateAndBodyValidation(t *testing.T) {
	ctx := context.Background()
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "GET" {
			reply(w, `{"id":"20","status":"current","version":{"number":7}}`)
			return
		}
		w.WriteHeader(409)
		reply(w, `{"message":"test-secret"}`)
	})
	if _, e := a.updatePage(ctx, &UpdatePageIn{PageID: "20", ExpectedVersion: 7, Title: "New", Body: "<p>x</p>"}); e == nil || !strings.Contains(e.Error(), "conflict") || strings.Contains(e.Error(), "test-secret") || calls != 2 {
		t.Fatalf("%v %d", e, calls)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid request reached transport") })
	for _, in := range []UpdatePageIn{{PageID: "20", ExpectedVersion: 0, Title: "x"}, {PageID: "../1", ExpectedVersion: 1, Title: "x"}, {PageID: "20", ExpectedVersion: 1, Title: "x", Representation: "view"}, {PageID: "20", ExpectedVersion: 1, Title: "x", Representation: "atlas_doc_format", Body: `{"type":"doc","version":2}`}} {
		if _, e := a.updatePage(ctx, &in); e == nil {
			t.Fatal("invalid update accepted")
		}
	}
	if _, e := pageBody("New", `{"type":"doc","version":1,"content":[]}`, "atlas_doc_format"); e != nil {
		t.Fatal(e)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("draft modified")
		}
		reply(w, `{"id":"20","status":"draft","version":{"number":7}}`)
	})
	if _, e := a.updatePage(ctx, &UpdatePageIn{PageID: "20", ExpectedVersion: 7, Title: "x", Body: "<p>x</p>"}); e == nil {
		t.Fatal("draft update")
	}
}

func TestMissingCollectionIsNotEmptySuccess(t *testing.T) {
	ctx := context.Background()
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if _, e := a.spaces(ctx, &ListSpacesIn{}); e == nil {
		t.Fatal("missing collection accepted")
	}
	if _, e := a.pages(ctx, &ListPagesIn{}); e == nil {
		t.Fatal("missing collection accepted")
	}
}

func TestMissingBodyCannotErasePage(t *testing.T) {
	if _, e := pageBody("Title", "", "storage"); e == nil {
		t.Fatal("empty body accepted")
	}
	if _, e := pageBody("Title", "<p></p>", "storage"); e != nil {
		t.Fatal(e)
	}
}
