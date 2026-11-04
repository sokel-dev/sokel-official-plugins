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

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fake(t *testing.T, body, ct string, status int, inspect func(*http.Request)) (*client, *int) {
	t.Helper()
	count := new(int)
	c := newClient(Cred{})
	c.base = "http://service.lan/prefix/"
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		*count++
		if inspect != nil {
			inspect(r)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return c, count
}

type captureHost struct {
	ops    map[string]plugin.Invoke
	fields []contract.Field
}

func (h *captureHost) Register(o contract.Operation, f plugin.Invoke)   { h.ops[o.ID] = f }
func (h *captureHost) SetCredentialContract(f []contract.Field)         { h.fields = f }
func (*captureHost) SetAuthFlow(contract.AuthMeta, plugin.AuthHandlers) {}
func (*captureHost) DeclareEvent(contract.Event)                        {}
func (*captureHost) DeclareEventsCommon([]contract.Field, []string)     {}

type testCtx struct {
	context.Context
	cred map[string]string
}

func (c *testCtx) Credential() map[string]string    { return c.cred }
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unexpected file fetch") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}

type captureSink struct{ vars map[string]any }

func (s *captureSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}
func TestDeploymentAndTransport(t *testing.T) {
	for _, base := range []string{"", "file:///tmp/x", "https://user:pass@server", "https://server?a=secret", "https://server#frag", "http://server/a/../b", "http://server/%2fother", "http://server\\bad"} {
		c, n := fake(t, `{}`, "application/json", 200, nil)
		c.base = base
		if _, _, e := c.request(context.Background(), "GET", "/health", nil, nil, nil); e == nil || *n != 0 {
			t.Fatalf("bad URL accepted: %q", base)
		}
	}
	for _, base := range []string{"http://127.0.0.1:8765", "http://[::1]:8765/prefix", "https://service.lan/prefix/"} {
		c, _ := fake(t, `{}`, "application/json", 200, nil)
		c.base = base
		if _, _, e := c.request(context.Background(), "GET", "/health", nil, nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	for _, status := range []int{301, 302, 307, 400, 401, 403, 429, 500} {
		c, n := fake(t, "secret-token raw error", "text/plain", status, nil)
		if _, _, e := c.request(context.Background(), "GET", "/health", nil, nil, nil); e == nil || strings.Contains(e.Error(), "secret-token") || *n != 1 {
			t.Fatalf("HTTP %d: %v", status, e)
		}
	}
	c, n := fake(t, strings.Repeat("x", maxResponse+1), "application/json", 200, nil)
	if _, _, e := c.request(context.Background(), "GET", "/health", nil, nil, nil); e == nil {
		t.Fatal("oversize response")
	}
	*n = 0
	if _, _, e := c.request(context.Background(), "POST", "/", nil, make([]byte, (1<<20)+1), nil); e == nil || *n != 0 {
		t.Fatal("oversize request")
	}
	c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-token") })}
	if _, _, e := c.request(context.Background(), "GET", "/", nil, nil, nil); e == nil || strings.Contains(e.Error(), "secret-token") {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := c.request(ctx, "GET", "/", nil, nil, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	for _, tc := range []struct{ body, ct string }{{`null`, "application/json"}, {`{`, "application/json"}, {`{} {}`, "application/json"}, {`{}`, "text/html"}} {
		var out struct{}
		if decodeJSON([]byte(tc.body), http.Header{"Content-Type": {tc.ct}}, &out) == nil {
			t.Fatal("bad JSON accepted")
		}
	}
	var out struct{}
	if e := decodeJSON([]byte(`{}`), http.Header{"Content-Type": {"application/problem+json"}}, &out); e != nil {
		t.Fatal(e)
	}
}
func invoke(t *testing.T, c *client, op, input string, cred map[string]string) *captureSink {
	t.Helper()
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerWithClient(h, func(got Cred) *client { c.cred = got; return c })
	if len(h.ops) != 3 {
		t.Fatal("registration count")
	}
	ctx := &testCtx{context.Background(), cred}
	s := &captureSink{}
	if e := h.ops[op](ctx, json.RawMessage(input), s); e != nil {
		t.Fatal(e)
	}
	if e := h.ops[op](ctx, json.RawMessage(`{bad`), s); e == nil {
		t.Fatal("invalid input accepted")
	}
	return s
}
