package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

type callbackRecordedTrigger struct {
	event, id string
	payload   any
	failed    bool
}

type callbackIntegrationContext struct {
	portalTestContext
	failTrigger bool
	triggers    []callbackRecordedTrigger
}

func (c *callbackIntegrationContext) Trigger(event, id string, payload any) error {
	c.triggers = append(c.triggers, callbackRecordedTrigger{event, id, payload, c.failTrigger})
	if c.failTrigger {
		return errors.New("simulated broker failure")
	}
	return nil
}

func callbackIntegrationCtx(c Cred) *callbackIntegrationContext {
	return &callbackIntegrationContext{portalTestContext: portalTestContext{Context: context.Background(), fields: map[string]string{
		"corp_id": c.CorpID, "agent_id": c.AgentID, "secret": c.Secret,
		"callback_token": c.CallbackToken, "callback_aes_key": c.CallbackAesKey,
	}}}
}

func callbackIntegrationClient(t *testing.T) *WeComClient {
	t.Helper()
	return newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			if r.URL.Query().Get("corpsecret") == "FAKE-SECRET" {
				io.WriteString(w, `{"errcode":40013,"errmsg":"invalid credential"}`)
			} else {
				io.WriteString(w, documentedTokenResponse)
			}
		case "/cgi-bin/agent/get":
			io.WriteString(w, `{"errcode":0,"agentid":218}`)
		default:
			t.Errorf("callback integration should only verify credentials, called %s", r.URL.Path)
		}
	})
}

func callbackThroughWrapper(t *testing.T, a *featureApplication, ctx *callbackIntegrationContext, cred Cred, plain string) {
	t.Helper()
	query, body := callbackRequest(t, cred, plain, a.callbacks.now())
	response := a.callbackWebhook(ctx, &sokel.WebhookRequest{Method: http.MethodPost, Path: "/callback", Query: query.Encode(), Body: body})
	if response.Status != 200 || string(response.Body) != "success" {
		t.Fatalf("callback wrapper rejected authenticated event: status=%d body=%s", response.Status, response.Body)
	}
}

func TestCallbackWrappersRouteEventsAndPreserveContractFields(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	a := &featureApplication{client: callbackIntegrationClient(t), callbacks: service, callbackStore: store}
	ctx := callbackIntegrationCtx(cred)
	imageXML := callbackPlain(cred, "4561255354251345929", "image", `<MediaId>media-id-1</MediaId><PicUrl>https://example.com/image.jpg</PicUrl>`)
	cardXML := callbackPlain(cred, "", "event", `<Event>template_card_event</Event><EventKey>confirm</EventKey><TaskId>task-id-1</TaskId><CardType>button_interaction</CardType><ResponseCode>update-once-code</ResponseCode><SelectedItems><SelectedItem><QuestionKey>risk</QuestionKey><OptionIds><OptionId>yes</OptionId></OptionIds></SelectedItem></SelectedItems>`)
	callbackThroughWrapper(t, a, ctx, cred, imageXML)
	callbackThroughWrapper(t, a, ctx, cred, cardXML)
	before, err := store.Pending(cred, callbackCredentialKey(cred), 10)
	if err != nil || len(before) != 2 {
		t.Fatalf("expected two durable pending messages: %#v %v", before, err)
	}
	ctx.failTrigger = true
	if err := a.flushCallbackEvents(ctx, cred); err == nil {
		t.Fatal("source accepted a failed Trigger")
	}
	afterFailure, err := store.Pending(cred, callbackCredentialKey(cred), 10)
	if err != nil || len(afterFailure) != 2 || afterFailure[0].ID != before[0].ID {
		t.Fatalf("failed Trigger removed or changed durable event: %#v %v", afterFailure, err)
	}
	ctx.failTrigger = false
	if err := a.flushCallbackEvents(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if len(ctx.triggers) != 3 || !ctx.triggers[0].failed || ctx.triggers[1].id != ctx.triggers[0].id {
		t.Fatalf("retry did not reuse same id: %#v", ctx.triggers)
	}
	if ctx.triggers[1].event != "message_received" || ctx.triggers[2].event != "application_event" {
		t.Fatalf("wrong event routing: %#v", ctx.triggers)
	}
	for index, trigger := range ctx.triggers[1:] {
		b, err := json.Marshal(trigger.payload)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(b, &fields) != nil {
			t.Fatal("event payload is not JSON")
		}
		want := map[string]string{"event_id": trigger.id, "from_user": "zhangsan", "to_user": cred.CorpID, "agent_id": cred.AgentID}
		if index == 0 {
			want["msg_id"], want["message_type"], want["media_id"], want["pic_url"], want["raw_xml"] = "4561255354251345929", "image", "media-id-1", "https://example.com/image.jpg", imageXML
		} else {
			want["message_type"], want["event_type"], want["event_key"], want["task_id"], want["card_type"], want["response_code"], want["raw_xml"] = "event", "template_card_event", "confirm", "task-id-1", "button_interaction", "update-once-code", cardXML
		}
		for key, expected := range want {
			var got string
			if json.Unmarshal(fields[key], &got) != nil || got != expected {
				t.Errorf("%s field %s: want %q, got %s", trigger.event, key, expected, fields[key])
			}
		}
		// The source transport uses sokel tags, not json tags. Ensure both map
		// each concrete field to the same names asserted on the recorded payload.
		typ := reflect.TypeOf(trigger.payload).Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Tag.Get("sokel") != field.Tag.Get("json") {
				t.Errorf("%s field %s has mismatched transport tags", trigger.event, field.Name)
			}
		}
	}
	if pending, err := store.Pending(cred, callbackCredentialKey(cred), 10); err != nil || len(pending) != 0 {
		t.Fatalf("successful Trigger did not mark events: %#v %v", pending, err)
	}
	listed, err := a.listReceived(ctx, &ListReceivedIn{Limit: 10})
	if err != nil || len(listed.Messages) != 2 || listed.Messages[0].TaskID != "task-id-1" || listed.Messages[0].ResponseCode != "update-once-code" || listed.Messages[0].RawXML != cardXML {
		t.Fatalf("recent-message handler lost template action fields: %#v %v", listed, err)
	}
	// A duplicated upstream callback is acknowledged from the persisted cache
	// and cannot create a new downstream event after the first was dispatched.
	callbackThroughWrapper(t, a, ctx, cred, cardXML)
	if err := a.flushCallbackEvents(ctx, cred); err != nil || len(ctx.triggers) != 3 {
		t.Fatalf("duplicate callback emitted another event: %d %v", len(ctx.triggers), err)
	}
}

