package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fresh() *client {
	c := newClient(Cred{AccessToken: "synthetic-test-token"})
	c.baseURL = "http://service.internal/prefix"
	return c
}
func httpResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func withReply(c *client, status int, body string) {
	c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { return httpResponse(status, body), nil })}
}

type testCtx struct{ context.Context }

func (*testCtx) Credential() map[string]string {
	return map[string]string{"access_token": "synthetic-test-token"}
}
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("not used") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("not used")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("not used")
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(o contract.Operation, f plugin.Invoke) { h.calls[o.ID] = f }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}

type fixture struct {
	Op                        string
	Input                     json.RawMessage
	Method, Path, Query, Body string
	Status                    int
	Response                  string
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	b, e := os.ReadFile("testdata/documented-wire.json")
	if e != nil {
		t.Fatal(e)
	}
	var f []fixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestDocumentedWireRequests(t *testing.T) {
	t.Setenv("SOKEL_BASEROW_URL", "http://service.internal/prefix")
	p := sokel.New(sokel.Config{Name: "baserow"})
	register(p)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for _, f := range fixtures(t) {
		t.Run(f.Op, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = transport(func(r *http.Request) (*http.Response, error) {
				calls++
				b, _ := io.ReadAll(r.Body)
				if r.Method != f.Method || r.URL.Path != "/prefix"+f.Path || r.URL.RawQuery != f.Query || string(b) != f.Body {
					t.Errorf("wire: %s %s ?%s %s; want %s %s ?%s %s", r.Method, r.URL.Path, r.URL.RawQuery, b, f.Method, "/prefix"+f.Path, f.Query, f.Body)
				}
				if r.Header.Get("Authorization") != "Token synthetic-test-token" {
					t.Error("missing header auth")
				}
				if r.Method != http.MethodGet && r.GetBody != nil {
					t.Error("mutation body is replayable")
				}
				return httpResponse(f.Status, f.Response), nil
			})
			s := &testSink{}
			fn := h.calls[f.Op]
			if fn == nil {
				t.Fatal("not registered")
			}
			e := fn(&testCtx{context.Background()}, f.Input, s)
			if e != nil || s.vars == nil || calls != 1 {
				t.Fatalf("invoke %v output %v calls %d", e, s.vars, calls)
			}
			if f.Op == "health_check" && s.vars["ok"] != true {
				t.Fatal("health failed", s.vars)
			}
			b, _ := json.Marshal(s.vars)
			if f.Op != "health_check" && strings.Contains(f.Response, "9007199254740993") && !strings.Contains(string(b), "9007199254740993") {
				t.Fatal("large ID or JSON integer lost", string(b))
			}
			if f.Op == "list_rows" || f.Op == "list_projects" || f.Op == "list_tasks" {
				if s.vars["has_more"] != true {
					t.Fatal("pagination lost", s.vars)
				}
			}
		})
	}
}
