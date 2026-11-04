package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

const fileJSON = `{"id":"item1","name":"报告.txt","size":3,"eTag":"v1","lastModifiedDateTime":"2026-01-01T00:00:00Z","file":{"mimeType":"text/plain"},"@microsoft.graph.downloadUrl":"https://tenant.sharepoint.com/download?auth=temporary"}`

func TestGeneratedDriveReadSearchAndDownloadInfo(t *testing.T) {
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/v1.0/me/drive":
			reply(w, `{"id":"drive","name":"Documents","driveType":"business","webUrl":"https://tenant.sharepoint.com"}`)
		case "/v1.0/drives/drive/items/item1":
			reply(w, fileJSON)
		case "/v1.0/drives/drive/root/search(q='O''Brien / 财务')":
			if r.URL.Query().Get("$top") != "100" {
				t.Fatal("missing page size")
			}
			reply(w, `{"value":[`+fileJSON+`]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	v := invoke(t, c, "get_drive", `{}`, nil)
	if v["drive"].(map[string]any)["drive_type"] != "business" {
		t.Fatal(v)
	}
	v = invoke(t, c, "get_item", `{"drive_id":"drive","item_id":"item1"}`, nil)
	if v["item"].(map[string]any)["etag"] != "v1" {
		t.Fatal(v)
	}
	invoke(t, c, "search_items", `{"drive_id":"drive","query":"O'Brien / 财务"}`, nil)
	v = invoke(t, c, "get_download_info", `{"drive_id":"drive","item_id":"item1"}`, nil)
	if !strings.HasPrefix(v["download_url"].(string), "https://tenant.sharepoint.com/") || calls != 4 {
		t.Fatal("download URL was fetched or lost")
	}
}
func TestGeneratedUploadExactBytes(t *testing.T) {
	payload := []byte{0, 1, 255}
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || r.URL.Path != "/v1.0/drives/drive/root:/报告.txt:/content" || r.Header.Get("If-Match") != "v1" || r.Header.Get("Content-Type") != "application/octet-stream" || string(b) != string(payload) {
			t.Fatalf("wrong upload %s %s %q", r.Method, r.URL, b)
		}
		w.WriteHeader(201)
		reply(w, fileJSON)
	})
	ctx := &testCtx{Context: context.Background(), file: payload}
	v := invoke(t, c, "upload_file", `{"drive_id":"drive","if_match":"v1","file":{"name":"报告.txt","mime":"text/plain","size":3,"key":"test"}}`, ctx)
	if v["item"].(map[string]any)["id"] != "item1" {
		t.Fatal(v)
	}
}
func TestDeltaRetainsDeletionAndRepeatedIDs(t *testing.T) {
	link := graphOrigin + "/v1.0/drives/drive/root/delta?$deltatoken=abc"
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, `{"value":[`+fileJSON+`,{"id":"item1","deleted":{}}],"@odata.deltaLink":"`+link+`"}`)
	})
	v := invoke(t, c, "delta", `{"drive_id":"drive"}`, nil)
	items := v["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["deleted"] != true || v["delta_link"] != link {
		t.Fatal(v)
	}
	out, e := c.delta(context.Background(), &DeltaIn{DriveID: "drive", ContinuationURL: link})
	if e != nil || out.DeltaLink != link {
		t.Fatal("unchanged checkpoint must be accepted")
	}
}
func TestDriveValidationAndMalformedReceipts(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached network") })
	if _, e := c.getItem(context.Background(), &GetItemIn{DriveID: "drive", ItemID: "../bad"}); e == nil {
		t.Fatal("path injection")
	}
	if _, e := c.searchItems(context.Background(), &SearchItemsIn{DriveID: "drive"}); e == nil {
		t.Fatal("empty query")
	}
	for _, f := range []*plugin.File{nil, {Name: "../file", Size: 2}, {Name: "a.txt", Size: 5 << 20}} {
		if _, e := c.uploadFile(&testCtx{Context: context.Background()}, &UploadFileIn{DriveID: "drive", File: f}); e == nil {
			t.Fatal("invalid file")
		}
	}
	if _, e := c.uploadFile(&testCtx{Context: context.Background(), fetchErr: errors.New("signed-secret")}, &UploadFileIn{DriveID: "drive", File: &plugin.File{Name: "a.txt"}}); e == nil || strings.Contains(e.Error(), "signed-secret") {
		t.Fatal("file fetch leak")
	}
	for _, body := range []string{`{}`, `{"value":null}`, `{"value":[{"id":"item1"}]}`, `{"value":[],"@odata.nextLink":"https://evil.test/v1.0/drives/drive/root/children"}`} {
		c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
		if _, e := c.listItems(context.Background(), &ListItemsIn{DriveID: "drive"}); e == nil {
			t.Fatalf("malformed page accepted %s", body)
		}
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{"value":[]}`) })
	if _, e := c.delta(context.Background(), &DeltaIn{DriveID: "drive"}); e == nil {
		t.Fatal("missing delta checkpoint")
	}
	for _, url := range []string{"http://tenant.sharepoint.com/file", "https://u:p@tenant.sharepoint.com/file", "https://127.0.0.1/file"} {
		c = fixture(t, func(w http.ResponseWriter, r *http.Request) {
			reply(w, strings.ReplaceAll(fileJSON, "https://tenant.sharepoint.com/download?auth=temporary", url))
		})
		if _, e := c.getDownloadInfo(context.Background(), &GetDownloadInfoIn{DriveID: "drive", ItemID: "item1"}); e == nil {
			t.Fatal("unsafe download URL")
		}
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if _, e := c.uploadFile(&testCtx{Context: context.Background(), file: []byte("abc")}, &UploadFileIn{DriveID: "drive", File: &plugin.File{Name: "a.txt"}}); e == nil {
		t.Fatal("missing upload receipt")
	}
}

func TestDeltaFunctionLinksAndSparseMetadata(t *testing.T) {
	paths := []string{"/v1.0/drives/drive/root/delta(token='opaque%2Btoken')", "/v1.0/drives/drive/delta(token=opaque-token)"}
	for _, path := range paths {
		link := graphOrigin + path
		c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			reply(w, `{"value":[{"id":"folder1","name":"Folder","folder":{}}],"@odata.deltaLink":"`+link+`"}`)
		})
		out, err := c.delta(context.Background(), &DeltaIn{DriveID: "drive"})
		if err != nil || out.DeltaLink != link || len(out.Items) != 1 {
			t.Fatalf("official delta function shape rejected: %+v %v", out, err)
		}
		if _, err = c.delta(context.Background(), &DeltaIn{DriveID: "drive", ContinuationURL: link}); err != nil {
			t.Fatal(err)
		}
	}
}
