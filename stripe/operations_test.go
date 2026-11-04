package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"strings"
	"testing"
)

const sampleCustomer = `{"id":"cus_example","object":"customer","email":"user@example.com","name":"User","created":1679609767,"livemode":false}`
const sampleSubscription = `{"id":"sub_example","object":"subscription","customer":"cus_example","status":"active","currency":"usd","created":1679609767,"cancel_at_period_end":false}`
const sampleIntent = `{"id":"pi_example","object":"payment_intent","customer":"cus_example","status":"succeeded","currency":"usd","amount":2000,"amount_received":2000,"client_secret":"never-expose","created":1679609767}`

func TestAllGeneratedReadOperations(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("Stripe write attempted")
		}
		switch r.URL.Path {
		case "/v1/customers/cus_example":
			reply(w, sampleCustomer)
		case "/v1/subscriptions/sub_example":
			reply(w, sampleSubscription)
		case "/v1/payment_intents/pi_example":
			reply(w, sampleIntent)
		case "/v1/subscriptions":
			if r.URL.Query().Get("status") != "all" || r.URL.Query().Get("customer") != "cus_example" {
				t.Error("missing query filters")
			}
			reply(w, `{"object":"list","has_more":false,"data":[`+sampleSubscription+`]}`)
		case "/v1/payment_intents":
			reply(w, `{"object":"list","has_more":false,"data":[`+sampleIntent+`]}`)
		case "/v1/customers":
			reply(w, `{"object":"list","has_more":false,"data":[`+sampleCustomer+`]}`)
		default:
			t.Error("unknown endpoint")
		}
	})
	t.Setenv("STRIPE_API_BASE_URL", c.base)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	ctx := &testCtx{Context: context.Background(), cred: map[string]string{"api_key": "test-secret"}}
	for op, input := range map[string]string{"get_customer": `{"id":"cus_example"}`, "get_subscription": `{"id":"sub_example"}`, "get_payment_intent": `{"id":"pi_example"}`, "list_customers": `{"email":"user@example.com"}`, "list_subscriptions": `{"customer":"cus_example","status":"all"}`, "list_payment_intents": `{"customer":"cus_example"}`} {
		sink := &testSink{}
		if e := h.calls[op](ctx, json.RawMessage(input), sink); e != nil {
			t.Fatalf("%s: %v", op, e)
		}
		b, _ := json.Marshal(sink.vars)
		if strings.Contains(string(b), "never-expose") || strings.Contains(string(b), "client_secret") {
			t.Fatal("client secret leaked")
		}
	}
}
func TestInvalidInputsAndMalformedPages(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached network") })
	if _, e := c.getCustomer(context.Background(), &GetCustomerIn{ID: "../cus_x"}); e == nil {
		t.Fatal("bad ID")
	}
	if _, e := c.listSubscriptions(context.Background(), &ListSubscriptionsIn{Customer: "pi_wrong"}); e == nil {
		t.Fatal("wrong customer prefix")
	}
	if _, e := c.listSubscriptions(context.Background(), &ListSubscriptionsIn{Status: "invalid"}); e == nil {
		t.Fatal("bad status")
	}
	if _, e := c.listPaymentIntents(context.Background(), &ListPaymentIntentsIn{StartingAfter: "cus_wrong"}); e == nil {
		t.Fatal("wrong cursor")
	}
	for _, body := range []string{`{}`, `null`, `{"object":"list","data":[],"has_more":true}`, `{"object":"list","data":[]}`, `{"object":"list","data":[{"id":"cus_same"}],"has_more":true}`, `{"object":"list","data":[{"id":"cus_a"},{"id":"cus_a"}],"has_more":false}`} {
		c = mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
		if _, e := c.listCustomers(context.Background(), &ListCustomersIn{StartingAfter: "cus_same"}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	c = mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if h, _ := c.health(context.Background()); h.OK {
		t.Fatal("false health success")
	}
}
