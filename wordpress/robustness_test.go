package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testCred = Cred{Username: "editor", ApplicationPassword: "test-secret"}

func mockClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return &client{base: s.URL, cred: testCred, http: s.Client()}
}
func reply(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, body)
}
func TestErrorsNoLeaksAndNoWriteRetry(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "90")
				w.WriteHeader(status)
				reply(w, `{"error":"test-secret token in body"}`)
			})
			var out map[string]any
			_, e := c.json(context.Background(), "POST", "/probe", nil, map[string]string{"clear": ""}, &out)
			if e == nil || strings.Contains(e.Error(), "test-secret") || calls.Load() != 1 {
				t.Fatalf("unsafe/retried error: %v count=%d", e, calls.Load())
			}
			var ae *apiError
			if !errors.As(e, &ae) || ae.Status != status {
				t.Fatalf("not typed: %v", e)
			}
			if status == 429 && ae.RetryAfter != 90*time.Second {
				t.Fatal("retry-after lost")
			}
		})
	}
}
func TestRedirectAndJSONBoundaries(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	var out map[string]any
	if _, e := c.json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || leaked.Load() != 0 {
		t.Fatal("redirect accepted")
	}
	for _, body := range []string{`not JSON`, `{} {}`, strings.Repeat("x", maxJSON+1)} {
		c = mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
		if _, e := c.json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil {
			t.Fatal("malformed or oversized response accepted")
		}
	}
	if _, e := c.json(context.Background(), "POST", "/probe", nil, map[string]any{"bad": make(chan int)}, &out); e == nil {
		t.Fatal("non-JSON input accepted")
	}
	if _, e := c.json(context.Background(), "POST", "/probe", nil, map[string]string{"large": strings.Repeat("x", maxRequest)}, &out); e == nil {
		t.Fatal("oversized JSON accepted")
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("test-secret in transport error")
}
func TestDeploymentAndCancellation(t *testing.T) {
	for _, base := range []string{"http://public.example", "https://u:p@example.com", "https://example.com?key=secret", "https://example.com/#fragment", ""} {
		if validBase(base) {
			t.Fatalf("unsafe deployment URL accepted: %s", base)
		}
	}
	c := newClient(testCred)
	c.base = "https://service.example"
	c.http = &http.Client{Transport: errorTransport{}}
	var out map[string]any
	if _, e := c.json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal("transport error leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.json(ctx, "GET", "/probe", nil, nil, &out); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation lost: %v", e)
	}
	for _, path := range []string{"//evil.example", "/x?key=secret", "relative"} {
		if _, _, e := c.request(context.Background(), "GET", path, nil, nil, ""); e == nil {
			t.Fatal("unsafe path accepted")
		}
	}
	c.cred = Cred{}
	if _, e := c.json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil {
		t.Fatal("missing credential accepted")
	}
	if retryAfter("garbage") != time.Minute || retryAfter("0") != time.Minute || retryAfter(time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat)) < 119*time.Second {
		t.Fatal("bad Retry-After handling")
	}
}

type testCtx struct {
	context.Context
	cred     map[string]string
	file     []byte
	fetchErr error
}

func (c *testCtx) Credential() map[string]string      { return c.cred }
func (c *testCtx) Fetch(*plugin.File) ([]byte, error) { return c.file, c.fetchErr }
func (c *testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c *testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(op contract.Operation, fn plugin.Invoke) { h.calls[op.ID] = fn }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func TestGeneratedHealthAndRegistration(t *testing.T) {
	status := 200
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); reply(w, `{"id":7}`) })
	t.Setenv("WORDPRESS_SITE_URL", c.base)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	ctx := &testCtx{Context: context.Background(), cred: map[string]string{"username": "editor", "application_password": "test-secret"}}
	sink := &testSink{}
	if e := h.calls["health_check"](ctx, json.RawMessage(`{}`), sink); e != nil || sink.vars["ok"] != true {
		t.Fatalf("health %v %+v", e, sink.vars)
	}
	status = 401
	sink = &testSink{}
	if e := h.calls["health_check"](ctx, json.RawMessage(`{}`), sink); e != nil || sink.vars["ok"] != false || sink.vars["message"] == "" {
		t.Fatalf("failed health %v %+v", e, sink.vars)
	}
	p := sokel.New(sokel.Config{Name: "wordpress"})
	register(p)
}
