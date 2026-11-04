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
	"strings"
	"testing"
	"time"
)

type testCtx struct {
	context.Context
	cred map[string]string
}

func (c *testCtx) Credential() map[string]string      { return c.cred }
func (c *testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unused") }
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
func invoke(t *testing.T, c *client, op, body string) map[string]any {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = c.http.Transport
	defer func() { http.DefaultTransport = old }()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	if len(h.calls) != 7 {
		t.Fatal("operation count")
	}
	sink := &testSink{}
	ctx := &testCtx{Context: context.Background(), cred: map[string]string{"access_token": "fixture-token"}}
	if e := h.calls[op](ctx, json.RawMessage(body), sink); e != nil {
		t.Fatal(e)
	}
	return sink.vars
}
func TestHTTPFailuresNoLeaksNoRetries(t *testing.T) {
	for _, s := range []int{201, 202, 301, 302, 307, 308, 400, 401, 402, 403, 404, 409, 422, 429, 500} {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			calls := 0
			c := fixture(t, func(r *http.Request) (int, string) { calls++; return s, `{"error":"fixture-token private data"}` })
			_, e := c.request(context.Background(), "POST", "/v7/developer/applications/current", nil, map[string]any{"x": false})
			if e == nil || strings.Contains(e.Error(), "fixture-token") || calls != 1 {
				t.Fatalf("error %v calls %d", e, calls)
			}
			var ae *apiError
			if !errors.As(e, &ae) || ae.Status != s {
				t.Fatal(e)
			}
			if s == 429 && ae.RetryAfter != 17*time.Second {
				t.Fatal("retry-after")
			}
		})
	}
}
func TestRedirectNeverForwardsCredential(t *testing.T) {
	c := newClient(Cred{AccessToken: "fixture-token"})
	calls := 0
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, e := c.request(context.Background(), "GET", "/v7/developer/applications/current", nil, nil); e == nil || calls != 1 {
		t.Fatalf("redirect followed: %d %v", calls, e)
	}
}
func TestHTTPBoundariesAndSafeErrors(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) { return 200, strings.Repeat("x", maxJSON+1) })
	if _, e := c.request(context.Background(), "GET", "/v7/developer/applications/current", nil, nil); e == nil {
		t.Fatal("unbounded response")
	}
	if _, e := c.request(context.Background(), "POST", "/v7/developer/applications/current", nil, map[string]any{"x": strings.Repeat("x", 1<<20)}); e == nil {
		t.Fatal("unbounded request")
	}
	if _, e := c.request(context.Background(), "POST", "/v7/developer/applications/current", nil, map[string]any{"x": func() {}}); e == nil {
		t.Fatal("invalid JSON request")
	}
	for _, p := range []string{"https://evil.example/", "/wrong", "/v7/developer/applications/current/../secret", "/v7/developer/applications/current?x=1", "/v7/developer/applications/current%2fother"} {
		if _, e := c.request(context.Background(), "GET", p, nil, nil); e == nil {
			t.Fatal("unsafe path")
		}
	}
	c.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture-token sensitive query") })
	if _, e := c.request(context.Background(), "POST", "/v7/developer/applications/current", nil, nil); e == nil || strings.Contains(e.Error(), "fixture-token") {
		t.Fatal("transport leak")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.request(ctx, "GET", "/v7/developer/applications/current", nil, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	for _, b := range []string{"", "null", "{} {}", "{broken"} {
		var v map[string]any
		if e := decode([]byte(b), &v); e == nil {
			t.Fatal("invalid response")
		}
	}
	for _, s := range []string{"", "secret\r\nInjected: yes", "a b"} {
		c.cred = Cred{AccessToken: "fixture-token"}
		c.cred.AccessToken = s
		if _, e := c.request(context.Background(), "GET", "/v7/developer/applications/current", nil, nil); e == nil {
			t.Fatal("invalid credential")
		}
	}
	if retryAfter("invalid") != time.Minute || retryAfter("-1") != time.Minute || retryAfter(time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat)) <= 0 {
		t.Fatal("retry parsing")
	}
	p := sokel.New(sokel.Config{Name: "fixture"})
	register(p)
}
