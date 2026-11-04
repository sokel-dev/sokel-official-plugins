package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"strconv"
	"strings"
	"time"
)

const maxWebhookBody = 2 << 20

func webhook(ctx sokel.WebhookCtx, r *sokel.WebhookRequest) sokel.WebhookResponse {
	return webhookAt(ctx, r, time.Now())
}
func webhookReply(status int) sokel.WebhookResponse {
	return sokel.WebhookResponse{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: []byte(`{}`)}
}
func validSignature(raw []byte, header, secret string, now time.Time) bool {
	if secret == "" || len(header) > 8192 {
		return false
	}
	var ts string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return false
		}
		switch kv[0] {
		case "t":
			if ts != "" {
				return false
			}
			ts = kv[1]
		case "v1":
			if len(signatures) >= 32 {
				return false
			}
			signatures = append(signatures, kv[1])
		}
	}
	n, e := strconv.ParseInt(ts, 10, 64)
	if e != nil || n < now.Unix()-300 || n > now.Unix()+300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(raw)
	sum := mac.Sum(nil)
	for _, s := range signatures {
		signature, e := hex.DecodeString(s)
		if e == nil && hmac.Equal(signature, sum) {
			return true
		}
	}
	return false
}
func webhookAt(ctx sokel.WebhookCtx, r *sokel.WebhookRequest, now time.Time) sokel.WebhookResponse {
	if r.Method != "POST" {
		return webhookReply(405)
	}
	cred := sokel.CredentialAs[Cred](ctx)
	if cred.WebhookSecret == "" {
		return webhookReply(503)
	}
	raw := r.Body
	if len(raw) > maxWebhookBody || len(r.BodyB64) > 3<<20 {
		return webhookReply(413)
	}
	if len(raw) == 0 && r.BodyB64 != "" {
		var e error
		raw, e = base64.StdEncoding.DecodeString(r.BodyB64)
		if e != nil {
			return webhookReply(400)
		}
	}
	if len(raw) > maxWebhookBody {
		return webhookReply(413)
	}
	if !validSignature(raw, r.Header("Stripe-Signature"), cred.WebhookSecret, now) {
		return webhookReply(401)
	}
	var event struct {
		ID         string `json:"id"`
		Object     string `json:"object"`
		APIVersion string `json:"api_version"`
		Type       string `json:"type"`
		Created    int64  `json:"created"`
		LiveMode   *bool  `json:"livemode"`
		Account    string `json:"account"`
		Data       struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if decodeJSON(raw, &event) != nil || validID(event.ID, "evt_") != nil || event.Object != "event" || event.Created <= 0 || event.LiveMode == nil || event.Type == "" {
		return webhookReply(400)
	}
	if event.Account != "" && validID(event.Account, "acct_") != nil {
		return webhookReply(400)
	}
	kind := ""
	switch {
	case event.Type == "customer.created" || event.Type == "customer.updated" || event.Type == "customer.deleted":
		kind = "customer"
	case strings.HasPrefix(event.Type, "customer.subscription."):
		kind = "subscription"
	case strings.HasPrefix(event.Type, "payment_intent."):
		kind = "payment_intent"
	default:
		return webhookReply(200)
	}
	if event.APIVersion != stripeVersion {
		return webhookReply(400)
	}
	var object struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	if decodeJSON(event.Data.Object, &object) != nil || object.Object != kind {
		return webhookReply(400)
	}
	var err error
	switch kind {
	case "customer":
		var customer Customer
		if decodeJSON(event.Data.Object, &customer) != nil || validID(customer.ID, "cus_") != nil {
			return webhookReply(400)
		}
		err = TriggerCustomerChanged(ctx, event.ID, &CustomerChangedEvent{EventID: event.ID, EventType: event.Type, Created: event.Created, Livemode: *event.LiveMode, AccountID: event.Account, Customer: customer})
	case "subscription":
		var sub Subscription
		if decodeJSON(event.Data.Object, &sub) != nil || validID(sub.ID, "sub_") != nil || sub.Status == "" {
			return webhookReply(400)
		}
		err = TriggerSubscriptionChanged(ctx, event.ID, &SubscriptionChangedEvent{EventID: event.ID, EventType: event.Type, Created: event.Created, Livemode: *event.LiveMode, AccountID: event.Account, Subscription: sub})
	case "payment_intent":
		var intent PaymentIntent
		if decodeJSON(event.Data.Object, &intent) != nil || validID(intent.ID, "pi_") != nil || intent.Status == "" || intent.Currency == "" {
			return webhookReply(400)
		}
		err = TriggerPaymentIntentChanged(ctx, event.ID, &PaymentIntentChangedEvent{EventID: event.ID, EventType: event.Type, Created: event.Created, Livemode: *event.LiveMode, AccountID: event.Account, PaymentIntent: intent})
	}
	if err != nil {
		return webhookReply(503)
	}
	return webhookReply(200)
}
