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

type testCtx struct {
	context.Context
	data, stored []byte
	fetches      int
	failStore    bool
}

func (*testCtx) Credential() map[string]string {
	return map[string]string{"user_id": "alice", "app_password": "secret"}
}
func (c *testCtx) Fetch(*plugin.File) ([]byte, error) { c.fetches++; return c.data, nil }
func (c *testCtx) Upload(name, ct string, b []byte) (*plugin.File, error) {
	if c.failStore {
		return nil, errors.New("SECRET storage")
	}
	c.stored = b
	return &plugin.File{ID: "f_result", Name: name, Mime: ct, Size: int64(len(b))}, nil
}
func (c *testCtx) UploadReader(name, ct string, r io.Reader) (*plugin.File, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	return c.Upload(name, ct, b)
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(o contract.Operation, f plugin.Invoke) { h.calls[o.ID] = f }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func invoke(t *testing.T, c *client, ctx *testCtx, op string, in any) (map[string]any, error) {
	t.Helper()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerWithClient(h, func(cred Cred) *client {
		if cred.UserID != "alice" || cred.AppPassword != "secret" {
			t.Fatal("credential lost")
		}
		return c
	})
	if len(h.calls) != 6 {
		t.Fatal("registration count")
	}
	b, _ := json.Marshal(in)
	sink := &testSink{}
	e := h.calls[op](ctx, b, sink)
	return sink.vars, e
}

const fileXML = `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/cloud/remote.php/dav/files/alice/Reports/a%20b.txt</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>3</d:getcontentlength><d:getetag>&quot;v1&quot;</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`

func TestAllGeneratedOperationsAndFiles(t *testing.T) {
	for _, op := range []string{"get_info", "list_directory", "create_directory", "upload_file", "download_file", "move_resource"} {
		t.Run(op, func(t *testing.T) {
			ctx := &testCtx{Context: context.Background(), data: []byte("abc")}
			c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
			c.base = "http://cloud.local/cloud"
			calls := 0
			c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.Method {
				case "PROPFIND":
					if op == "list_directory" {
						return fixture(207, "application/xml", directoryXML), nil
					}
					return fixture(207, "application/xml", fileXML), nil
				case "GET":
					if r.Header.Get("If-Match") != `"v1"` {
						t.Fatal("download missing concurrency protection")
					}
					return fixture(200, "text/plain", "abc"), nil
				case "PUT":
					b, _ := io.ReadAll(r.Body)
					if string(b) != "abc" || r.Header.Get("If-None-Match") != "*" {
						t.Fatal("unsafe upload")
					}
					return fixture(201, "", ""), nil
				case "MKCOL", "MOVE":
					return fixture(201, "", ""), nil
				}
				t.Fatalf("unexpected method %s", r.Method)
				return nil, nil
			})}
			in := map[string]any{"path": "Reports/a b.txt"}
			if op == "list_directory" {
				in["path"] = "Reports"
			}
			if op == "upload_file" {
				in["file"] = map[string]any{"id": "f_input", "name": "a.txt", "size": 3}
			}
			if op == "move_resource" {
				in = map[string]any{"source_path": "Reports/a b.txt", "destination_path": "Reports/moved.txt"}
			}
			vars, e := invoke(t, c, ctx, op, in)
			if e != nil {
				t.Fatal(e)
			}
			if len(vars) == 0 {
				t.Fatal("missing output")
			}
			if op == "download_file" && (calls != 2 || string(ctx.stored) != "abc") {
				t.Fatal("native download failed")
			}
			if op == "upload_file" && ctx.fetches != 1 {
				t.Fatal("native fetch missing")
			}
		})
	}
}
func TestRejectUnsafeUploadBeforeFetch(t *testing.T) {
	ctx := &testCtx{Context: context.Background()}
	c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
	c.base = "http://cloud.local/cloud"
	for _, in := range []UploadFileIn{{Path: "a", File: &plugin.File{URL: "https://secret/file"}}, {Path: "a", File: &plugin.File{ID: "f", Size: maxFile + 1}}, {Path: "../a", File: &plugin.File{ID: "f"}}, {Path: "a", File: &plugin.File{ID: "f"}, IfMatch: `"v1"`}} {
		if _, e := c.uploadFile(ctx, &in); e == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	if ctx.fetches != 0 {
		t.Fatal("fetched invalid file")
	}
}
func TestUntrustedHrefAndXML(t *testing.T) {
	c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
	c.base = "http://cloud.local/cloud"
	for _, bad := range []string{"http://evil.local/cloud/remote.php/dav/files/alice/Reports/a%20b.txt", "/cloud/remote.php/dav/files/bob/Reports/a%20b.txt", "/cloud/remote.php/dav/files/alice/Reports/a%2fb.txt", "/cloud/remote.php/dav/files/alice/Reports/%2e%2e/secret"} {
		c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return fixture(207, "application/xml", strings.ReplaceAll(fileXML, "/cloud/remote.php/dav/files/alice/Reports/a%20b.txt", bad)), nil
		})}
		if _, e := c.getInfo(context.Background(), &GetInfoIn{Path: "Reports/a b.txt"}); e == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return fixture(207, "application/xml", fileXML+`<extra/>`), nil
	})}
	if _, e := c.getInfo(context.Background(), &GetInfoIn{Path: "Reports/a b.txt"}); e == nil {
		t.Fatal("accepted trailing XML")
	}
}
