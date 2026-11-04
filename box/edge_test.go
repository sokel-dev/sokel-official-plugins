package main

import (
	"context"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"strings"
	"testing"
)

func TestInvalidOperationInputs(t *testing.T) {
	mockTransport(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	ctx := &testCtx{Context: context.Background()}
	for _, tc := range []struct{ op, in string }{
		{"list_folder", `{"folder_id":"../0"}`}, {"list_folder", `{"folder_id":"0","marker":"x\n"}`}, {"search", `{"query":" "}`}, {"search", `{"query":"x","page":{"offset":10001}}`}, {"search", `{"query":"x","page":{"limit":0}}`}, {"search", `{"query":"x","ancestor_folder_id":"../0"}`}, {"get_file", `{"file_id":"0"}`}, {"get_folder", `{"folder_id":"x"}`}, {"create_folder", `{"parent_id":"0","name":"../x"}`}, {"upload_file", `{"parent_id":"0"}`}, {"upload_file", `{"parent_id":"0","file":{"name":"../x"}}`}, {"upload_file", `{"parent_id":"0","file":{"name":"x","size":33554433}}`}, {"download_file", `{"file_id":"x"}`}} {
		if _, e := invoke(t, tc.op, tc.in, ctx); e == nil {
			t.Fatalf("accepted %s %s", tc.op, tc.in)
		}
	}
}
func TestMalformedMetadataAndPagination(t *testing.T) {
	for _, tc := range []struct{ op, in, body string }{
		{"list_folder", `{"folder_id":"0"}`, `{}`}, {"list_folder", `{"folder_id":"0","marker":"M"}`, `{"entries":[],"limit":100,"next_marker":"M"}`}, {"list_folder", `{"folder_id":"0"}`, `{"entries":[{"id":"1","type":"unknown","name":"x"}],"limit":100}`}, {"search", `{"query":"cat"}`, `{"entries":[],"type":"search_results_items","offset":1,"limit":100,"total_count":1}`}, {"get_file", `{"file_id":"123"}`, `{"id":"456","type":"file","name":"x"}`}, {"get_file", `{"file_id":"123"}`, `{"id":"123","type":"file","name":"x","size":-1}`}, {"get_file", `{"file_id":"123"}`, `{"id":"123","type":"file","name":"x","sha1":"wrong"}`}, {"get_folder", `{"folder_id":"10"}`, `{}`}, {"create_folder", `{"parent_id":"0","name":"Folder"}`, `{}`}} {
		t.Run(tc.op+tc.body[:1], func(t *testing.T) {
			mockTransport(t, func(*http.Request) (*http.Response, error) { return response(200, tc.body), nil })
			if _, e := invoke(t, tc.op, tc.in, &testCtx{Context: context.Background()}); e == nil {
				t.Fatal("accepted malformed response")
			}
		})
	}
	t.Run("offset follows limit not count", func(t *testing.T) {
		mockTransport(t, func(*http.Request) (*http.Response, error) {
			return response(200, `{"type":"search_results_items","entries":[],"offset":0,"limit":100,"total_count":101}`), nil
		})
		out, e := testClient().search(&testCtx{Context: context.Background()}, &SearchIn{Query: "cat"})
		if e != nil || !out.HasMore || out.NextOffset != 100 {
			t.Fatal("gapped offset pagination broken", out, e)
		}
	})
	t.Run("last page", func(t *testing.T) {
		mockTransport(t, func(*http.Request) (*http.Response, error) {
			return response(200, `{"type":"search_results_items","entries":[],"offset":0,"limit":100,"total_count":0}`), nil
		})
		out, e := testClient().search(&testCtx{Context: context.Background()}, &SearchIn{Query: "cat"})
		if e != nil || out.HasMore || out.NextOffset != 0 {
			t.Fatal(out, e)
		}
	})
}
func TestDownloadRedirectPolicy(t *testing.T) {
	for _, location := range []string{"http://dl.boxcloud.com/d/x", "https://evil.example/x", "https://boxcloud.com.evil.example/x", "https://user:pass@dl.boxcloud.com/x", "https://dl.boxcloud.com:444/x", "https://dl.boxcloud.com/x#secret", "https://dl.boxcloud.com/x?access_token=secret", "https://dl.boxcloud.com/test-secret", "https://dl.boxcloud.com/%74est-secret", "https://dl.boxcloud.com/x?x=%74est-secret", "//dl.boxcloud.com/x", "/relative"} {
		t.Run(location, func(t *testing.T) {
			calls := 0
			mockTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return response(200, fileMeta), nil
				}
				r2 := response(302, "")
				r2.Header.Set("Location", location)
				return r2, nil
			})
			_, e := testClient().download_file(&testCtx{Context: context.Background()}, &DownloadFileIn{FileID: "123"})
			if e == nil || calls != 2 || strings.Contains(e.Error(), "test-secret") {
				t.Fatalf("redirect accepted/leaked %v calls=%d", e, calls)
			}
		})
	}
	t.Run("redirect loop bounded", func(t *testing.T) {
		calls := 0
		mockTransport(t, func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(200, fileMeta), nil
			}
			if calls > 2 && r.Header.Get("Authorization") != "" {
				t.Fatal("bearer forwarded")
			}
			res := response(302, "")
			res.Header.Set("Location", "https://dl.boxcloud.com/d/x")
			return res, nil
		})
		_, e := testClient().download_file(&testCtx{Context: context.Background()}, &DownloadFileIn{FileID: "123"})
		if e == nil || calls != 5 {
			t.Fatal("unbounded redirects", calls, e)
		}
	})
}
func TestFileFailuresAndEmptyFiles(t *testing.T) {
	for _, mode := range []string{"fetch", "oversize upload", "bad receipt", "no size", "oversize download", "wrong bytes", "checksum", "store", "not ready", "http error"} {
		t.Run(mode, func(t *testing.T) {
			ctx := &testCtx{Context: context.Background(), content: []byte("abc")}
			if mode == "fetch" {
				ctx.failFetch = true
			}
			if mode == "oversize upload" {
				ctx.content = make([]byte, maxFile+1)
			}
			mockTransport(t, func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					return response(200, `{}`), nil
				}
				if !strings.HasSuffix(r.URL.Path, "/content") {
					meta := fileMeta
					if mode == "no size" {
						meta = `{"id":"123","type":"file","name":"x"}`
					}
					if mode == "oversize download" {
						meta = strings.ReplaceAll(meta, `"size":3`, `"size":33554433`)
					}
					return response(200, meta), nil
				}
				res := response(200, "abc")
				switch mode {
				case "wrong bytes":
					res = response(200, "ab")
				case "checksum":
					res = response(200, "xyz")
				case "not ready":
					res = response(202, "")
					res.Header.Set("Retry-After", "10")
				case "http error":
					res = response(401, "test-secret")
				}
				return res, nil
			})
			var e error
			switch mode {
			case "fetch", "oversize upload", "bad receipt":
				_, e = testClient().upload_file(ctx, &UploadFileIn{ParentID: "0", File: &plugin.File{Name: "x"}})
			default:
				ctx.failUpload = mode == "store"
				_, e = testClient().download_file(ctx, &DownloadFileIn{FileID: "123"})
			}
			if e == nil || strings.Contains(e.Error(), "sensitive-") {
				t.Fatal("unsafe file failure", e)
			}
		})
	}
	t.Run("empty file", func(t *testing.T) {
		mockTransport(t, func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/content") {
				return response(200, ""), nil
			}
			return response(200, `{"id":"123","type":"file","name":"empty","size":0,"sha1":"da39a3ee5e6b4b0d3255bfef95601890afd80709"}`), nil
		})
		out, e := testClient().download_file(&testCtx{Context: context.Background()}, &DownloadFileIn{FileID: "123"})
		if e != nil || out.File.Size != 0 {
			t.Fatal(e)
		}
	})
}
