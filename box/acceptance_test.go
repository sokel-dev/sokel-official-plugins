package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

type testCtx struct {
	context.Context
	content               []byte
	uploaded              []byte
	failFetch, failUpload bool
}

func (c *testCtx) Credential() map[string]string {
	return map[string]string{"access_token": "test-secret"}
}
func (c *testCtx) Fetch(*plugin.File) ([]byte, error) {
	if c.failFetch {
		return nil, errors.New("sensitive-source-url")
	}
	return c.content, nil
}
func (c *testCtx) Upload(name, mime string, b []byte) (*plugin.File, error) {
	if c.failUpload {
		return nil, errors.New("sensitive-storage-url")
	}
	c.uploaded = append([]byte{}, b...)
	return &plugin.File{Name: name, Mime: mime, Size: int64(len(b))}, nil
}
func (c *testCtx) UploadReader(name, mime string, r io.Reader) (*plugin.File, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	return c.Upload(name, mime, b)
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(o contract.Operation, fn plugin.Invoke) { h.calls[o.ID] = fn }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func invoke(t *testing.T, op, input string, ctx *testCtx) (map[string]any, error) {
	t.Helper()
	h := &testHost{map[string]plugin.Invoke{}}
	registerOperations(h)
	s := &testSink{}
	e := h.calls[op](ctx, json.RawMessage(input), s)
	return s.vars, e
}
func mockTransport(t *testing.T, fn transportFunc) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() { http.DefaultTransport = old })
}
func checkJSON(t *testing.T, b []byte, want string) {
	t.Helper()
	var x, y any
	if json.Unmarshal(b, &x) != nil || json.Unmarshal([]byte(want), &y) != nil {
		t.Fatalf("invalid JSON %s", b)
	}
	a, _ := json.Marshal(x)
	z, _ := json.Marshal(y)
	if string(a) != string(z) {
		t.Fatalf("body %s want %s", a, z)
	}
}

const fileMeta = `{"id":"123","type":"file","name":"图😀.txt","size":3,"sha1":"a9993e364706816aba3e25717850c26c9cd0d89d","etag":"1"}`
const folderMeta = `{"id":"10","type":"folder","name":"Folder"}`

func TestDocumentedWireRequests(t *testing.T) {
	for _, tc := range []struct{ op, input, method, path, body, result, key string }{
		{"health_check", `{}`, "GET", "/2.0/users/me", "", `{"id":"42","type":"user","name":"Test"}`, "ok"},
		{"list_folder", `{"folder_id":"0","marker":"M1"}`, "GET", "/2.0/folders/0/items", "", `{"entries":[` + folderMeta + `],"limit":100,"next_marker":"M2"}`, "next_marker"},
		{"search", `{"query":"cat","ancestor_folder_id":"0","page":{"offset":0,"limit":10}}`, "GET", "/2.0/search", "", `{"type":"search_results_items","entries":[` + fileMeta + `],"limit":10,"offset":0,"total_count":11}`, "next_offset"},
		{"get_file", `{"file_id":"123"}`, "GET", "/2.0/files/123", "", fileMeta, "item"},
		{"get_folder", `{"folder_id":"10"}`, "GET", "/2.0/folders/10", "", folderMeta, "item"},
		{"create_folder", `{"parent_id":"0","name":"Folder"}`, "POST", "/2.0/folders", `{"name":"Folder","parent":{"id":"0"}}`, folderMeta, "item"},
		{"upload_file", `{"parent_id":"0","file":{"name":"图😀.txt","mime":"text/plain","size":3}}`, "POST", "/api/2.0/files/content", "", `{"total_count":1,"entries":[` + fileMeta + `]}`, "item"},
		{"download_file", `{"file_id":"123"}`, "GET", "/2.0/files/123", "", fileMeta, "file"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			calls := 0
			mockTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.op == "download_file" && calls == 2 {
					if r.URL.Path != "/2.0/files/123/content" || r.Header.Get("Authorization") != "Bearer test-secret" {
						t.Fatal("bad content request")
					}
					out := response(302, "")
					out.Header.Set("Location", "https://dl.boxcloud.com/d/test/download")
					return out, nil
				}
				if tc.op == "download_file" && calls == 3 {
					if r.URL.Host != "dl.boxcloud.com" || r.Header.Get("Authorization") != "" {
						t.Fatal("download bearer leaked")
					}
					out := response(200, "abc")
					out.Header.Set("Content-Type", "text/plain")
					return out, nil
				}
				host := "api.box.com"
				if tc.op == "upload_file" {
					host = "upload.box.com"
				}
				if r.Method != tc.method || r.URL.Path != tc.path || r.URL.Host != host || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				if tc.body != "" {
					b, _ := io.ReadAll(r.Body)
					checkJSON(t, b, tc.body)
				}
				switch tc.op {
				case "list_folder":
					if r.URL.Query().Get("usemarker") != "true" || r.URL.Query().Get("marker") != "M1" {
						t.Fatal("marker lost")
					}
				case "search":
					if r.URL.Query().Get("offset") != "0" || r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("ancestor_folder_ids") != "0" {
						t.Fatal("search query lost")
					}
				case "upload_file":
					mr, e := r.MultipartReader()
					if e != nil {
						t.Fatal(e)
					}
					p, e := mr.NextPart()
					if e != nil || p.FormName() != "attributes" {
						t.Fatal("attributes must be first")
					}
					b, _ := io.ReadAll(p)
					checkJSON(t, b, `{"name":"图😀.txt","parent":{"id":"0"}}`)
					p, e = mr.NextPart()
					if e != nil || p.FormName() != "file" || p.FileName() != "图😀.txt" || strings.Contains(p.Header.Get("Content-Disposition"), "filename*") {
						t.Fatal("invalid UTF-8 file part")
					}
					b, _ = io.ReadAll(p)
					if string(b) != "abc" {
						t.Fatal("upload bytes changed")
					}
				}
				return response(200, tc.result), nil
			})
			ctx := &testCtx{Context: context.Background(), content: []byte("abc")}
			out, e := invoke(t, tc.op, tc.input, ctx)
			if e != nil {
				t.Fatal(e)
			}
			if _, ok := out[tc.key]; !ok {
				t.Fatalf("missing output %+v", out)
			}
			if tc.op == "download_file" {
				if calls != 3 || string(ctx.uploaded) != "abc" {
					t.Fatal("download flow failed")
				}
			} else if calls != 1 {
				t.Fatal("unexpected request count")
			}
			if tc.op == "health_check" && out["ok"] != true {
				t.Fatal(out)
			}
		})
	}
}
