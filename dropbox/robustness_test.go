package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testClient() *client { return newClient(Cred{AccessToken: "test-secret"}) }
func TestTransportBoundaries(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 429, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			mockTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				res := response(status, `{"error":"test-secret private-url"}`)
				res.Header.Set("Retry-After", "123")
				res.Header.Set("Location", "https://evil.example/path")
				return res, nil
			})
			var out any
			e := testClient().json(context.Background(), "POST", "/probe", nil, map[string]string{"x": "y"}, &out)
			if e == nil || strings.Contains(e.Error(), "test-secret") || strings.Contains(e.Error(), "private-url") || calls != 1 {
				t.Fatalf("unsafe or retried error %v calls=%d", e, calls)
			}
			if status == 429 && !strings.Contains(e.Error(), "123") {
				t.Fatal("Retry-After lost")
			}
		})
	}
	for _, body := range []string{`bad`, `{} {}`, strings.Repeat("x", maxJSON+1)} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			mockTransport(t, func(*http.Request) (*http.Response, error) { return response(200, body), nil })
			var out any
			if e := testClient().json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil {
				t.Fatal("accepted malformed or oversized response")
			}
		})
	}
	t.Run("wrong media type", func(t *testing.T) {
		mockTransport(t, func(*http.Request) (*http.Response, error) {
			r := response(200, `{}`)
			r.Header.Set("Content-Type", "text/html")
			return r, nil
		})
		var out any
		if testClient().json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
			t.Fatal("accepted non-JSON")
		}
	})
	t.Run("transport redaction", func(t *testing.T) {
		mockTransport(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret private-url") })
		var out any
		e := testClient().json(context.Background(), "GET", "/probe", nil, nil, &out)
		if e == nil || strings.Contains(e.Error(), "test-secret") || strings.Contains(e.Error(), "private-url") {
			t.Fatal(e)
		}
	})
	t.Run("cancel and timeout", func(t *testing.T) {
		mockTransport(t, func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var out any
		if e := testClient().json(ctx, "GET", "/probe", nil, nil, &out); !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		c := testClient()
		c.http = &http.Client{Timeout: time.Millisecond}
		if e := c.json(context.Background(), "GET", "/probe", nil, nil, &out); e == nil {
			t.Fatal("missing timeout")
		}
	})
}
func TestConfigurationAndCredentialValidation(t *testing.T) {
	for _, s := range []string{"", "http://public.example", "https://u:p@example.com", "https://example.com?token=secret", "https://example.com/#fragment"} {
		if validBase(s) {
			t.Fatalf("unsafe base %s", s)
		}
	}
	for _, s := range []string{"https://service.example", "http://127.0.0.1:1", "http://localhost:2"} {
		if !validBase(s) {
			t.Fatal("valid base rejected")
		}
	}
	c := testClient()
	for _, path := range []string{"relative", "//evil.example", "/x?key=secret", "/x#f"} {
		if _, e := c.endpoint(c.base, path, nil); e == nil {
			t.Fatal("bad path accepted")
		}
	}
	c.base = "http://public.example"
	var out any
	if c.json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
		t.Fatal("unsafe base sent")
	}
	c = testClient()
	for _, token := range []string{"", "bad\nheader"} {
		c.cred.AccessToken = token
		if c.json(context.Background(), "GET", "/probe", nil, nil, &out) == nil {
			t.Fatal("bad credential accepted")
		}
	}
	if retryDelay("invalid") != 60 || retryDelay("0") != 60 {
		t.Fatal("bad default retry delay")
	}
	if fileMIME("bad\r\nheader") != "application/octet-stream" || fileMIME("text/plain; charset=utf-8") != "text/plain" {
		t.Fatal("bad MIME handling")
	}
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "x\n", strings.Repeat("x", 256)} {
		if validName(name) {
			t.Fatal("unsafe filename")
		}
	}
	if !validName("图😀.txt") {
		t.Fatal("Unicode filename rejected")
	}
	if c.json(context.Background(), "POST", "/probe", nil, map[string]any{"x": make(chan int)}, &out) == nil {
		t.Fatal("invalid JSON accepted")
	}
	if c.json(context.Background(), "POST", "/probe", nil, strings.Repeat("x", maxRequest), &out) == nil {
		t.Fatal("large request accepted")
	}
	if _, e := c.do(context.Background(), "POST", c.base, make([]byte, maxFile+(1<<20)+1), nil, true, time.Second); e == nil {
		t.Fatal("large body accepted")
	}
	if _, e := bounded(errorReader{}, 10); e == nil {
		t.Fatal("read error lost")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestHealthFailuresAndRegistration(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			mockTransport(t, func(*http.Request) (*http.Response, error) { return response(status, `{}`), nil })
			ctx := &testCtx{Context: context.Background()}
			out, e := invoke(t, "health_check", `{}`, ctx)
			if e != nil || out["ok"] != false || out["message"] == "" {
				t.Fatalf("false success or handler error %+v %v", out, e)
			}
		})
	}
	p := sokel.New(sokel.Config{Name: "dropbox"})
	register(p)
}
