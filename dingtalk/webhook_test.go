package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

type sourceCtx struct {
	context.Context
	cred   map[string]string
	events []*MessageEvent
	ids    []string
	fail   bool
}

func (c *sourceCtx) Credential() map[string]string { return c.cred }
func (c *sourceCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c *sourceCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c *sourceCtx) Fetch(*plugin.File) ([]byte, error)       { return nil, errors.New("unused") }
func (c *sourceCtx) UpdateCredential(map[string]string) error { return nil }
func (c *sourceCtx) ReportStatus(string, string)              {}
func (c *sourceCtx) Trigger(name, id string, payload any) error {
	if c.fail {
		return errors.New("delivery unavailable")
	}
	if name != "message" {
		return errors.New("wrong event")
	}
	c.ids = append(c.ids, id)
	c.events = append(c.events, payload.(*MessageEvent))
	return nil
}
func signedRequest(body string, ts int64) *sokel.WebhookRequest {
	s := strconv.FormatInt(ts, 10)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(s + "\nsecret"))
	return &sokel.WebhookRequest{Method: "POST", Body: []byte(body), Headers: map[string]string{"Timestamp": s, "Sign": base64.StdEncoding.EncodeToString(mac.Sum(nil))}}
}
func TestWebhookValidationAndNormalization(t *testing.T) {
	ctx := &sourceCtx{Context: context.Background(), cred: map[string]string{"app_secret": "secret", "robot_code": "robot"}}
	body := `{"msgId":"m1","conversationId":"c1","robotCode":"robot","msgtype":"richText","sessionWebhook":"DO_NOT_EMIT","senderStaffId":"u1","content":{"richText":[{"text":"hello"},{"downloadCode":"attachment"}]}}`
	r := signedRequest(body, time.Now().UnixMilli())
	if got := webhook(ctx, r); got.Status != 200 {
		t.Fatalf("status %d", got.Status)
	}
	if len(ctx.events) != 1 || ctx.events[0].Text != "hello" || ctx.events[0].DownloadCodes[0] != "attachment" || ctx.ids[0] != "m1" {
		t.Fatalf("events %#v", ctx.events)
	}
	encoded, _ := json.Marshal(ctx.events[0])
	if strings.Contains(string(encoded), "DO_NOT_EMIT") {
		t.Fatal("session webhook leaked")
	}
	for _, tc := range []struct {
		name   string
		change func(*sokel.WebhookRequest)
		want   int
	}{
		{"signature", func(r *sokel.WebhookRequest) { r.Headers["Sign"] = "invalid" }, 401},
		{"expired", func(r *sokel.WebhookRequest) { *r = *signedRequest(body, time.Now().Add(-2*time.Hour).UnixMilli()) }, 401},
		{"future", func(r *sokel.WebhookRequest) { *r = *signedRequest(body, time.Now().Add(2*time.Hour).UnixMilli()) }, 401},
		{"wrong robot", func(r *sokel.WebhookRequest) { r.Body = []byte(strings.ReplaceAll(body, `"robot"`, `"other"`)) }, 403},
		{"missing id", func(r *sokel.WebhookRequest) { r.Body = []byte(`{}`) }, 400},
		{"invalid json", func(r *sokel.WebhookRequest) { r.Body = []byte(`no`) }, 400},
		{"method", func(r *sokel.WebhookRequest) { r.Method = "GET" }, 405},
		{"oversize", func(r *sokel.WebhookRequest) { r.Body = make([]byte, (1<<20)+1) }, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := signedRequest(body, time.Now().UnixMilli())
			tc.change(r)
			if got := webhook(ctx, r); got.Status != tc.want {
				t.Fatalf("status=%d want=%d", got.Status, tc.want)
			}
		})
	}
	if len(ctx.events) != 1 {
		t.Fatal("invalid requests triggered events")
	}
	ctx.fail = true
	if got := webhook(ctx, signedRequest(body, time.Now().UnixMilli())); got.Status != 503 {
		t.Fatal("failed delivery acknowledged")
	}
	ctx.fail = false
	r = signedRequest(`{"msgId":"m2","conversationId":"c1","robotCode":"robot","msgtype":"audio","content":{"recognition":"spoken","downloadCode":"audio"}}`, time.Now().UnixMilli())
	r.BodyB64 = base64.StdEncoding.EncodeToString(r.Body)
	r.Body = nil
	if got := webhook(ctx, r); got.Status != 200 || ctx.events[1].Text != "spoken" {
		t.Fatalf("base64/audio failed: %d", got.Status)
	}
}
