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
	"net/url"
	"strings"
	"testing"
	"time"
)

type testCtx struct {
	context.Context
	cred map[string]string
}

func (c *testCtx) Credential() map[string]string    { return c.cred }
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unused") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(op contract.Operation, fn plugin.Invoke) { h.calls[op.ID] = fn }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func invoke(t *testing.T, c *client, op, body string) (map[string]any, error) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = c.http.Transport
	defer func() { http.DefaultTransport = old }()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	if len(h.calls) != 8 {
		t.Fatal("operation count")
	}
	sink := &testSink{}
	e := h.calls[op](&testCtx{Context: context.Background(), cred: map[string]string{"api_key": "fixture-secret"}}, json.RawMessage(body), sink)
	return sink.vars, e
}
func TestTransportFailureBoundaries(t *testing.T) {
	for _, status := range []int{201, 202, 204, 301, 302, 307, 308, 400, 401, 403, 404, 409, 422, 425, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := fixture(t, func(r *http.Request) (int, string) {
				calls++
				if r.GetBody != nil {
					t.Fatal("write request can be replayed")
				}
				return status, `{"message":"fixture-secret secret personal data"}`
			})
			_, e := c.request(context.Background(), "POST", "/v3/account", nil, map[string]any{"x": 1}, 200)
			var ae *apiError
			if e == nil || strings.Contains(e.Error(), "fixture-secret") || strings.Contains(e.Error(), "personal") || calls != 1 || !errors.As(e, &ae) {
				t.Fatal(e, calls)
			}
			if status == 429 && ae.RetryAfter != 17*time.Second {
				t.Fatal(ae.RetryAfter)
			}
		})
	}
	c := fixture(t, func(*http.Request) (int, string) { return 200, `{}` })
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "request", URL: "https://example.com/?token=fixture-secret", Err: errors.New("fixture-secret")}
	})
	if _, e := c.request(context.Background(), "GET", "/v3/account", nil, nil, 200); e == nil || strings.Contains(e.Error(), "fixture-secret") {
		t.Fatal(e)
	}
}
func TestRedirectNeverFollowed(t *testing.T) {
	calls := 0
	c := newClient(&Cred{APIKey: "fixture-secret"})
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example/?secret=fixture-secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, e := c.request(context.Background(), "GET", "/v3/account", nil, nil, 200)
	if e == nil || calls != 1 || strings.Contains(e.Error(), "evil") {
		t.Fatal(e, calls)
	}
}
func TestMalformedResponsesAndLimits(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `{} {}`, `{"code":"invalid_parameter"}`, `{"status":400}`} {
		if strings.Contains(raw, `"status"`) {
			continue
		}
		c := fixture(t, func(*http.Request) (int, string) { return 200, raw })
		if _, e := c.request(context.Background(), "GET", "/v3/account", nil, nil, 200); e == nil {
			t.Fatal(raw)
		}
	}
	c := fixture(t, func(*http.Request) (int, string) { return 200, strings.Repeat(" ", maxResponse+1) })
	if _, e := c.request(context.Background(), "GET", "/v3/account", nil, nil, 200); e == nil {
		t.Fatal("large response")
	}
	c = fixture(t, func(*http.Request) (int, string) { t.Fatal("invalid request sent"); return 200, "{}" })
	if _, e := c.request(context.Background(), "POST", "/v3/account", nil, map[string]string{"body": strings.Repeat("x", maxRequest)}, 200); e == nil {
		t.Fatal("large request")
	}
	if _, e := c.request(context.Background(), "GET", "//evil.example", nil, nil, 200); e == nil {
		t.Fatal("invalid path")
	}
	if _, e := c.request(context.Background(), "POST", "/v3/account", nil, map[string]any{"bad": make(chan int)}, 200); e == nil {
		t.Fatal("invalid JSON")
	}
	for _, key := range []string{"", "x\r\nAuthorization: stolen", " x", "x\x00"} {
		c := newClient(&Cred{APIKey: key})
		if _, e := c.request(context.Background(), "GET", "/v3/account", nil, nil, 200); e == nil {
			t.Fatal("bad key accepted")
		}
	}
	p := sokel.New(sokel.Config{Name: "brevo"})
	register(p)
}
func TestPaginationValidation(t *testing.T) {
	for _, v := range []struct{ total, n, offset, limit int }{{-1, 0, 0, 50}, {3, 0, 0, 50}, {1, 2, 0, 50}, {1, 1, 1, 50}, {10, 2, 0, 1}, {10000002, 1, 10000000, 50}} {
		if _, _, e := pageResult(&v.total, v.n, v.offset, v.limit); e == nil {
			t.Fatal(v)
		}
	}
	if _, _, e := pageResult(nil, 0, 0, 50); e == nil {
		t.Fatal("missing total")
	}
	total := 5
	more, next, e := pageResult(&total, 2, 0, 50)
	if e != nil || !more || next != 2 {
		t.Fatal(more, next, e)
	}
	for _, v := range []int64{0, -1, 1001} {
		if _, _, e := pagination(&v, nil, 1000); e == nil {
			t.Fatal(v)
		}
	}
	neg := int64(-1)
	if _, _, e := pagination(nil, &neg, 1000); e == nil {
		t.Fatal("negative offset")
	}
}
