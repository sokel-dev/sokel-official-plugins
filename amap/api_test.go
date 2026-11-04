package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTransportDoesNotLeakOrRetry(t *testing.T) {
	for _, status := range []int{301, 302, 307, 401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a := newAPI(Cred{APIKey: "test-secret"})
			calls := 0
			a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://example.com/?key=test-secret"}}, Body: io.NopCloser(strings.NewReader("test-secret")), Request: r}, nil
			})
			_, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "test"})
			if e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
				t.Fatal(e, calls)
			}
		})
	}
	a := newAPI(Cred{APIKey: "test-secret"})
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("failed URL %s test-secret", r.URL)
	})
	_, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "test"})
	if e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
}
func TestInvalidCredentialsAndCancelledContext(t *testing.T) {
	a := newAPI(Cred{})
	calls := 0
	a.client.Transport = transport(func(r *http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("test-secret") })
	if _, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "test"}); e == nil || calls != 0 {
		t.Fatal(e, calls)
	}
	a.cred = Cred{APIKey: "test-secret"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.GeocodeAddress(ctx, &GeocodeAddressIn{Address: "test"}); e != context.Canceled {
		t.Fatal(e)
	}
}
func TestResponseBoundsAndMalformedJSON(t *testing.T) {
	for _, raw := range []string{strings.Repeat(" ", int(jsonLimit)+1), `{} {}`, `<html>test-secret</html>`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "test"}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(e)
		}
	}
}
