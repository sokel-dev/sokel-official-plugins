package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestClientFailuresAreBoundedAndSanitized(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		body   string
	}{{"denied", 401, `{"message":"test-secret-do-not-expose"}`}, {"forbidden", 403, "secret"}, {"quota", 429, "test-secret-do-not-expose"}, {"server", 500, "test-secret-do-not-expose"}, {"malformed", 200, "<html>"}, {"trailing", 200, `{} {}`}, {"envelope", 200, `{"error":"test-secret-do-not-expose"}`}, {"err", 200, `{"err":"test-secret-do-not-expose"}`}, {"code", 200, `{"code":124,"message":"test-secret-do-not-expose"}`}, {"size", 200, strings.Repeat("x", maxResponse+1)}} {
		t.Run(c.name, func(t *testing.T) {
			a, n := fakeAPI(t, "POST", "/probe", c.status, c.body, nil)
			_, e := a.call(context.Background(), "POST", "/probe", nil, struct{}{}, 200)
			if e == nil || strings.Contains(e.Error(), "test-secret-do-not-expose") || *n != 1 {
				t.Fatalf("unsafe error %v calls=%d", e, *n)
			}
		})
	}
}
func TestRedirectNeverForwardsCredential(t *testing.T) {
	a := newAPI(Cred{AccessToken: "test-secret-do-not-expose"})
	calls := 0
	a.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.invalid/steal"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, e := a.call(context.Background(), "GET", "/probe", nil, nil, 200)
	if e == nil || calls != 1 {
		t.Fatalf("followed redirect %v %d", e, calls)
	}
}
func TestLocalClientValidation(t *testing.T) {
	a := newAPI(Cred{AccessToken: "test-secret-do-not-expose"})
	a.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Error("unexpected request")
		return nil, errors.New("unused")
	})
	for _, p := range []string{"https://evil.invalid", "//evil.invalid/x?y=1", "/../x", "/x#fragment"} {
		if _, e := a.call(context.Background(), "GET", p, nil, nil, 200); e == nil {
			t.Fatal("route accepted")
		}
	}
	if _, e := a.call(context.Background(), "POST", "/probe", nil, make(chan int), 200); e == nil {
		t.Fatal("unsupported body")
	}
	if _, e := a.call(context.Background(), "POST", "/probe", nil, strings.Repeat("x", maxRequest+1), 200); e == nil {
		t.Fatal("unbounded request")
	}
	a.cred.AccessToken = "invalid\r\nheader"
	if _, e := a.call(context.Background(), "GET", "/probe", nil, nil, 200); e == nil {
		t.Fatal("credential accepted")
	}
}
func TestTransportErrorsDoNotEchoSecrets(t *testing.T) {
	a := newAPI(Cred{AccessToken: "test-secret-do-not-expose"})
	a.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("test-secret-do-not-expose") })
	for _, cancel := range []bool{false, true} {
		ctx, c := context.WithCancel(context.Background())
		if cancel {
			c()
		}
		_, e := a.call(ctx, "GET", "/probe", url.Values{}, nil, 200)
		c()
		if e == nil || strings.Contains(e.Error(), "test-secret-do-not-expose") {
			t.Fatal("unsafe transport error")
		}
	}
}
func TestPaginationBounds(t *testing.T) {
	for _, n := range []int{-1, 1001} {
		if _, e := pageQuery(n, 100, "", "cursor"); e == nil {
			t.Fatal("page size accepted")
		}
	}
	if _, e := pageQuery(10, 100, "\n", "cursor"); e == nil {
		t.Fatal("cursor accepted")
	}
	if _, e := pageQuery(10, 100, strings.Repeat("x", 4097), "cursor"); e == nil {
		t.Fatal("long cursor accepted")
	}
}
