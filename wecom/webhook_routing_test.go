package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func TestGatewayCallbackWildcardHasNoLeadingSlash(t *testing.T) {
	s, err := OpenCallbackStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &featureApplication{callbacks: NewCallbackService(s), callbackStore: s}
	ctx := &portalTestContext{Context: context.Background(), fields: map[string]string{"corp_id": "wwtest", "agent_id": "1000002", "secret": "TEST-SECRET"}}
	alerts := &application{}
	// The first form is captured from Sokel's real /hooks/{token}/* gateway.
	for _, path := range []string{"callback", "/callback", "callback/", "/callback/"} {
		out := a.routeWebhook(alerts, ctx, &sokel.WebhookRequest{Method: http.MethodGet, Path: path, Query: fmt.Sprintf("msg_signature=x&timestamp=%d&nonce=n", time.Now().Unix())})
		if out.Status != http.StatusServiceUnavailable {
			t.Fatalf("%q: expected unconfigured-callback error, got %d", path, out.Status)
		}
	}
	for _, path := range []string{"", "/"} {
		out := a.routeWebhook(alerts, ctx, &sokel.WebhookRequest{Method: http.MethodGet, Path: path})
		if out.Status != http.StatusOK {
			t.Fatalf("alert shell %q: %d", path, out.Status)
		}
	}
	if out := a.routeWebhook(alerts, ctx, &sokel.WebhookRequest{Method: http.MethodGet, Path: "unrelated"}); out.Status != http.StatusNotFound {
		t.Fatalf("unknown path: %d", out.Status)
	}
}