func TestCallbackPendingSurvivesSecretRotationAndRestart(t *testing.T) {
	service, store, cred := newCallbackTestService(t)
	a := &featureApplication{client: callbackIntegrationClient(t), callbacks: service, callbackStore: store}
	ctx := callbackIntegrationCtx(cred)
	plain := callbackPlain(cred, "persisted-msg-id", "text", `<Content>pending before rotation</Content>`)
	callbackThroughWrapper(t, a, ctx, cred, plain)
	before, err := store.Pending(cred, callbackCredentialKey(cred), 10)
	if err != nil || len(before) != 1 {
		t.Fatal("missing durable event before restart")
	}
	dir, clock := store.dir, service.now
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCallbackStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a.callbackStore, a.callbacks = reopened, NewCallbackService(reopened)
	a.callbacks.now = clock
	rotated := cred
	rotated.Secret = "ROTATED-SECRET"
	if callbackCredentialKey(rotated) != callbackCredentialKey(cred) {
		t.Fatal("callback pending identity is tied to rotatable Secret")
	}
	rotatedCtx := callbackIntegrationCtx(rotated)
	invalid := rotated
	invalid.Secret = "FAKE-SECRET"
	invalidCtx := callbackIntegrationCtx(invalid)
	if _, err := a.listReceived(invalidCtx, &ListReceivedIn{Limit: 10}); err == nil {
		t.Fatal("syntax-valid public app identity read pending data with invalid Secret")
	}
	if err := a.flushCallbackEvents(invalidCtx, invalid); err == nil || len(invalidCtx.triggers) != 0 {
		t.Fatal("unverified credential drained outbox")
	}
	wrongApp := rotated
	wrongApp.AgentID = "219"
	wrongAppCtx := callbackIntegrationCtx(wrongApp)
	if _, err := a.listReceived(wrongAppCtx, &ListReceivedIn{Limit: 10}); err == nil {
		t.Fatal("credential for another AgentID read callback data")
	}
	if err := a.flushCallbackEvents(rotatedCtx, rotated); err != nil {
		t.Fatal(err)
	}
	if len(rotatedCtx.triggers) != 1 || rotatedCtx.triggers[0].id != before[0].ID || rotatedCtx.triggers[0].event != "message_received" {
		t.Fatalf("rotation/restart stranded or changed original event: %#v", rotatedCtx.triggers)
	}
	pending, err := reopened.Pending(rotated, callbackCredentialKey(rotated), 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("rotated source left pending records: %#v %v", pending, err)
	}
	if b, _ := json.Marshal(rotatedCtx.triggers[0].payload); strings.Contains(string(b), rotated.Secret) || strings.Contains(string(b), rotated.CallbackAesKey) || strings.Contains(string(b), rotated.CallbackToken) {
		t.Fatal("event payload leaked application/callback credentials")
	}
}
