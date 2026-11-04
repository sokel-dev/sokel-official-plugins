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
func response(ct, body string) *http.Response {
	return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body))}
}

type testCtx struct {
	context.Context
	data                  []byte
	stored                []byte
	failFetch, failUpload bool
	fetches               int
}

func (c *testCtx) Credential() map[string]string {
	return map[string]string{"username": "user", "password": "secret"}
}
func (c *testCtx) Fetch(f *plugin.File) ([]byte, error) {
	c.fetches++
	if c.failFetch {
		return nil, errors.New("secret fetch URL")
	}
	return c.data, nil
}
func (c *testCtx) Upload(name, mime string, b []byte) (*plugin.File, error) {
	if c.failUpload {
		return nil, errors.New("secret upload URL")
	}
	c.stored = append([]byte{}, b...)
	return &plugin.File{ID: "stored", Name: name, Mime: mime, Size: int64(len(b))}, nil
}
func (c *testCtx) UploadReader(name, mime string, r io.Reader) (*plugin.File, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	return c.Upload(name, mime, b)
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(o contract.Operation, f plugin.Invoke) { h.calls[o.ID] = f }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func dispatch(t *testing.T, op, input string, ctx *testCtx) (map[string]any, error) {
	t.Helper()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	sink := &testSink{}
	e := h.calls[op](ctx, json.RawMessage(input), sink)
	return sink.vars, e
}
func testClient(fn roundTrip) *client {
	c := newClient("http://convert.internal:3000/prefix", Cred{Username: "user", Password: "secret"})
	c.http = &http.Client{Transport: fn}
	return c
}
func ctxFor(data string) *testCtx { return &testCtx{Context: context.Background(), data: []byte(data)} }
