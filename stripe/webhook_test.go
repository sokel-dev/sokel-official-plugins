package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sourceCtx struct {
	context.Context
	cred   map[string]string
	events []map[string]any
	ids    []string
	fail   bool
}

func (c *sourceCtx) Credential() map[string]string      { return c.cred }
func (c *sourceCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unused") }
func (c *sourceCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c *sourceCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c *sourceCtx) UpdateCredential(map[string]string) error {
	return errors.New("webhooks must not write credentials")
}
func (c *sourceCtx) ReportStatus(string, string) {}
func (c *sourceCtx) Trigger(name, id string, p any) error {
	if c.fail {
		return errors.New("publish failed")
	}
	v := contract.StructToVars(p)
	v["event"] = name
	c.events = append(c.events, v)
	c.ids = append(c.ids, id)
	return nil
}
func signed(body string, ts int64) *sokel.WebhookRequest {
	s := strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	mac.Write([]byte(s + "." + body))
	return &sokel.WebhookRequest{Method: "POST", Body: []byte(body), Headers: map[string]string{"Stripe-Signature": "t=" + s + ",v1=" + hex.EncodeToString(mac.Sum(nil))}}
}

const intentEvent = `{"id":"evt_example","object":"event","api_version":"2026-10-28.endive","type":"payment_intent.succeeded","created":1793664000,"livemode":false,"data":{"object":{"id":"pi_example","object":"payment_intent","amount":2000,"amount_received":2000,"currency":"usd","status":"succeeded","client_secret":"DO_NOT_EMIT","created":1793663990,"customer":"cus_example"}}}`

func TestSignedEventAndSecretProjection(t *testing.T) {
	ctx := &sourceCtx{Context: context.Background(), cred: map[string]string{"webhook_secret": "whsec_test"}}
	now := time.Unix(1793664000, 0)
	if r := webhookAt(ctx, signed(intentEvent, now.Unix()), now); r.Status != 200 {
		t.Fatalf("signed webhook status %d", r.Status)
	}
	if len(ctx.events) != 1 || ctx.events[0]["event"] != "payment_intent_changed" || ctx.events[0]["event_id"] != "evt_example" {
		t.Fatalf("bad event %+v", ctx.events)
	}
	b, _ := json.Marshal(ctx.events)
	if strings.Contains(string(b), "DO_NOT_EMIT") || strings.Contains(string(b), "client_secret") {
		t.Fatal("payment secret leaked")
	}
}

func TestWebhookInvalidSignaturesAndBoundaries(t *testing.T) {
	now := time.Unix(1793664000, 0)
	ctx := &sourceCtx{Context: context.Background(), cred: map[string]string{"webhook_secret": "whsec_test"}}
	for _, tc := range []struct {
		name   string
		edit   func(*sokel.WebhookRequest)
		status int
	}{
		{"method", func(r *sokel.WebhookRequest) { r.Method = "GET" }, 405},
		{"invalid signature", func(r *sokel.WebhookRequest) { r.Headers["Stripe-Signature"] = "t=1793664000,v1=00" }, 401},
		{"expired", func(r *sokel.WebhookRequest) { *r = *signed(intentEvent, now.Unix()-301) }, 401},
		{"future", func(r *sokel.WebhookRequest) { *r = *signed(intentEvent, now.Unix()+301) }, 401},
		{"changed bytes", func(r *sokel.WebhookRequest) { r.Body = append(r.Body, ' ') }, 401},
		{"duplicate timestamp", func(r *sokel.WebhookRequest) { r.Headers["Stripe-Signature"] += ",t=1793664000" }, 401},
		{"missing identity", func(r *sokel.WebhookRequest) { *r = *signed(`{}`, now.Unix()) }, 400},
		{"wrong API version", func(r *sokel.WebhookRequest) {
			*r = *signed(strings.ReplaceAll(intentEvent, stripeVersion, "2020-08-27"), now.Unix())
		}, 400},
		{"missing object", func(r *sokel.WebhookRequest) {
			*r = *signed(strings.ReplaceAll(intentEvent, `"object":"payment_intent"`, `"object":"subscription"`), now.Unix())
		}, 400},
		{"too large", func(r *sokel.WebhookRequest) { r.Body = make([]byte, maxWebhookBody+1) }, 413},
		{"bad base64", func(r *sokel.WebhookRequest) { r.Body = nil; r.BodyB64 = "!" }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := signed(intentEvent, now.Unix())
			tc.edit(r)
			if got := webhookAt(ctx, r, now); got.Status != tc.status {
				t.Fatalf("status %d want %d", got.Status, tc.status)
			}
		})
	}
	if len(ctx.events) != 0 {
		t.Fatal("invalid webhook emitted")
	}
	ctx.fail = true
	if r := webhookAt(ctx, signed(intentEvent, now.Unix()), now); r.Status != 503 {
		t.Fatal("failed local publish acknowledged")
	}
	ctx.fail = false
	ctx.cred = map[string]string{}
	if r := webhookAt(ctx, signed(intentEvent, now.Unix()), now); r.Status != 503 {
		t.Fatal("missing secret accepted")
	}
}
func TestWebhookRotatedSignaturesReplayAndOtherObjects(t *testing.T) {
	now := time.Unix(1793664000, 0)
	ctx := &sourceCtx{Context: context.Background(), cred: map[string]string{"webhook_secret": "whsec_test"}}
	r := signed(intentEvent, now.Unix())
	r.Headers["Stripe-Signature"] = "v1=00," + r.Headers["Stripe-Signature"] + ",v0=ignored"
	r.BodyB64 = base64.StdEncoding.EncodeToString(r.Body)
	r.Body = nil
	if got := webhookAt(ctx, r, now); got.Status != 200 {
		t.Fatal("rotated signature/base64 rejected")
	}
	if got := webhookAt(ctx, r, now); got.Status != 200 || ctx.ids[0] != ctx.ids[1] {
		t.Fatal("replay id not stable")
	}
	for _, tc := range []struct{ kind, obj, name string }{{"customer.updated", sampleCustomer, "customer_changed"}, {"customer.subscription.updated", sampleSubscription, "subscription_changed"}} {
		body := `{"id":"evt_other","object":"event","api_version":"` + stripeVersion + `","type":"` + tc.kind + `","created":1793664000,"livemode":false,"data":{"object":` + tc.obj + `}}`
		if got := webhookAt(ctx, signed(body, now.Unix()), now); got.Status != 200 || ctx.events[len(ctx.events)-1]["event"] != tc.name {
			t.Fatalf("event %s failed", tc.kind)
		}
	}
	body := strings.ReplaceAll(intentEvent, "payment_intent.succeeded", "invoice.paid")
	count := len(ctx.events)
	if r := webhookAt(ctx, signed(body, now.Unix()), now); r.Status != 200 || len(ctx.events) != count {
		t.Fatal("unsupported event not ignored")
	}
}
