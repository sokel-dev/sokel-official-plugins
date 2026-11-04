package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestServiceURLs(t *testing.T) {
	for _, base := range []string{"", "https://user:secret@host", "https://host?token=secret", "https://host#fragment", "file:///tmp/secret", "https://host/../x", "https://host/a/./b", "https://host/a%2fb", "http://host:0", "http://host:99999", "http://host/x\\y"} {
		c := newClient(base, Cred{})
		if _, e := c.endpoint("/route"); e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatalf("accepted or leaked %q: %v", base, e)
		}
	}
	for _, base := range []string{"http://127.0.0.1:9998", "http://tika:9998/prefix/", "https://docs.internal/sub/path", "http://[::1]:3000"} {
		c := newClient(base, Cred{})
		u, e := c.endpoint("/route")
		if e != nil || u != strings.TrimRight(base, "/")+"/route" {
			t.Fatalf("%q: %s %v", base, u, e)
		}
	}
}
func TestCredentialValidation(t *testing.T) {
	for _, cred := range []Cred{{Username: "u"}, {Password: "p"}, {Username: "u:p", Password: "p"}, {Username: "u", Password: "p\r\nleak"}} {
		c := newClient("http://host", cred)
		r, _ := http.NewRequest("GET", "http://host", nil)
		if c.auth(r) == nil {
			t.Fatal("bad credential")
		}
	}
	c := newClient("http://host", Cred{})
	r, _ := http.NewRequest("GET", "http://host", nil)
	if e := c.auth(r); e != nil || r.Header.Get("Authorization") != "" {
		t.Fatal("anonymous auth")
	}
}
func TestHTTPBoundaries(t *testing.T) {
	for _, status := range []int{201, 204, 301, 302, 307, 308, 400, 401, 403, 404, 413, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := testClient(func(r *http.Request) (*http.Response, error) {
				calls++
				res := response("text/plain", "secret upstream details")
				res.StatusCode = status
				res.Header.Set("Location", "https://outside.invalid/secret")
				res.Header.Set("Retry-After", "secret-token")
				return res, nil
			})
			_, _, e := c.request(context.Background(), "PUT", "/route", []byte("x"), nil, 100)
			if e == nil || strings.Contains(e.Error(), "secret") || calls != 1 {
				t.Fatalf("calls %d error %v", calls, e)
			}
		})
	}
	for _, bad := range []string{"length", "limit", "read"} {
		t.Run(bad, func(t *testing.T) {
			c := testClient(func(*http.Request) (*http.Response, error) {
				res := response("application/json", "abcd")
				switch bad {
				case "length":
					res.ContentLength = 2
				case "limit":
					res.ContentLength = -1
					res.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 101)))
				case "read":
					res.ContentLength = -1
					res.Body = io.NopCloser(badReader{})
				}
				return res, nil
			})
			if _, _, e := c.request(context.Background(), "GET", "/route", nil, nil, 100); e == nil {
				t.Fatal("bad response")
			}
		})
	}
	c := testClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("https://user:secret@internal/path")
	})
	if _, _, e := c.request(context.Background(), "GET", "/route", nil, nil, 100); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := c.request(canceled, "GET", "/route", nil, nil, 100); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errors.New("private read failure") }
func TestFileBoundaries(t *testing.T) {
	for _, f := range []*plugin.File{nil, {ID: "x", Name: "../file"}, {ID: "x", Name: "a", Size: -1}, {ID: "x", Name: "a", Size: maxInput + 1}, {URL: "https://external.invalid/file", Name: "a"}, {ID: "x", Name: "a\r\nheader"}} {
		if _, e := readFile(ctxFor("x"), f); e == nil {
			t.Fatal("bad file")
		}
	}
	ctx := ctxFor("x")
	ctx.failFetch = true
	if _, e := readFile(ctx, &plugin.File{ID: "x", Name: "a"}); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	if _, e := readFile(ctxFor("x"), &plugin.File{ID: "x", Name: "a", Size: 2}); e == nil {
		t.Fatal("size mismatch")
	}
	if _, e := readFile(ctxFor(strings.Repeat("x", maxInput+1)), &plugin.File{ID: "x", Name: "a"}); e == nil {
		t.Fatal("actual size cap")
	}
}
func TestJSONAndMediaTypes(t *testing.T) {
	for _, b := range []string{"", "{", "{} {}", string([]byte{'"', 255, '"'})} {
		var out any
		if decodeJSON([]byte(b), &out) == nil {
			t.Fatal("bad JSON")
		}
	}
	if mediaType(http.Header{"Content-Type": {"text/plain"}}, "application/json") == nil {
		t.Fatal("bad media type")
	}
	if retryDelay("2") != 2 || retryDelay("secret") != 60 {
		t.Fatal("retry header")
	}
}
