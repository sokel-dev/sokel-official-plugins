package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type errorTransport struct{ err error }

func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }
func TestStatusTransportAndCredentialErrorsAreSanitized(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 404, 409, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "https://attacker.invalid")
				w.WriteHeader(status)
				reply(w, "test-secret upstream error")
			})
			o, e := a.health_check(context.Background(), &HealthCheckIn{})
			if e != nil || o.OK || strings.Contains(o.Message, "test-secret") || calls != 1 {
				t.Fatalf("%v %v calls=%d", o, e, calls)
			}
		})
	}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("invalid credential reached network") })
	for _, token := range []string{"", " ", "test-secret\nheader"} {
		a.token = token
		o, e := a.health_check(context.Background(), &HealthCheckIn{})
		if e != nil || o.OK || strings.Contains(o.Message, "test-secret") {
			t.Fatal(o, e)
		}
	}
	a.token = "test-secret"
	a.client.Transport = errorTransport{errors.New("test-secret at https://private.invalid")}
	o, e := a.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || o.OK || strings.Contains(o.Message, "test-secret") || strings.Contains(o.Message, "private") {
		t.Fatal(o, e)
	}
}
func TestSizeMalformedShapeAndCancellation(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{`{broken secret`, `{}`, `null`} {
		a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, raw) })
		o, e := a.health_check(ctx, &HealthCheckIn{})
		if e != nil || o.OK {
			t.Fatal("bad identity accepted", o, e)
		}
	}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, strings.Repeat("x", jsonLimit+1)) })
	if _, e := a.request(ctx, "GET", "/v1.0/me", nil, nil, 200); e == nil || !strings.Contains(e.Error(), "16 MiB") {
		t.Fatal(e)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("large request reached network") })
	if _, e := a.request(ctx, "POST", "/v1.0/me", nil, map[string]string{"body": strings.Repeat("x", requestLimit)}, 200); e == nil {
		t.Fatal("large request accepted")
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	a.client.Transport = errorTransport{context.Canceled}
	if _, e := a.request(c, "GET", "/v1.0/me", nil, nil, 200); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		d, ok := r.Context().Deadline()
		if !ok || time.Until(d) > 56*time.Second {
			t.Fatal("HTTP timeout absent")
		}
		reply(w, `{"id":"viewer"}`)
	})
	if _, e := a.delegated(ctx); e != nil {
		t.Fatal(e)
	}
}
func TestNextLinksCannotChangeOriginOrResource(t *testing.T) {
	path := "/v1.0/users/viewer/messages"
	for _, next := range []string{"https://evil.invalid/v1.0/users/viewer/messages?$skip=1", "http://graph.microsoft.com/v1.0/users/viewer/messages?$skip=1", "https://test-secret@graph.microsoft.com/v1.0/users/viewer/messages?$skip=1", "https://graph.microsoft.com:443/v1.0/users/viewer/messages?$skip=1", "https://graph.microsoft.com/v1.0/users/other/messages?$skip=1", "https://graph.microsoft.com/beta/users/viewer/messages?$skip=1", "https://graph.microsoft.com/v1.0/users/viewer/messages?$skip=1#x", "https://graph.microsoft.com/v1.0/users/viewer/messages", "/v1.0/users/viewer/messages?$skip=1"} {
		if e := validateNext(path, next); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(next, e)
		}
	}
	exact := "https://graph.microsoft.com" + path + "?$skiptoken=A%2BB%2F%3D&$select=id"
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.String() != exact {
			t.Fatalf("nextLink mutated: %s", r.URL)
		}
		reply(w, `{"value":[]}`)
	})
	var values []map[string]any
	if next, e := a.page(context.Background(), path, url.Values{"ignored": {"x"}}, exact, &values); e != nil || next != "" || calls != 1 || values == nil {
		t.Fatal(next, e, calls)
	}
	for _, raw := range []string{`{}`, `{"value":null}`, `{"value":{}}`, `{"value":[1]}`, `{"value":[],"@odata.nextLink":"https://evil.invalid/x"}`} {
		a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, raw) })
		if _, e := a.page(context.Background(), path, nil, "", &values); e == nil {
			t.Fatal("invalid page accepted", raw)
		}
	}
	a = fakeAPI(func(w http.ResponseWriter, r *http.Request) { reply(w, `{"value":[],"@odata.nextLink":"`+exact+`"}`) })
	if _, e := a.page(context.Background(), path, nil, exact, &values); e == nil {
		t.Fatal("nonadvancing page accepted")
	}
}
func TestInputValidationDoesNotUseUntrustedPaths(t *testing.T) {
	for _, id := range []string{"", "..", "../other", "x/y", "x?query", "x%2fy", "x\nnext"} {
		if _, e := segment(id, "id"); e == nil {
			t.Fatal("invalid ID", id)
		}
	}
	for _, n := range []int{-1, 51} {
		if _, e := pageSize(n); e == nil {
			t.Fatal("invalid page size")
		}
	}
	if _, e := contentType("markdown"); e == nil {
		t.Fatal("invalid content type")
	}
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) { t.Fatal("bad path reached network") })
	if _, e := a.request(context.Background(), "GET", "//evil.invalid", nil, nil, 200); e == nil {
		t.Fatal("invalid path")
	}
}
