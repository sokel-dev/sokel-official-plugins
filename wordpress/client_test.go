package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const examplePost = `{"id":10,"status":"draft","slug":"sample","link":"https://example.com/?p=10","title":{"rendered":"Title"},"content":{"rendered":"<p>Body</p>"},"excerpt":{"rendered":""},"categories":[1],"modified_gmt":"2026-01-01T00:00:00"}`

func TestPostPageAndBasicAuth(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "editor" || p != "app-password" || r.URL.Path != "/wp-json/wp/v2/posts" || r.URL.Query().Get("page") != "2" {
			t.Error("incorrect read request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-WP-Total", "30")
		w.Header().Set("X-WP-TotalPages", "3")
		io.WriteString(w, "["+examplePost+"]")
	}))
	defer s.Close()
	c := &client{base: s.URL, cred: Cred{Username: "editor", ApplicationPassword: "app-password"}, http: s.Client()}
	out, e := c.listPosts(context.Background(), &ListPostsIn{Page: "2", Status: "draft"})
	if e != nil {
		t.Fatal(e)
	}
	if out.NextPage != "3" || out.Posts[0].TitleHTML != "Title" {
		t.Fatalf("bad result %+v", out)
	}
}
func TestCreateAlwaysDraft(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil || r.Method != "POST" || b["status"] != "draft" || b["content"] != "<p>你好</p>" {
			t.Error("incorrect draft request")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, examplePost)
	}))
	defer s.Close()
	c := &client{base: s.URL, cred: Cred{Username: "editor", ApplicationPassword: "app-password"}, http: s.Client()}
	if _, e := c.createDraft(context.Background(), &CreateDraftIn{Title: "Title", Content: "<p>你好</p>"}); e != nil {
		t.Fatal(e)
	}
}
