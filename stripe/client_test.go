package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomerCursorAndPinnedVersion(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/customers" || r.URL.Query().Get("starting_after") != "cus_prev" || r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Stripe-Version") != "2026-10-28.endive" {
			t.Error("incorrect Stripe request")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[{"id":"cus_next","object":"customer","name":"Customer","created":1679609767,"livemode":false}],"has_more":true}`)
	}))
	defer s.Close()
	c := &client{base: s.URL, cred: Cred{APIKey: "test-secret"}, http: s.Client()}
	out, e := c.listCustomers(context.Background(), &ListCustomersIn{StartingAfter: "cus_prev"})
	if e != nil {
		t.Fatal(e)
	}
	if out.NextStartingAfter != "cus_next" || !out.HasMore {
		t.Fatalf("bad page %+v", out)
	}
}
