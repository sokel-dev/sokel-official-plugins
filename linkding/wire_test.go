package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

const bookmarkFixture = `{"id":9007199254740993,"url":"https://example.org/entry","title":"Example","description":"Text","notes":"","is_archived":false,"unread":false,"shared":false,"tag_names":[],"date_added":"2026-11-04T00:00:00Z","date_modified":"2026-11-04T00:00:00Z"}`

func TestDocumentedWireOperations(t *testing.T) {
	cases := []struct {
		op, path, method, body string
		status                 int
		in                     any
	}{
		{"health_check", "/api/tags/", "GET", `{"count":0,"next":null,"results":[]}`, 200, map[string]any{}},
		{"list_bookmarks", "/api/bookmarks/", "GET", `{"count":1,"next":null,"results":[` + bookmarkFixture + `]}`, 200, map[string]any{}},
		{"get_bookmark", "/api/bookmarks/9007199254740993/", "GET", bookmarkFixture, 200, map[string]any{"bookmark_id": "9007199254740993"}},
		{"save_bookmark", "/api/bookmarks/", "POST", bookmarkFixture, 201, map[string]any{"url": "https://example.org/entry", "fields": map[string]any{"unread": false, "tag_names": []string{}}}},
		{"update_bookmark", "/api/bookmarks/9007199254740993/", "PATCH", bookmarkFixture, 200, map[string]any{"bookmark_id": "9007199254740993", "changes": map[string]any{"unread": false, "tag_names": []string{}}}},
		{"archive_bookmark", "/api/bookmarks/9007199254740993/archive/", "POST", "", 204, map[string]any{"bookmark_id": "9007199254740993"}},
		{"unarchive_bookmark", "/api/bookmarks/9007199254740993/unarchive/", "POST", "", 204, map[string]any{"bookmark_id": "9007199254740993"}},
		{"delete_bookmark", "/api/bookmarks/9007199254740993/", "DELETE", "", 204, map[string]any{"bookmark_id": "9007199254740993"}},
		{"list_tags", "/api/tags/", "GET", `{"count":1,"next":null,"results":[{"id":1,"name":"example","date_added":"2026-11-04T00:00:00Z"}]}`, 200, map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			calls := 0
			s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/prefix"+tc.path || r.Method != tc.method || r.Header.Get("Authorization") != "Token fixture" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				if tc.op == "save_bookmark" {
					if r.URL.Query().Get("disable_scraping") != "true" || r.URL.Query().Get("disable_html_snapshot") != "true" {
						t.Error("background metadata not disabled")
					}
				}
				if tc.op == "save_bookmark" || tc.op == "update_bookmark" {
					b, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(b), `"unread":false`) || !strings.Contains(string(b), `"tag_names":[]`) {
						t.Errorf("omission bug %s", b)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer s.Close()
			t.Setenv("SOKEL_LINKDING_URL", s.URL+"/prefix")
			v, e := dispatch(t, tc.op, tc.in, map[string]string{"api_token": "fixture"})
			if e != nil {
				t.Fatal(e)
			}
			if calls != 1 {
				t.Fatalf("calls %d", calls)
			}
			if len(v) == 0 {
				t.Fatal("empty output")
			}
		})
	}
}
