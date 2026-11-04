package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalServiceAcceptance(t *testing.T) {
	base := os.Getenv("SOKEL_TEST_MINIFLUX_URL")
	if base == "" {
		t.Skip("set SOKEL_TEST_MINIFLUX_URL, TOKEN and FEED_URL for isolated local acceptance")
	}
	u, e := url.Parse(base)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("local acceptance requires a loopback service")
	}
	key := os.Getenv("SOKEL_TEST_MINIFLUX_TOKEN")
	feed := os.Getenv("SOKEL_TEST_MINIFLUX_FEED_URL")
	if key == "" || feed == "" {
		t.Fatal("local token and synthetic feed URL required")
	}
	c := newClient(Cred{APIKey: key})
	c.base = base
	call := func(op string, in any) map[string]any { return invoke(t, c, op, in) }
	call("get_current_user", map[string]any{})
	cat := call("create_category", map[string]any{"title": fmt.Sprintf("sokel-acceptance-%d", time.Now().UnixNano())})
	b, _ := json.Marshal(cat["category"])
	var cv struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &cv)
	call("list_categories", map[string]any{})
	sub := call("create_feed", map[string]any{"feed_url": feed, "category_id": cv.ID})
	fid := sub["feed_id"].(string)
	call("list_feeds", map[string]any{})
	call("refresh_feed", map[string]any{"feed_id": fid})
	first := call("list_entries", map[string]any{"options": map[string]any{"limit": 2, "category_id": cv.ID}})
	b, _ = json.Marshal(first["entries"])
	var es []struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &es)
	if len(es) != 2 || first["may_have_more"] != true {
		t.Fatal("expected first page of synthetic entries")
	}
	call("update_entries", map[string]any{"entry_ids": []string{es[0].ID}, "changes": map[string]any{"status": "read", "starred": false}})
	got := call("get_entry", map[string]any{"entry_id": es[0].ID})
	b, _ = json.Marshal(got)
	if !strings.Contains(string(b), `"status":"read"`) || !strings.Contains(string(b), `"starred":false`) {
		t.Fatal("reading state not applied")
	}
	second := call("list_entries", map[string]any{"options": map[string]any{"limit": 2, "category_id": cv.ID, "after_id": first["next_after_id"]}})
	b, _ = json.Marshal(second["entries"])
	json.Unmarshal(b, &es)
	if len(es) != 1 || second["may_have_more"] != false {
		t.Fatal("cursor did not return final synthetic entry")
	}
	t.Log("all 9 generated operations passed with synthetic local feed, cursor pagination and explicit false")
}
