package main

import (
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/bookstack/schema"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, n string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/" + n + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestOfficialDocumentationExamples(t *testing.T) {
	for _, tc := range []struct{ op, input, file string }{{"get_shelf", `{"shelf_id":"14"}`, "shelves-read"}, {"get_book", `{"book_id":"16"}`, "books-read"}, {"get_chapter", `{"chapter_id":"1"}`, "chapters-read"}, {"get_page", `{"page_id":"306"}`, "pages-read"}, {"search", `{"query":"test"}`, "search-all"}, {"list_entities", `{"kind":"books"}`, "books-list"}, {"create_page", `{"parent_type":"book","parent_id":"1","name":"My API Page","format":"html","content":"<p>my new API page</p>"}`, "pages-create"}, {"update_page", `{"page_id":"361","changes":{"name":"My updated API Page"}}`, "pages-update"}} {
		t.Run(tc.op, func(t *testing.T) {
			t.Setenv("SOKEL_BOOKSTACK_URL", "http://service.internal:3000/prefix")
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Token tokenid:secret" {
					t.Fatal("auth")
				}
				return response("application/json", fixture(t, tc.file)), nil
			})
			vars, e := dispatch(t, tc.op, tc.input, ctxFor(""))
			if e != nil {
				t.Fatal(e)
			}
			b, _ := json.Marshal(vars)
			if strings.Contains(string(b), "secret") || len(vars) == 0 {
				t.Fatal("empty or credential-bearing output")
			}
			if tc.op == "get_book" {
				if !strings.Contains(string(b), "Building Bridges") || !strings.Contains(string(b), "Cool Animals") {
					t.Fatal("nested page lost")
				}
			}
		})
	}
}
func TestGeneratedZeroEmptyAndMarkdown(t *testing.T) {
	t.Setenv("SOKEL_BOOKSTACK_URL", "http://service.internal:3000/prefix")
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if r.GetBody != nil {
			t.Fatal("write replay")
		}
		for _, key := range []string{`"priority":0`, `"tags":[]`} {
			if !strings.Contains(string(b), key) {
				t.Fatalf("missing %s: %s", key, b)
			}
		}
		if r.Method == "POST" {
			for _, key := range []string{`"chapter_id":9007199254740993`, `"markdown":"# Page"`, `"changelog":"initial"`} {
				if !strings.Contains(string(b), key) {
					t.Fatal(string(b))
				}
			}
		} else {
			if !strings.Contains(string(b), `"html":""`) {
				t.Fatal(string(b))
			}
		}
		return response("application/json", `{"id":9007199254740993,"book_id":1,"chapter_id":2,"name":"Page","html":"","revision_count":0,"tags":[]}`), nil
	})
	for _, tc := range []struct{ op, in string }{{"create_page", `{"parent_type":"chapter","parent_id":"9007199254740993","name":"Page","format":"markdown","content":"# Page","options":{"priority":0,"tags":[],"changelog":"initial"}}`}, {"update_page", `{"page_id":"9007199254740993","changes":{"html":"","priority":0,"tags":[]}}`}} {
		v, e := dispatch(t, tc.op, tc.in, ctxFor(""))
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "9007199254740993") {
			t.Fatal(string(b))
		}
	}
}
func TestPagingAndInvalidResponses(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("offset") != "" {
			t.Fatal("search uses page, not offset")
		}
		return response("application/json", `{"data":[{"id":1,"name":"Page","type":"page"}],"total":21}`), nil
	})
	o, e := c.search(ctxFor(""), &SearchIn{Query: "query"})
	if e != nil || o.NextPage != 2 {
		t.Fatal(o, e)
	}
	for _, raw := range []string{`null`, `{}`, `{"data":null,"total":0}`, `{"data":[],"total":1}`, `{"data":[{"id":0,"name":"Bad"}],"total":1}`, `{"data":[],"total":-1}`} {
		c := testClient(func(*http.Request) (*http.Response, error) { return response("application/json", raw), nil })
		if _, e := c.listEntities(ctxFor(""), &ListEntitiesIn{Kind: "books"}); e == nil {
			t.Fatal("accepted invalid list")
		}
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		return response("application/json", `{"data":[],"total":0}`), nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || !o.OK {
		t.Fatal(o, e)
	}
	c = testClient(func(*http.Request) (*http.Response, error) {
		s := response("application/json", `{"error":{"message":"secret"}}`)
		s.StatusCode = 403
		return s, nil
	})
	if o, e := c.healthCheck(ctxFor(""), &HealthCheckIn{}); e != nil || o.OK || strings.Contains(o.Message, "secret") {
		t.Fatal(o, e)
	}
	for _, kind := range []string{"shelves", "books", "chapters", "pages"} {
		c := testClient(func(*http.Request) (*http.Response, error) {
			return response("application/json", `{"data":[],"total":0}`), nil
		})
		if _, e := c.listEntities(ctxFor(""), &ListEntitiesIn{Kind: kind}); e != nil {
			t.Fatal(e)
		}
	}
}
func TestInputBoundaries(t *testing.T) {
	c := testClient(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input made HTTP request")
		return nil, nil
	})
	z := 0
	bad := 101
	if _, e := c.listEntities(ctxFor(""), &ListEntitiesIn{Kind: "../users"}); e == nil {
		t.Fatal("kind")
	}
	if _, e := c.listEntities(ctxFor(""), &ListEntitiesIn{Kind: "books", Pagination: schema.Pagination{Count: &z}}); e == nil {
		t.Fatal("count")
	}
	if _, e := c.search(ctxFor(""), &SearchIn{Query: "q", Pagination: schema.SearchPagination{Count: &bad}}); e == nil {
		t.Fatal("search count")
	}
	for _, in := range []*CreatePageIn{{ParentType: "user"}, {ParentType: "book", ParentID: "1", Name: "Name", Format: "html"}, {ParentType: "book", ParentID: "01"}} {
		if _, e := c.createPage(ctxFor(""), in); e == nil {
			t.Fatal("create validation")
		}
	}
	text := "text"
	if _, e := c.updatePage(ctxFor(""), &UpdatePageIn{PageID: "1", Changes: schema.PageChanges{HTML: &text, Markdown: &text}}); e == nil {
		t.Fatal("ambiguous format")
	}
	if _, e := c.updatePage(ctxFor(""), &UpdatePageIn{PageID: "1"}); e == nil {
		t.Fatal("empty changes")
	}
	if _, e := c.getPage(ctxFor(""), &GetPageIn{PageID: "../1"}); e == nil {
		t.Fatal("path")
	}
	tags := []schema.Tag{{Name: "", Value: "x"}}
	if _, e := c.updatePage(ctxFor(""), &UpdatePageIn{PageID: "1", Changes: schema.PageChanges{Tags: &tags}}); e == nil {
		t.Fatal("tag")
	}
}
