package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestContinuationValidation(t *testing.T) {
	cases := []struct {
		next  string
		count string
		ok    bool
	}{{`"http://fixture.invalid/prefix/api/bookmarks/?limit=1&offset=1"`, "2", true}, {`"https://evil.invalid/api/bookmarks/?limit=1&offset=1"`, "2", false}, {`"http://fixture.invalid/prefix/api/bookmarks/?limit=1&offset=0"`, "2", false}, {`"http://fixture.invalid/prefix/api/bookmarks/?limit=1&offset=1&q=changed"`, "2", false}, {`null`, "2", false}, {`null`, "1", true}}
	for _, tc := range cases {
		s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"count":`+tc.count+`,"next":`+tc.next+`,"results":[`+bookmarkFixture+`]}`)
		}))
		t.Setenv("SOKEL_LINKDING_URL", s.URL+"/prefix")
		_, e := dispatch(t, "list_bookmarks", map[string]any{"options": map[string]any{"limit": 1}}, map[string]string{"api_token": "fixture"})
		s.Close()
		if (e == nil) != tc.ok {
			t.Fatalf("next %s: %v", tc.next, e)
		}
	}
}
func TestInvalidInputBeforeNetwork(t *testing.T) {
	calls := 0
	s := newFixtureServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer s.Close()
	t.Setenv("SOKEL_LINKDING_URL", s.URL)
	for _, tc := range []struct {
		op string
		in any
	}{{"get_bookmark", map[string]any{"bookmark_id": "1e2"}}, {"update_bookmark", map[string]any{"bookmark_id": "1", "changes": map[string]any{}}}, {"save_bookmark", map[string]any{"url": "file:///tmp/secret"}}, {"save_bookmark", map[string]any{"url": "https://u:p@example.org/"}}, {"list_tags", map[string]any{"options": map[string]any{"limit": 0}}}, {"list_bookmarks", map[string]any{"options": map[string]any{"offset": -1}}}, {"list_bookmarks", map[string]any{"options": map[string]any{"modified_since": "yesterday"}}}} {
		if _, e := dispatch(t, tc.op, tc.in, map[string]string{"api_token": "fixture"}); e == nil {
			t.Fatalf("accepted %s", tc.op)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestErrorsRedirectsAndReplay(t *testing.T) {
	for _, status := range []int{302, 401, 429, 500} {
		calls := 0
		s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Location", "https://evil.invalid")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"SECRET"}`)
		}))
		t.Setenv("SOKEL_LINKDING_URL", s.URL)
		_, e := dispatch(t, "get_bookmark", map[string]any{"bookmark_id": "1"}, map[string]string{"api_token": "fixture"})
		s.Close()
		if e == nil || strings.Contains(e.Error(), "SECRET") || calls != 1 {
			t.Fatalf("status%d %v calls%d", status, e, calls)
		}
	}
	c := newClient(Cred{APIToken: "fixture"})
	c.base = "http://fixture.invalid"
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.GetBody != nil {
			t.Fatal("mutation replay enabled")
		}
		return &http.Response{StatusCode: 204, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if _, e := c.archiveBookmark(context.Background(), &ArchiveBookmarkIn{BookmarkID: "1"}); e != nil {
		t.Fatal(e)
	}
}
func TestMalformedBookmarkAndResponseLimit(t *testing.T) {
	for _, body := range []string{`null`, `{}`, strings.Replace(bookmarkFixture, `"id":9007199254740993`, `"id":0`, 1), strings.Replace(bookmarkFixture, `"unread":false`, `"unread":null`, 1), strings.Repeat(" ", maxResponse+1)} {
		s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_LINKDING_URL", s.URL)
		_, e := dispatch(t, "get_bookmark", map[string]any{"bookmark_id": "9007199254740993"}, map[string]string{"api_token": "fixture"})
		s.Close()
		if e == nil {
			t.Fatal("malformed response accepted")
		}
	}
}
