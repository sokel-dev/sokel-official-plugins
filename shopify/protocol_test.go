package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/shopify/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Synthetic protocol fixtures derived from the linked official API documentation; not live captures.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(t *testing.T, f func(*http.Request) (int, string)) *api {
	t.Helper()
	a := newAPI(testCred())
	a.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		code, body := f(r)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return a
}
func bodyOf(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func testCred() Cred { return Cred{Shop: "example-shop", AccessToken: "test-secret"} }
func TestProtocol(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		calls++
		if r.URL.String() != "https://example-shop.myshopify.com/admin/api/2026-10/graphql.json" || r.Method != "POST" || r.Header.Get("X-Shopify-Access-Token") != "test-secret" {
			t.Fatal(r.URL, r.Method)
		}
		b := bodyOf(t, r)
		q := b["query"].(string)
		if strings.Contains(q, "productUpdate") {
			v := b["variables"].(map[string]any)["product"].(map[string]any)
			if v["status"] != "DRAFT" || v["id"] != "gid://shopify/Product/1" {
				t.Fatal(v)
			}
			return 200, `{"data":{"productUpdate":{"product":{"id":"gid://shopify/Product/1","title":"Product","status":"DRAFT"},"userErrors":[]}}}`
		}
		return 200, `{"data":{"shop":{"id":"gid://shopify/Shop/1","name":"Shop","myshopifyDomain":"example-shop.myshopify.com","currencyCode":"USD"}}}`
	})
	h, e := a.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || h == nil || !h.OK || calls != 1 {
		t.Fatalf("health: %#v %v calls=%d", h, e, calls)
	}
	status := "DRAFT"
	v, e := a.update_product(context.Background(), &UpdateProductIn{ProductID: "gid://shopify/Product/1", Changes: schema.ProductChanges{Status: &status}})
	if e != nil || v == nil || v.Product.Status != "DRAFT" {
		t.Fatalf("update: %#v %v", v, e)
	}
}
