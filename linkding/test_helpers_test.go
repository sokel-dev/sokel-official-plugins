package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testCtx struct {
	context.Context
	credentials map[string]string
}

func (c *testCtx) Credential() map[string]string    { return c.credentials }
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unexpected file fetch") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(o contract.Operation, f plugin.Invoke) { h.calls[o.ID] = f }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func dispatch(t *testing.T, op string, in any, creds map[string]string) (map[string]any, error) {
	t.Helper()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	b, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	sink := &testSink{}
	if h.calls[op] == nil {
		t.Fatal("operation not registered")
	}
	e = h.calls[op](&testCtx{Context: context.Background(), credentials: creds}, b, sink)
	return sink.vars, e
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixtureServer struct {
	URL      string
	previous http.RoundTripper
}

func newFixtureServer(h http.Handler) *fixtureServer {
	s := &fixtureServer{URL: "http://fixture.invalid", previous: http.DefaultTransport}
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result(), nil
	})
	return s
}
func (s *fixtureServer) Close() { http.DefaultTransport = s.previous }
