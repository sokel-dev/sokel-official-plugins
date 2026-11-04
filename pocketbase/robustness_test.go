package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTransportBoundaries(t *testing.T) {
	for _, code := range []int{200, 201, 204, 301, 302, 307, 400, 401, 403, 404, 409, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c := fresh()
			calls := 0
			c.http = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				calls++
				res := httpResponse(code, `{"error":"secret-test secret-url"}`)
				res.Header.Set("Location", "https://evil.example/?credential=secret-test")
				res.Header.Set("Retry-After", "123")
				return res, nil
			})}
			var out any
			e := c.json(context.Background(), "POST", "/probe", nil, map[string]any{"x": false}, &out)
			if code == 200 {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil || strings.Contains(e.Error(), "secret-test") || strings.Contains(e.Error(), "secret-url") {
				t.Fatalf("unsafe error %v", e)
			}
			if calls != 1 {
				t.Fatal("request retried or redirect followed")
			}
			if code == 429 && !strings.Contains(e.Error(), "123") {
				t.Fatal("missing retry delay")
			}
		})
	}
	for _, body := range []string{"", `{`, `{} {}`, strings.Repeat("x", maxResponse+1)} {
		c := fresh()
		withReply(c, 200, body)
		var out any
		if c.json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
			t.Fatal("malformed or oversized response accepted")
		}
	}
	c := fresh()
	c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		r := httpResponse(200, `{}`)
		r.Header.Set("Content-Type", "text/html")
		return r, nil
	})}
	var out any
	if c.json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
		t.Fatal("wrong media accepted")
	}
	c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-test secret-url") })}
	if e := c.json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "secret-test") {
		t.Fatal("unsafe transport error", e)
	}
	c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		r := httpResponse(200, "")
		r.Body = io.NopCloser(errorReader{})
		return r, nil
	})}
	if c.json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
		t.Fatal("read error ignored")
	}
	c = fresh()
	c.http = &http.Client{Timeout: time.Millisecond, Transport: transport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	if c.json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
		t.Fatal("timeout ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := c.json(ctx, "GET", "/probe", nil, nil, &out); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if e := c.json(ctx, "GET", "/probe", nil, nil, &out); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestValidationBeforeTransport(t *testing.T) {
	c := fresh()
	c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network call"); return nil, nil })}
	var out any
	for _, p := range []string{"relative", "//evil.example", "/x?token=y", "/x#frag", "/x/../secret", "/x/./secret", "/x\\y", "/x\r"} {
		if c.json(context.Background(), "GET", p, nil, nil, &out) == nil {
			t.Fatal("unsafe path", p)
		}
	}
	if c.json(context.Background(), "POST", "/probe", nil, map[string]any{"x": make(chan int)}, &out) == nil {
		t.Fatal("invalid JSON accepted")
	}
	if c.json(context.Background(), "POST", "/probe", nil, strings.Repeat("x", maxRequest), &out) == nil {
		t.Fatal("oversized request accepted")
	}
	for _, v := range []string{"", "a b", "a\n", "a\x00", "密钥", strings.Repeat("x", 16385)} {
		if safeToken(v) {
			t.Fatal("bad token accepted")
		}
	}
	if !safeToken("valid+/=") {
		t.Fatal("valid token rejected")
	}
	for _, v := range []string{"bad", "0", "-1", "9999999999", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)} {
		if retryAfter(v) != 60 {
			t.Fatal("bad retry fallback")
		}
	}
	if n := retryAfter(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)); n < 58 || n > 61 {
		t.Fatal(n)
	}
	if requireKeys([]byte(`{"a":false}`), "a") != nil || requireKeys([]byte(`{}`), "a") == nil || requireKeys([]byte(`{"a":null}`), "a") == nil || requireKeys([]byte(`bad`), "a") == nil {
		t.Fatal("required metadata validation")
	}
}

func (c *client) json(ctx context.Context, method, path string, q url.Values, input, out any) error {
	b, e := c.request(ctx, method, path, q, input, 200)
	if e != nil {
		return e
	}
	return decodeJSON(b, out)
}
func TestHealthFailures(t *testing.T) {
	t.Setenv("SOKEL_POCKETBASE_URL", "")
	for _, status := range []int{200, 401, 403} {
		c := fresh()
		withReply(c, status, `{}`)
		o, e := c.health_check(context.Background(), &HealthCheckIn{})
		if e != nil || o == nil || o.OK || o.Message == "" {
			t.Fatal("false health success", o, e)
		}
	}
	o, e := newClient(Cred{}).health_check(context.Background(), &HealthCheckIn{})
	if e != nil || o.OK {
		t.Fatal(o, e)
	}
}
func TestRegisteredFailures(t *testing.T) {
	t.Setenv("SOKEL_POCKETBASE_URL", "http://service.internal/prefix")
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = transport(func(*http.Request) (*http.Response, error) {
		return httpResponse(401, `{"detail":"synthetic-test-token"}`), nil
	})
	for _, f := range fixtures(t) {
		s := &testSink{}
		e := h.calls[f.Op](&testCtx{context.Background()}, f.Input, s)
		if f.Op == "health_check" {
			if e != nil || s.vars["ok"] != false {
				t.Fatal(f.Op, e, s.vars)
			}
		} else if e == nil {
			t.Fatal("401 did not fail", f.Op)
		}
		if e != nil && strings.Contains(e.Error(), "synthetic-test-token") {
			t.Fatal("credential leaked")
		}
		if e = h.calls[f.Op](&testCtx{context.Background()}, json.RawMessage(`{`), s); e == nil {
			t.Fatal("malformed input accepted", f.Op)
		}
	}
}
func TestMalformedBusinessResponses(t *testing.T) {
	t.Setenv("SOKEL_POCKETBASE_URL", "http://service.internal/prefix")
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for _, f := range fixtures(t) {
		for _, body := range []string{`{}`, `null`, `{"error":"ERROR_INVALID_TOKEN","detail":"synthetic-test-token"}`} {
			t.Run(f.Op+body, func(t *testing.T) {
				http.DefaultTransport = transport(func(*http.Request) (*http.Response, error) { return httpResponse(f.Status, body), nil })
				s := &testSink{}
				e := h.calls[f.Op](&testCtx{context.Background()}, f.Input, s)
				if f.Op == "health_check" {
					if e != nil || s.vars["ok"] != false {
						t.Fatal(e, s.vars)
					}
				} else if e == nil {
					t.Fatal("malformed response accepted", s.vars)
				}
				if e != nil && strings.Contains(e.Error(), "synthetic-test-token") {
					t.Fatal("raw error leaked")
				}
			})
		}
	}
}
