package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/shopify/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type captureHost struct {
	ops    map[string]plugin.Invoke
	fields []contract.Field
}

func (h *captureHost) Register(op contract.Operation, fn plugin.Invoke) { h.ops[op.ID] = fn }
func (h *captureHost) SetCredentialContract(f []contract.Field)         { h.fields = f }
func (*captureHost) SetAuthFlow(contract.AuthMeta, plugin.AuthHandlers) {}
func (*captureHost) DeclareEvent(contract.Event)                        {}
func (*captureHost) DeclareEventsCommon([]contract.Field, []string)     {}

type captureSink struct{ v any }

func (s *captureSink) Vars(v any) { s.v = v }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}

type testCtx struct{ context.Context }

func (*testCtx) Credential() map[string]string                       { return nil }
func (*testCtx) Fetch(*plugin.File) ([]byte, error)                  { panic("unexpected fetch") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) { panic("unexpected upload") }
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("unexpected upload")
}
func dispatchAll(t *testing.T, a *api, inputs map[string]string, check func(string, map[string]any)) {
	t.Helper()
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.ops) != len(inputs) || len(h.fields) == 0 {
		t.Fatal("incomplete registration")
	}
	found := false
	for _, f := range h.fields {
		if f.Type == "secret" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing secret control")
	}
	for id, raw := range inputs {
		t.Run(id, func(t *testing.T) {
			fn := h.ops[id]
			if fn == nil {
				t.Fatal("missing operation")
			}
			sink := &captureSink{}
			if e := fn(&testCtx{context.Background()}, json.RawMessage(raw), sink); e != nil || sink.v == nil {
				t.Fatalf("%s: %v %#v", id, e, sink.v)
			}
			vars := contract.StructToVars(sink.v)
			wire, _ := json.Marshal(vars)
			var m map[string]any
			_ = json.Unmarshal(wire, &m)
			check(id, m)
			if e := fn(&testCtx{context.Background()}, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errors.New("test-secret") }
func (badReader) Close() error             { return nil }
func TestClientBoundaries(t *testing.T) {
	for _, code := range []int{301, 400, 401, 403, 409, 412, 422, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			a := fakeAPI(t, func(r *http.Request) (int, string) { calls++; return code, `{"error":"test-secret"}` })
			var out map[string]any
			_, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out)
			if e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
				t.Fatal(e, calls)
			}
		})
	}
	for _, body := range []string{`{bad`, `{} {}`, strings.Repeat("x", int(jsonLimit)+1)} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		var out map[string]any
		if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil {
			t.Fatal("bad body accepted")
		}
	}
	a := fakeAPI(t, func(*http.Request) (int, string) { t.Fatal("request should be rejected locally"); return 200, `{}` })
	var out map[string]any
	for _, path := range []string{"relative", "//evil.example/", "/bad?q=x", "/bad#fragment"} {
		if _, e := a.request(context.Background(), "GET", path, nil, nil, &out); e == nil {
			t.Fatal(path)
		}
	}
	if _, e := a.request(context.Background(), "POST", "/probe", nil, map[string]any{"body": strings.Repeat("x", int(jsonLimit)+1)}, &out); e == nil {
		t.Fatal("oversize body accepted")
	}
	if _, e := a.request(context.Background(), "POST", "/probe", nil, make(chan int), &out); e == nil {
		t.Fatal("invalid JSON accepted")
	}
	a = newAPI(testCred())
	if a.client.Timeout != 55*time.Second {
		t.Fatal("missing timeout")
	}
	a.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.request(ctx, "GET", "/probe", nil, nil, &out); e == nil {
		t.Fatal("cancel ignored")
	}
	a.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: badReader{}, Header: http.Header{}, Request: r}, nil
	})
	if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
	calls := 0
	a.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example/token"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || calls != 1 {
		t.Fatal("redirect followed", e, calls)
	}
}
func TestGeneratedDispatch(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		b := bodyOf(t, r)
		q := b["query"].(string)
		switch {
		case strings.Contains(q, "productUpdate"):
			v := b["variables"].(map[string]any)["product"].(map[string]any)
			if v["title"] != "New title" {
				t.Fatal(v)
			}
			return 200, `{"data":{"productUpdate":{"product":{"id":"gid://shopify/Product/1","title":"New title","productType":"Book"},"userErrors":[]}}}`
		case strings.Contains(q, "shop {"):
			return 200, `{"data":{"shop":{"id":"gid://shopify/Shop/1","myshopifyDomain":"example-shop.myshopify.com"}}}`
		case strings.Contains(q, "products("):
			return 200, `{"data":{"products":{"nodes":[{"id":"gid://shopify/Product/1","productType":"Book"}],"pageInfo":{"hasNextPage":true,"endCursor":"opaque+token"}}}}`
		case strings.Contains(q, "product("):
			return 200, `{"data":{"product":{"id":"gid://shopify/Product/1","productType":"Book"}}}`
		case strings.Contains(q, "inventoryItems("):
			return 200, `{"data":{"inventoryItems":{"nodes":[{"id":"gid://shopify/InventoryItem/1","tracked":false,"requiresShipping":true}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`
		case strings.Contains(q, "orders("):
			return 200, `{"data":{"orders":{"nodes":[{"id":"gid://shopify/Order/1","currentTotalPriceSet":{"shopMoney":{"amount":"123.40","currencyCode":"USD"}}}],"pageInfo":{"hasNextPage":false}}}}`
		case strings.Contains(q, "order("):
			return 200, `{"data":{"order":{"id":"gid://shopify/Order/1","displayFinancialStatus":"PAID","currentTotalPriceSet":{"shopMoney":{"amount":"123.40","currencyCode":"USD"}}}}}`
		}
		t.Fatal(q)
		return 500, `{}`
	})
	dispatchAll(t, a, map[string]string{"health_check": `{}`, "get_shop": `{}`, "list_products": `{"limit":2,"cursor":"previous"}`, "get_product": `{"product_id":"gid://shopify/Product/1"}`, "list_orders": `{}`, "get_order": `{"order_id":"gid://shopify/Order/1"}`, "list_inventory_items": `{}`, "update_product": `{"product_id":"gid://shopify/Product/1","changes":{"title":"New title"}}`}, func(id string, v map[string]any) {
		if id == "get_product" && v["product"].(map[string]any)["product_type"] != "Book" {
			t.Fatal(v)
		}
		if id == "get_order" && v["order"].(map[string]any)["total"].(map[string]any)["currency"] != "USD" {
			t.Fatal(v)
		}
	})
}
func TestPaginationAndErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"errors":[{"message":"test-secret"}],"data":{"products":{}}}`, `{"data":{"products":{}}}`, `{"data":{"products":{"nodes":null,"pageInfo":{"hasNextPage":false}}}}`, `{"data":{"products":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"same"}}}}`, `{"data":{"products":{"nodes":[{}],"pageInfo":{"hasNextPage":false}}}}`} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := a.list_products(context.Background(), &ListProductsIn{Cursor: "same"}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(e, body)
		}
	}
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		v := bodyOf(t, r)["variables"].(map[string]any)
		if v["after"] != "a+/=" || v["first"] != float64(2) || v["query"] != "status:active" {
			t.Fatal(v)
		}
		return 200, `{"data":{"products":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`
	})
	if v, e := a.list_products(context.Background(), &ListProductsIn{Limit: 2, Cursor: "a+/=", Query: "status:active"}); e != nil || v.HasMore || v.Products == nil {
		t.Fatal(v, e)
	}
	for _, body := range []string{`{"data":{"productUpdate":{"product":{"id":"gid://shopify/Product/1"},"userErrors":[{"message":"test-secret"}]}}}`, `{"data":{"productUpdate":{"product":{"id":"gid://shopify/Product/1"}}}}`} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		title := "t"
		if _, e := a.update_product(context.Background(), &UpdateProductIn{ProductID: "gid://shopify/Product/1", Changes: schema.ProductChanges{Title: &title}}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(e)
		}
	}
}
func TestInputValidation(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) (int, string) { t.Fatal("invalid input reached provider"); return 200, `{}` })
	for _, name := range []string{"https://evil.example", "a.b", "-bad", "example.myshopify.com", "a/secret"} {
		a.cred.Shop = name
		if _, e := a.origin(); e == nil {
			t.Fatal(name)
		}
	}
	a.cred = testCred()
	for _, id := range []string{"1", "gid://shopify/Order/1", "gid://shopify/Product/1?x"} {
		if _, e := a.get_product(context.Background(), &GetProductIn{ProductID: id}); e == nil {
			t.Fatal(id)
		}
	}
	if _, e := a.list_products(context.Background(), &ListProductsIn{Limit: 101}); e == nil {
		t.Fatal("limit")
	}
	for _, change := range []schema.ProductChanges{{}, {Title: ptr("")}, {Status: ptr("BAD")}} {
		if _, e := a.update_product(context.Background(), &UpdateProductIn{ProductID: "gid://shopify/Product/1", Changes: change}); e == nil {
			t.Fatal("changes")
		}
	}
	a.cred.AccessToken = "bad\nsecret"
	if h, e := a.health_check(context.Background(), &HealthCheckIn{}); e != nil || h.OK {
		t.Fatal(h, e)
	}
}
func ptr(s string) *string { return &s }
