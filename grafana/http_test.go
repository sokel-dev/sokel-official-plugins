package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestEndpointAndCredentialBoundaries(t *testing.T) {
	for _, base := range []string{"", "https://u:secret@host", "https://host?secret=x", "https://host/#fragment", "ftp://host", "http://host/a/../b", "http://host/%2e%2e", "http://host:99999", "http://host\\other"} {
		c := newClient(base, Cred{Token: "secret"})
		if _, e := c.endpoint("/api/test", nil); e == nil {
			t.Fatalf("accepted unsafe base: %q", base)
		}
	}
	c := newClient("http://internal:8080/prefix/", Cred{Token: "secret"})
	u, e := c.endpoint("/api/test", url.Values{"q": {"a&b"}})
	if e != nil || u.String() != "http://internal:8080/prefix/api/test?q=a%26b" {
		t.Fatalf("prefix: %v %v", u, e)
	}
	for _, token := range []string{"", "x\nsecret", "white space"} {
		c := testClient(func(*http.Request) (*http.Response, error) { t.Fatal("invalid token made a request"); return nil, nil })
		c.cred.Token = token
		if _, _, e := c.request(context.Background(), "GET", "/api/test", nil, nil, "", 200, 100); e == nil {
			t.Fatal("accepted invalid token")
		}
	}
}
func TestHTTPFailureSafety(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308, 400, 401, 403, 404, 429, 500, 201} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c := testClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.GetBody != nil {
					t.Fatal("replay enabled")
				}
				s := response("application/json", `{"token":"secret","url":"private"}`)
				s.StatusCode = status
				s.Header.Set("Retry-After", "7")
				s.Header.Set("Location", "http://other.internal/secret")
				return s, nil
			})
			_, _, e := c.request(context.Background(), "POST", "/api/test", nil, []byte(`{}`), "application/json", 200, 1000)
			if e == nil || strings.Contains(e.Error(), "secret") || strings.Contains(e.Error(), "private") || calls != 1 {
				t.Fatalf("unsafe error or retry: %v calls=%d", e, calls)
			}
		})
	}
	c := testClient(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("https://host/private?token=secret")
	})
	if _, _, e := c.request(context.Background(), "GET", "/api/test", nil, nil, "", 200, 100); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := c.request(canceled, "GET", "/api/test", nil, nil, "", 200, 100); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("private body") }
func (brokenReader) Close() error             { return nil }
func TestResponseBoundsAndJSON(t *testing.T) {
	for _, kind := range []string{"known-big", "stream-big", "truncated", "read-error"} {
		t.Run(kind, func(t *testing.T) {
			c := testClient(func(*http.Request) (*http.Response, error) {
				s := response("application/json", "12345")
				switch kind {
				case "known-big":
					s.ContentLength = 999
				case "stream-big":
					s.ContentLength = -1
				case "truncated":
					s.ContentLength = 6
				case "read-error":
					s.Body = brokenReader{}
				}
				return s, nil
			})
			cap := int64(4)
			if kind == "truncated" || kind == "read-error" {
				cap = 10
			}
			if _, _, e := c.request(context.Background(), "GET", "/api/test", nil, nil, "", 200, cap); e == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
	for _, b := range []string{`{`, "\xff", `{} {}`} {
		var out map[string]any
		if decodeJSON([]byte(b), &out) == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	c := testClient(func(*http.Request) (*http.Response, error) { return response("text/html", `<html>Login</html>`), nil })
	var out any
	if _, e := c.json(context.Background(), "GET", "/api/test", nil, nil, 200, &out); e == nil {
		t.Fatal("HTML login accepted")
	}
	for _, s := range []string{"0", "01", "-1", "1.2", "9223372036854775808", "1/2", ""} {
		if _, e := positiveID(s); e == nil {
			t.Fatalf("bad id accepted %q", s)
		}
	}
	if n, e := positiveID("9007199254740993"); e != nil || n != 9007199254740993 {
		t.Fatal(n, e)
	}
	if retryDelay("3") != 3 || retryDelay("-2") != 60 || retryDelay("garbage") != 60 {
		t.Fatal("retry delay")
	}
	if n := retryDelay(time.Now().Add(20 * time.Second).UTC().Format(http.TimeFormat)); n < 18 || n > 21 {
		t.Fatal(n)
	}
}
