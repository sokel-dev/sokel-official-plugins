package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTransportBoundaries(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 429, 500} {
		a := newAPI("test-secret")
		calls := 0
		a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://evil.example"}}, Body: io.NopCloser(strings.NewReader("test-secret")), Request: r}, nil
		})
		var out any
		e := a.json(context.Background(), "POST", "/boundary", nil, map[string]any{"x": false}, &out)
		if e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
			t.Fatal(status, e, calls)
		}
	}
	for _, raw := range []string{"{broken", "{}{}", strings.Repeat(" ", int(jsonLimit)+1)} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		var out any
		if e := a.json(context.Background(), "GET", "/boundary", nil, nil, &out); e == nil {
			t.Fatal("bad response accepted")
		}
	}
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid local request sent"); return "{}" })
	var out any
	for _, path := range []string{"//evil.example", "/x?secret=y", "/x\n"} {
		if e := a.json(context.Background(), "GET", path, nil, nil, &out); e == nil {
			t.Fatal(path)
		}
	}
	if e := a.json(context.Background(), "POST", "/boundary", nil, map[string]any{"oversize": strings.Repeat("a", int(jsonLimit))}, &out); e == nil {
		t.Fatal("oversize request accepted")
	}
	a.token = ""
	if e := a.json(context.Background(), "GET", "/boundary", nil, nil, &out); e == nil {
		t.Fatal("empty token accepted")
	}
	a = newAPI("test-secret")
	a.client.Transport = transport(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("test-secret") })
	if e := a.json(context.Background(), "GET", "/boundary", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if e := a.json(c, "GET", "/boundary", nil, nil, &out); e != context.Canceled {
		t.Fatal(e)
	}
}
func TestRedirectNeverForwardsCredential(t *testing.T) {
	calls := 0
	a := newAPI("test-secret")
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	var out any
	if e := a.json(context.Background(), "GET", "/boundary", nil, nil, &out); e == nil || calls != 1 {
		t.Fatal(e, calls)
	}
}
