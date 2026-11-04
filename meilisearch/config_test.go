package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestConfiguredURLAndOptionalKey(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:6333", "http://localhost:7700/prefix/", "http://[::1]:6333", "https://search.internal/proxy/search"} {
		c := fresh()
		c.baseURL = base
		c.cred.APIKey = ""
		c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "" || r.Header.Get("api-key") != "" {
				t.Fatal("empty key injected")
			}
			if r.URL.String() != strings.TrimRight(base, "/")+"/probe" {
				t.Fatal(r.URL)
			}
			return httpResponse(200, `{}`), nil
		})
		if _, e := c.request(context.Background(), "GET", "/probe", nil, nil, 200); e != nil {
			t.Fatal(base, e)
		}
	}
	for _, base := range []string{"", "relative", "ftp://localhost/x", "http://user:pass@localhost", "http://localhost/?key=secret-test", "http://localhost/#secret-test", "http://localhost/a/../b", "http://localhost/a/./b", "http://localhost/a//b", "http://localhost/%2e%2e/x", "http://localhost:99999", "http://localhost:0", "http://localhost/x\\y", "http://localhost/x y"} {
		c := fresh()
		c.baseURL = base
		c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid URL sent"); return nil, nil })
		if _, e := c.request(context.Background(), "GET", "/probe", nil, nil, 200); e == nil || strings.Contains(e.Error(), "secret-test") {
			t.Fatal(base, e)
		}
	}
	c := fresh()
	c.cred.APIKey = "bad\nkey"
	if _, e := c.authorization(); e == nil {
		t.Fatal("bad key")
	}
}
