package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/dropbox/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"strings"
	"testing"
)

func TestInvalidOperationInputs(t *testing.T) {
	mockTransport(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	ctx := &testCtx{Context: context.Background()}
	for _, tc := range []struct{ op, in string }{
		{"list_folder", `{"path":"/"}`}, {"list_folder", `{"path":"../x"}`}, {"list_folder", `{"options":{"limit":0}}`}, {"continue_list", `{"cursor":""}`}, {"continue_search", `{"cursor":"x\n"}`}, {"get_metadata", `{"path":""}`}, {"search", `{"query":" "}`}, {"search", `{"query":"cat","options":{"path":"relative"}}`}, {"search", `{"query":"cat","options":{"max_results":0}}`}, {"upload_file", `{"path":"id:abc","file":{}}`}, {"upload_file", `{"path":"/x"}`}, {"upload_file", `{"path":"/x","file":{"size":33554433}}`}, {"download_file", `{"path":"/x/../y"}`}} {
		if _, e := invoke(t, tc.op, tc.in, ctx); e == nil {
			t.Fatalf("accepted %s %s", tc.op, tc.in)
		}
	}
}
func TestMetadataTagsAndPagination(t *testing.T) {
	for _, tc := range []struct{ op, in, body string }{
		{"list_folder", `{}`, `{}`}, {"continue_list", `{"cursor":"C1"}`, `{"entries":[],"has_more":true,"cursor":"C1"}`}, {"list_folder", `{}`, `{"entries":[{".tag":"mystery","name":"x"}],"has_more":false,"cursor":"C"}`}, {"get_metadata", `{"path":"id:different"}`, fileMeta}, {"get_metadata", `{"path":"/x"}`, `{".tag":"folder","name":"x"}`}, {"search", `{"query":"cat"}`, `{"matches":[],"has_more":true}`}, {"search", `{"query":"cat"}`, `{"matches":[{"metadata":{".tag":"other"}}],"has_more":false}`}, {"search", `{"query":"cat"}`, `{"matches":[{"metadata":{".tag":"metadata","metadata":{".tag":"file","name":"x","id":"id:x"}}}],"has_more":false}`}} {
		t.Run(tc.op+tc.body[:1], func(t *testing.T) {
			mockTransport(t, func(*http.Request) (*http.Response, error) { return response(200, tc.body), nil })
			if _, e := invoke(t, tc.op, tc.in, &testCtx{Context: context.Background()}); e == nil {
				t.Fatal("accepted malformed response")
			}
		})
	}
	t.Run("unchanged change cursor", func(t *testing.T) {
		mockTransport(t, func(*http.Request) (*http.Response, error) {
			return response(200, `{"entries":[],"has_more":false,"cursor":"C1"}`), nil
		})
		out, e := invoke(t, "continue_list", `{"cursor":"C1"}`, &testCtx{Context: context.Background()})
		if e != nil || out["cursor"] != "C1" {
			t.Fatal("valid idle cursor rejected")
		}
	})
	var entry schema.Entry
	if json.Unmarshal([]byte(folderMeta), &entry) != nil || validEntry(&entry, false) != nil {
		t.Fatal("folder rejected")
	}
	entry.Name = "../bad"
	if validEntry(&entry, false) == nil {
		t.Fatal("bad name")
	}
	if _, e := asciiJSON(make(chan int)); e == nil {
		t.Fatal("invalid header JSON")
	}
	arg, e := asciiJSON(map[string]string{"path": "/\u007f图😀.txt"})
	if e != nil || !strings.Contains(arg, `\u007f`) || !strings.Contains(arg, `\ud83d\ude00`) {
		t.Fatal(arg, e)
	}
}
func TestFileFailuresAndEmptyFiles(t *testing.T) {
	for _, mode := range []string{"fetch", "oversize", "bad receipt", "bad metadata", "oversize download", "wrong bytes", "store", "wrong id", "http error"} {
		t.Run(mode, func(t *testing.T) {
			ctx := &testCtx{Context: context.Background(), content: []byte("abc")}
			if mode == "fetch" {
				ctx.failFetch = true
			}
			if mode == "oversize" {
				ctx.content = make([]byte, maxFile+1)
			}
			mockTransport(t, func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "upload") {
					if mode == "bad receipt" {
						return response(200, `{}`), nil
					}
					return response(200, fileMeta), nil
				}
				res := response(200, "abc")
				meta := fileMeta
				switch mode {
				case "bad metadata":
					meta = `{}`
				case "oversize download":
					meta = strings.ReplaceAll(meta, `"size":3`, `"size":33554433`)
				case "wrong bytes":
					res = response(200, "ab")
				case "http error":
					res = response(401, "test-secret")
				}
				res.Header.Set("Dropbox-API-Result", meta)
				return res, nil
			})
			var e error
			switch mode {
			case "fetch", "oversize", "bad receipt":
				_, e = testClient().upload_file(ctx, &UploadFileIn{Path: "/x", File: &plugin.File{Name: "x"}})
			default:
				ctx.failUpload = mode == "store"
				path := "/x"
				if mode == "wrong id" {
					path = "id:wrong"
				}
				_, e = testClient().download_file(ctx, &DownloadFileIn{Path: path})
			}
			if e == nil || strings.Contains(e.Error(), "sensitive-") {
				t.Fatal("unsafe file failure", e)
			}
		})
	}
	t.Run("empty download", func(t *testing.T) {
		mockTransport(t, func(*http.Request) (*http.Response, error) {
			r := response(200, "")
			r.Header.Set("Dropbox-API-Result", strings.ReplaceAll(fileMeta, `"size":3`, `"size":0`))
			return r, nil
		})
		out, e := testClient().download_file(&testCtx{Context: context.Background()}, &DownloadFileIn{Path: "/x"})
		if e != nil || out.File.Size != 0 {
			t.Fatal(e)
		}
	})
	t.Run("content redirect forbidden", func(t *testing.T) {
		calls := 0
		mockTransport(t, func(*http.Request) (*http.Response, error) {
			calls++
			r := response(302, "")
			r.Header.Set("Location", "https://evil.example")
			return r, nil
		})
		_, e := testClient().download_file(&testCtx{Context: context.Background()}, &DownloadFileIn{Path: "/x"})
		if e == nil || calls != 1 {
			t.Fatal("redirect followed")
		}
	})
}
