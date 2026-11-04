package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/miniflux/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func TestCursorAndLargeIDs(t *testing.T) {
	c := newClient(Cred{APIKey: "private"})
	c.base = "http://rss.local/prefix"
	after := "9007199254740992"
	f := false
	n := 1
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/prefix/v1/entries" || r.Header.Get("X-Auth-Token") != "private" || r.URL.Query().Get("after_entry_id") != after || r.URL.Query().Get("starred") != "false" || r.URL.Query().Get("order") != "id" || r.URL.Query().Get("direction") != "asc" {
			t.Fatalf("bad request %v", r)
		}
		return fixture(200, `{"total":1,"entries":[{"id":9007199254740993,"feed_id":5,"title":"x","status":"unread","feed":{"password":"HIDDEN"}}]}`), nil
	})}
	o, e := c.listEntries(context.Background(), &ListEntriesIn{Options: schema.EntryOptions{Limit: &n, AfterID: &after, Starred: &f}})
	if e != nil || o.NextAfterID != "9007199254740993" || !o.MayHaveMore {
		t.Fatalf("%+v %v", o, e)
	}
}
func TestExplicitFalseAndRefresh(t *testing.T) {
	c := newClient(Cred{APIKey: "key"})
	c.base = "http://rss.local"
	calls := 0
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if calls == 1 && string(b) != `{"entry_ids":[1],"starred":false}` {
			t.Fatalf("payload %s", b)
		}
		if r.GetBody != nil {
			t.Fatal("mutation can replay")
		}
		return fixture(204, ""), nil
	})}
	f := false
	if _, e := c.updateEntries(context.Background(), &UpdateEntriesIn{EntryIDs: []string{"1"}, Changes: schema.EntryChanges{Starred: &f}}); e != nil {
		t.Fatal(e)
	}
	if _, e := c.refreshFeed(context.Background(), &RefreshFeedIn{FeedID: "1"}); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestRejectInvalidAndRedact(t *testing.T) {
	c := newClient(Cred{APIKey: "key"})
	c.base = "http://rss.local"
	calls := 0
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return fixture(401, `{"error":"SECRET upstream"}`), nil
	})}
	if _, e := c.getEntry(context.Background(), &GetEntryIn{EntryID: "1e4"}); e == nil || calls != 0 {
		t.Fatal("invalid ID sent")
	}
	if _, e := c.updateEntries(context.Background(), &UpdateEntriesIn{EntryIDs: []string{"1"}}); e == nil || calls != 0 {
		t.Fatal("empty patch sent")
	}
	if _, e := c.getCurrentUser(context.Background(), &GetCurrentUserIn{}); e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatalf("error %v", e)
	}
}
func TestTransportBoundaries(t *testing.T) {
	for _, status := range []int{302, 401, 429, 500} {
		c := newClient(Cred{APIKey: "secret"})
		c.base = "http://reader.local"
		calls := 0
		c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			r := fixture(status, "SECRET")
			r.Header.Set("Location", "https://evil.local")
			return r, nil
		})}
		if _, e := c.getCurrentUser(context.Background(), &GetCurrentUserIn{}); e == nil || strings.Contains(e.Error(), "SECRET") || calls != 1 {
			t.Fatalf("status %d: %v calls%d", status, e, calls)
		}
	}
	c := newClient(Cred{APIKey: "secret"})
	c.base = "http://reader.local"
	c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return fixture(200, strings.Repeat(" ", maxResponse+1)), nil
	})}
	if _, e := c.getCurrentUser(context.Background(), &GetCurrentUserIn{}); e == nil {
		t.Fatal("oversized response accepted")
	}
}
