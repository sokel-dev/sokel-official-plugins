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

const fileMeta = `{".tag":"file","name":"图😀.txt","id":"id:abc","path_lower":"/图😀.txt","path_display":"/图😀.txt","size":3,"rev":"abc123","server_modified":"2026-10-01T00:00:00Z","is_downloadable":true}`
const folderMeta = `{".tag":"folder","name":"Folder","id":"id:folder","path_lower":"/folder"}`

func TestDocumentedWireRequests(t *testing.T) {
	for _, tc := range []struct {
		op, input, route, body, result, key string
		content                             bool
	}{
		{"health_check", `{}`, "users/get_current_account", `null`, `{"account_id":"dbid:abc","name":{"display_name":"Test"}}`, "ok", false},
		{"list_folder", `{"path":"","options":{"recursive":false,"include_deleted":true,"limit":10}}`, "files/list_folder", `{"path":"","recursive":false,"include_deleted":true,"limit":10}`, `{"entries":[` + folderMeta + `],"cursor":"C1","has_more":false}`, "entries", false},
		{"continue_list", `{"cursor":"C1"}`, "files/list_folder/continue", `{"cursor":"C1"}`, `{"entries":[{".tag":"deleted","name":"old","path_lower":"/old"}],"cursor":"C2","has_more":false}`, "cursor", false},
		{"get_metadata", `{"path":"id:abc"}`, "files/get_metadata", `{"path":"id:abc","include_deleted":true}`, fileMeta, "entry", false},
		{"search", `{"query":"cat","options":{"filename_only":false,"max_results":10}}`, "files/search_v2", `{"query":"cat","options":{"filename_only":false,"max_results":10}}`, `{"matches":[{"metadata":{".tag":"metadata","metadata":` + fileMeta + `}}],"has_more":true,"cursor":"S1"}`, "entries", false},
		{"continue_search", `{"cursor":"S1"}`, "files/search/continue_v2", `{"cursor":"S1"}`, `{"matches":[],"has_more":false}`, "has_more", false},
		{"upload_file", `{"path":"/图😀.txt","file":{"name":"图😀.txt","size":3,"mime":"text/plain"}}`, "files/upload", `{"path":"/图😀.txt","mode":"add","autorename":false,"strict_conflict":true}`, fileMeta, "entry", true},
		{"download_file", `{"path":"/图😀.txt"}`, "files/download", `{"path":"/图😀.txt"}`, "abc", "file", true},
	} {
		t.Run(tc.op, func(t *testing.T) {
			calls := 0
			mockTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				host := "api.dropboxapi.com"
				if tc.content {
					host = "content.dropboxapi.com"
				}
				if r.Method != "POST" || r.URL.Host != host || r.URL.Path != "/2/"+tc.route || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
				}
				b, _ := io.ReadAll(r.Body)
				if tc.content {
					arg := r.Header.Get("Dropbox-API-Arg")
					for _, v := range []byte(arg) {
						if v >= 127 {
							t.Fatal("non-ASCII API-Arg")
						}
					}
					checkJSON(t, []byte(arg), tc.body)
					if tc.op == "upload_file" && string(b) != "abc" {
						t.Fatal("upload bytes changed")
					}
					if tc.op == "download_file" && len(b) != 0 {
						t.Fatal("download body must be empty")
					}
				} else {
					checkJSON(t, b, tc.body)
				}
				out := response(200, tc.result)
				if tc.op == "download_file" {
					out.Header.Set("Dropbox-API-Result", fileMeta)
					out.Header.Set("Content-Type", "text/plain")
				}
				return out, nil
			})
			ctx := &testCtx{Context: context.Background(), content: []byte("abc")}
			out, e := invoke(t, tc.op, tc.input, ctx)
			if e != nil {
				t.Fatal(e)
			}
			if _, ok := out[tc.key]; !ok || calls != 1 {
				t.Fatalf("missing output or call: %+v %d", out, calls)
			}
			if tc.op == "health_check" && out["ok"] != true {
				t.Fatal(out)
			}
			if tc.op == "download_file" && string(ctx.uploaded) != "abc" {
				t.Fatal("download bytes changed")
			}
		})
	}
}
