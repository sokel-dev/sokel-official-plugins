package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestLocalServiceAcceptance(t *testing.T) {
	base := os.Getenv("SOKEL_TEST_LINKDING_URL")
	if base == "" {
		t.Skip("set SOKEL_TEST_LINKDING_URL/TOKEN/BOOKMARK_URL/WRITE for isolated local acceptance")
	}
	u, e := url.Parse(base)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || os.Getenv("SOKEL_TEST_LINKDING_WRITE") != "1" {
		t.Fatal("local acceptance requires a loopback service and WRITE=1")
	}
	token := os.Getenv("SOKEL_TEST_LINKDING_TOKEN")
	bookmarkURL := os.Getenv("SOKEL_TEST_LINKDING_BOOKMARK_URL")
	if token == "" || bookmarkURL == "" {
		t.Fatal("synthetic token and URL required")
	}
	t.Setenv("SOKEL_LINKDING_URL", base)
	captureLocalHTTP(t, "linkding")
	creds := map[string]string{"api_token": token}
	call := func(op string, in any) map[string]any {
		v, e := dispatch(t, op, in, creds)
		if e != nil {
			t.Fatalf("%s: %v", op, e)
		}
		return v
	}
	getBookmark := func(v map[string]any) map[string]any {
		b, _ := json.Marshal(v["bookmark"])
		var out map[string]any
		if json.Unmarshal(b, &out) != nil {
			t.Fatal("invalid bookmark output")
		}
		return out
	}
	getPage := func(v map[string]any) map[string]any {
		b, _ := json.Marshal(v["page"])
		var out map[string]any
		json.Unmarshal(b, &out)
		return out
	}
	marker := fmt.Sprintf("sokelwave8%d", time.Now().UnixNano())
	health := call("health_check", map[string]any{})
	if health["ok"] != true {
		t.Fatalf("health %v", health)
	}
	urls := []string{}
	ids := []string{}
	for i := 0; i < 2; i++ {
		burl, _ := url.Parse(bookmarkURL)
		q := burl.Query()
		q.Set("fixture", marker)
		q.Set("entry", fmt.Sprint(i))
		burl.RawQuery = q.Encode()
		urls = append(urls, burl.String())
		v := getBookmark(call("save_bookmark", map[string]any{"url": urls[i], "fields": map[string]any{"title": marker + fmt.Sprint(i), "tag_names": []string{marker}, "unread": true, "shared": false}}))
		ids = append(ids, v["id"].(string))
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, e := dispatch(t, "delete_bookmark", map[string]any{"bookmark_id": id}, creds)
			if e != nil {
				t.Logf("cleanup bookmark: %v", e)
			}
		}
	})
	upsert := getBookmark(call("save_bookmark", map[string]any{"url": urls[0], "fields": map[string]any{"title": marker + "changed", "unread": false, "tag_names": []string{marker}}}))
	if upsert["id"] != ids[0] {
		t.Fatal("existing URL did not update same bookmark")
	}
	changed := getBookmark(call("update_bookmark", map[string]any{"bookmark_id": ids[0], "changes": map[string]any{"unread": false, "tag_names": []string{}}}))
	if changed["unread"] != false || len(changed["tag_names"].([]any)) != 0 {
		t.Fatal("false or empty tags lost")
	}
	call("get_bookmark", map[string]any{"bookmark_id": ids[0]})
	first := call("list_bookmarks", map[string]any{"options": map[string]any{"search": marker, "limit": 1}})
	page := getPage(first)
	if page["has_more"] != true {
		t.Fatal("first page must continue")
	}
	second := call("list_bookmarks", map[string]any{"options": map[string]any{"search": marker, "limit": 1, "offset": page["next_offset"]}})
	if getPage(second)["has_more"] != false {
		t.Fatal("second page should be final")
	}
	call("archive_bookmark", map[string]any{"bookmark_id": ids[0]})
	archived := getBookmark(call("get_bookmark", map[string]any{"bookmark_id": ids[0]}))
	if archived["is_archived"] != true {
		t.Fatal("archive not applied")
	}
	call("list_bookmarks", map[string]any{"options": map[string]any{"search": marker, "archived": true}})
	call("unarchive_bookmark", map[string]any{"bookmark_id": ids[0]})
	call("list_tags", map[string]any{"options": map[string]any{"limit": 1}})
	for _, id := range ids {
		v := call("delete_bookmark", map[string]any{"bookmark_id": id})
		if v["deleted"] != true {
			t.Fatal("deletion receipt missing")
		}
	}
	ids = nil
	t.Log("all nine generated operations passed; existing URL, paging, false, empty tags and deletion verified")
}
