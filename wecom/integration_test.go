package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

type portalTestContext struct {
	context.Context
	fields    map[string]string
	events    []InteractionActionEvent
	emitError bool
}

func (c *portalTestContext) Credential() map[string]string { return c.fields }
func (*portalTestContext) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (*portalTestContext) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (*portalTestContext) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unused") }
func (c *portalTestContext) Trigger(event, id string, payload any) error {
	if c.emitError {
		return errors.New("broker unavailable")
	}
	p := payload.(*InteractionActionEvent)
	if event != "interaction_action" || p.EventID != id {
		return errors.New("invalid event contract")
	}
	c.events = append(c.events, *p)
	return nil
}
func (*portalTestContext) UpdateCredential(map[string]string) error { return nil }
func (*portalTestContext) ReportStatus(string, string)              {}

type portalTestAPI struct {
	count           int
	fail            bool
	user            string
	delivery        Delivery
	lastDescription string
	deadline        bool
	verifyError     bool
}

func (c *portalTestAPI) VerifyApplication(_ context.Context, cred Cred) error {
	if c.verifyError || cred.Secret == "FAKE-SECRET" {
		return errors.New("application credential rejected")
	}
	return nil
}

func (*portalTestAPI) AccessToken(context.Context, Cred) (string, error) { return "test-token", nil }
func (c *portalTestAPI) SendText(context.Context, Cred, []string, string) (Delivery, error) {
	c.count++
	return c.delivery, nil
}
func (c *portalTestAPI) SendCard(_ context.Context, _ Cred, _ []string, _ string, description, _ string, _ string) (Delivery, error) {
	c.count++
	c.lastDescription = description
	if c.fail {
		return Delivery{}, errors.New("upstream unavailable")
	}
	return c.delivery, nil
}
func (c *portalTestAPI) UserID(ctx context.Context, _ Cred, code string) (string, error) {
	_, c.deadline = ctx.Deadline()
	if code == "bad" {
		return "", errors.New("invalid code")
	}
	return c.user, nil
}
func portalFixture(t *testing.T) (*application, *portalTestContext, *portalTestAPI) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := &portalTestContext{Context: context.Background(), fields: map[string]string{"corp_id": "wwtest", "agent_id": "1000002", "secret": "TEST-SECRET"}}
	api := &portalTestAPI{user: "alice", delivery: Delivery{MsgID: "message-1"}}
	return &application{store: s, api: api}, ctx, api
}
func interactionInput() *SendInteractiveMessageIn {
	return &SendInteractiveMessageIn{RequestID: "request-business-123", Users: []string{"Alice", "bob"}, Title: "发布意见", Content: "<script>alert(1)</script> 请审核本次发布计划。", Buttons: []MessageButton{{ID: "approve", Text: "同意", Value: map[string]any{"decision": "approved", "metadata": map[string]any{"version": "1.2.3"}}}, {ID: "reject", Text: "退回", Value: map[string]any{"decision": "rejected"}}}, Context: map[string]any{"request": map[string]any{"id": "release-123"}}, PortalURL: "https://alerts.example.com/hooks/test-credential", ExpiresHours: 72}
}
func sendTestInteraction(t *testing.T, a *application, ctx *portalTestContext) *SendInteractiveMessageOut {
	t.Helper()
	out, err := a.sendInteractiveMessage(ctx, interactionInput())
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func portalCall(a *application, ctx *portalTestContext, op string, body portalRequest) sokel.WebhookResponse {
	raw, _ := json.Marshal(body)
	return a.webhook(ctx, &sokel.WebhookRequest{Method: http.MethodPost, Query: "op=" + op, Headers: map[string]string{"Content-Type": "application/json", "Origin": "https://alerts.example.com"}, Body: raw})
}
func loginTicket(t *testing.T, a *application, ctx *portalTestContext, id string) portalResponse {
	t.Helper()
	state, _ := randomID()
	start := portalCall(a, ctx, "login", portalRequest{InteractionID: id, State: state})
	if start.Status != 200 {
		t.Fatalf("start %d: %s", start.Status, start.Body)
	}
	resp := portalCall(a, ctx, "exchange", portalRequest{InteractionID: id, State: state, Code: "member-code"})
	if resp.Status != 200 {
		t.Fatalf("exchange %d: %s", resp.Status, resp.Body)
	}
	var out portalResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInteractiveOperationDedupAndPartialDelivery(t *testing.T) {
	a, ctx, api := portalFixture(t)
	api.delivery.InvalidUser = "bob"
	first := sendTestInteraction(t, a, ctx)
	if first.AllAccepted || first.Reused || first.InvalidUser != "bob" {
		t.Fatalf("partial result: %+v", first)
	}
	second := sendTestInteraction(t, a, ctx)
	if !second.Reused || second.InteractionID != first.InteractionID || api.count != 1 {
		t.Fatal("repeat sent a second notification")
	}
	if strings.Contains(api.lastDescription, "<script>") || !strings.Contains(api.lastDescription, "&lt;script&gt;") {
		t.Fatal("card preview did not escape business text")
	}
	changed := interactionInput()
	changed.Title = "changed"
	if _, err := a.sendInteractiveMessage(ctx, changed); err == nil {
		t.Fatal("conflicting external ID accepted")
	}
	ctx.fields["secret"] = "ROTATED-SECRET"
	rotated := sendTestInteraction(t, a, ctx)
	if rotated.InteractionID != first.InteractionID || api.count != 1 {
		t.Fatal("secret rotation lost existing alert identity")
	}
}

func TestFailedSendCanResumeWithoutCreatingAnotherInteraction(t *testing.T) {
	a, ctx, api := portalFixture(t)
	api.fail = true
	if _, err := a.sendInteractiveMessage(ctx, interactionInput()); err == nil {
		t.Fatal("failed upstream reported success")
	}
	api.fail = false
	out := sendTestInteraction(t, a, ctx)
	if !out.Reused || api.count != 2 {
		t.Fatal("retry did not resume saved alert")
	}
}

func TestPortalOAuthAndActionEndToEnd(t *testing.T) {
	a, ctx, api := portalFixture(t)
	sent := sendTestInteraction(t, a, ctx)
	state, _ := randomID()
	start := portalCall(a, ctx, "login", portalRequest{InteractionID: sent.InteractionID, State: state})
	var auth struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	json.Unmarshal(start.Body, &auth)
	u, err := url.Parse(auth.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if start.Status != 200 || u.Host != "open.weixin.qq.com" || q.Get("scope") != "snsapi_base" || q.Get("redirect_uri") != sent.InteractionURL || q.Get("state") != state || q.Get("agentid") != "1000002" {
		t.Fatalf("invalid authorize URL: %s", start.Body)
	}
	exchanged := portalCall(a, ctx, "exchange", portalRequest{InteractionID: sent.InteractionID, State: state, Code: "member-code"})
	if exchanged.Status != 200 || !api.deadline {
		t.Fatalf("exchange failed or lacks timeout: %s", exchanged.Body)
	}
	var session portalResponse
	json.Unmarshal(exchanged.Body, &session)
	if session.UserID != "alice" || !validNonce(session.Ticket) || session.Interaction.Content != interactionInput().Content {
		t.Fatal("incorrect verified session")
	}
	replay := portalCall(a, ctx, "exchange", portalRequest{InteractionID: sent.InteractionID, State: state, Code: "member-code"})
	if replay.Status != 403 {
		t.Fatal("OAuth state replay accepted")
	}
	claim := portalCall(a, ctx, "action", portalRequest{InteractionID: sent.InteractionID, Ticket: session.Ticket, ActionID: "approve"})
	var claimed portalResponse
	json.Unmarshal(claim.Body, &claimed)
	if len(ctx.events) != 0 || claimed.EventStatus != "pending" {
		t.Fatal("HTTP handler synchronously drained the outbox")
	}
	_, ns, _ := credential(ctx)
	if err := a.flushEvents(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if claim.Status != 200 || claimed.Interaction.LastActionID != "approve" || claimed.Ticket == "" || len(ctx.events) != 1 || ctx.events[0].RequestID != "request-business-123" || ctx.events[0].ActorUserID != "alice" {
		t.Fatalf("claim not reflected in event: %s %+v", claim.Body, ctx.events)
	}
	firstEvent := ctx.events[0]
	if firstEvent.InteractionID != sent.InteractionID || firstEvent.MsgID != "message-1" || firstEvent.ActionID != "approve" || firstEvent.ActionText != "同意" || firstEvent.ActionValue["decision"] != "approved" || firstEvent.Context["request"].(map[string]any)["id"] != "release-123" {
		t.Fatalf("custom action data did not survive portal and event delivery: %+v", firstEvent)
	}
	again := portalCall(a, ctx, "action", portalRequest{InteractionID: sent.InteractionID, Ticket: session.Ticket, ActionID: "approve"})
	var duplicate portalResponse
	json.Unmarshal(again.Body, &duplicate)
	if again.Status != 200 || duplicate.EventID != claimed.EventID || len(ctx.events) != 1 {
		t.Fatal("retry duplicated a business event")
	}
	confirm := portalCall(a, ctx, "action", portalRequest{InteractionID: sent.InteractionID, Ticket: claimed.Ticket, ActionID: "reject"})
	var done portalResponse
	json.Unmarshal(confirm.Body, &done)
	if err := a.flushEvents(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if confirm.Status != 200 || done.Interaction.LastActionID != "reject" || len(ctx.events) != 2 {
		t.Fatalf("confirmation failed: %s", confirm.Body)
	}
	if ctx.events[0].Revision != 1 || ctx.events[1].Revision != 2 {
		t.Fatal("event revisions do not order the transitions")
	}
	status, err := a.getInteractiveMessage(ctx, &GetInteractiveMessageIn{InteractionID: sent.InteractionID})
	if err != nil || status.LastActionID != "reject" {
		t.Fatalf("query: %+v %v", status, err)
	}
}

func TestPortalRejectsForgedBusinessPayloadWithoutConsumingTicket(t *testing.T) {
	a, ctx, _ := portalFixture(t)
	sent := sendTestInteraction(t, a, ctx)
	session := loginTicket(t, a, ctx, sent.InteractionID)
	for _, field := range []string{"action_value", "context", "actor_user_id"} {
		raw, _ := json.Marshal(map[string]any{"interaction_id": sent.InteractionID, "ticket": session.Ticket, "action_id": "approve", field: map[string]any{"forged": true}})
		response := a.webhook(ctx, &sokel.WebhookRequest{Method: "POST", Query: "op=action", Headers: map[string]string{"Content-Type": "application/json", "Origin": "https://alerts.example.com"}, Body: raw})
		if response.Status != 400 {
			t.Fatalf("forged %s accepted: %d", field, response.Status)
		}
	}
	response := portalCall(a, ctx, "action", portalRequest{InteractionID: sent.InteractionID, Ticket: session.Ticket, ActionID: "approve"})
	if response.Status != 200 {
		t.Fatalf("rejected payload consumed the valid ticket: %s", response.Body)
	}
	_, ns, _ := credential(ctx)
	if err := a.flushEvents(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if len(ctx.events) != 1 || ctx.events[0].ActionValue["decision"] != "approved" || ctx.events[0].ActorUserID != "alice" {
		t.Fatalf("event was not built from trusted stored data: %+v", ctx.events)
	}
}

func TestPortalRejectsUnauthorizedAndCrossOriginRequests(t *testing.T) {
	a, ctx, api := portalFixture(t)
	sent := sendTestInteraction(t, a, ctx)
	state, _ := randomID()
	raw, _ := json.Marshal(portalRequest{InteractionID: sent.InteractionID, State: state})
	bad := a.webhook(ctx, &sokel.WebhookRequest{Method: "POST", Query: "op=login", Headers: map[string]string{"Content-Type": "application/json", "Origin": "https://attacker.example"}, Body: raw})
	if bad.Status != 403 {
		t.Fatal("cross-origin accepted")
	}
	api.user = "outsider"
	if r := portalCall(a, ctx, "login", portalRequest{InteractionID: sent.InteractionID, State: state}); r.Status != 200 {
		t.Fatal(string(r.Body))
	}
	denied := portalCall(a, ctx, "exchange", portalRequest{InteractionID: sent.InteractionID, State: state, Code: "member-code"})
	if denied.Status != 403 || strings.Contains(string(denied.Body), "请审核") {
		t.Fatal("nonrecipient read alert details")
	}
	for _, req := range []*sokel.WebhookRequest{
		{Method: "GET", Query: "op=action"},
		{Method: "POST", Query: "op=action", Headers: map[string]string{"Content-Type": "text/plain"}, Body: raw},
		{Method: "POST", Query: "op=action", Headers: map[string]string{"Content-Type": "application/json"}, Body: []byte(`{} {}`)},
	} {
		if r := a.webhook(ctx, req); r.Status < 400 {
			t.Fatalf("invalid request accepted: %+v", req)
		}
	}
	page := a.webhook(ctx, &sokel.WebhookRequest{Method: "GET", Query: "interaction=" + sent.InteractionID})
	if page.Status != 200 || strings.Contains(string(page.Body), interactionInput().Title) || !strings.Contains(page.Headers["Content-Security-Policy"], "frame-ancestors 'none'") || strings.Contains(string(page.Body), "{{NONCE}}") {
		t.Fatal("public shell leaked data or lacked protection")
	}
}

func TestPortalOutboxRetainsActionsForRetry(t *testing.T) {
	a, ctx, _ := portalFixture(t)
	sent := sendTestInteraction(t, a, ctx)
	session := loginTicket(t, a, ctx, sent.InteractionID)
	ctx.emitError = true
	resp := portalCall(a, ctx, "action", portalRequest{InteractionID: sent.InteractionID, Ticket: session.Ticket, ActionID: "reject"})
	var out portalResponse
	json.Unmarshal(resp.Body, &out)
	_, ns, _ := credential(ctx)
	if err := a.flushEvents(ctx, ns); err == nil {
		t.Fatal("broker failure not reported")
	}
	if resp.Status != 200 || out.EventStatus != "pending" || out.Interaction.LastActionID != "reject" || len(a.store.PendingEvents(ns)) != 1 {
		t.Fatalf("action lost on event error: %s", resp.Body)
	}
	ctx.emitError = false
	if err := a.flushEvents(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if len(ctx.events) != 1 || ctx.events[0].EventID != out.EventID || len(a.store.PendingEvents(ns)) != 0 {
		t.Fatal("durable outbox retry failed")
	}
}

func TestUnverifiedApplicationCannotAccessExistingInteractionsOrEvents(t *testing.T) {
	a, ctx, _ := portalFixture(t)
	sent := sendTestInteraction(t, a, ctx)
	session := loginTicket(t, a, ctx, sent.InteractionID)
	if r := portalCall(a, ctx, "action", portalRequest{InteractionID: sent.InteractionID, Ticket: session.Ticket, ActionID: "reject"}); r.Status != 200 {
		t.Fatal(string(r.Body))
	}
	_, ns, _ := credential(ctx)
	ctx.fields["secret"] = "FAKE-SECRET"
	if _, err := a.getInteractiveMessage(ctx, &GetInteractiveMessageIn{InteractionID: sent.InteractionID}); err == nil {
		t.Fatal("fake secret read application data")
	}
	if _, err := a.sendInteractiveMessage(ctx, interactionInput()); err == nil {
		t.Fatal("fake secret reused existing alert")
	}
	state, _ := randomID()
	if r := portalCall(a, ctx, "login", portalRequest{InteractionID: sent.InteractionID, State: state}); r.Status != 503 {
		t.Fatal("fake secret started portal login")
	}
	if err := a.flushEvents(ctx, ns); err == nil || len(ctx.events) != 0 || len(a.store.PendingEvents(ns)) != 1 {
		t.Fatal("fake secret stole event routing")
	}
	health, err := a.healthCheck(ctx, &HealthCheckIn{})
	if err != nil || health.OK {
		t.Fatal("fake secret reported healthy")
	}
	ctx.fields["secret"] = "TEST-SECRET"
	a.store.Close()
	health, err = a.healthCheck(ctx, &HealthCheckIn{})
	if err != nil || health.OK {
		t.Fatal("closed store reported healthy")
	}
}

func TestPortalNamespaceAndExpiry(t *testing.T) {
	a, ctx, _ := portalFixture(t)
	sent := sendTestInteraction(t, a, ctx)
	ctx.fields["agent_id"] = "1000003"
	state, _ := randomID()
	r := portalCall(a, ctx, "login", portalRequest{InteractionID: sent.InteractionID, State: state})
	if r.Status != 404 {
		t.Fatal("another application accessed alert")
	}
	ctx.fields["agent_id"] = "1000002"
	a.store.now = func() time.Time { return time.Now().Add(73 * time.Hour) }
	r = portalCall(a, ctx, "login", portalRequest{InteractionID: sent.InteractionID, State: state})
	if r.Status != 410 {
		t.Fatal("expired alert accepted login")
	}
}
