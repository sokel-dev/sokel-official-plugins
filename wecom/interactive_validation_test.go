package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type interactionValidationAPI struct {
	*portalTestAPI
	verifications int
	entryText     string
}

func (c *interactionValidationAPI) VerifyApplication(ctx context.Context, cred Cred) error {
	c.verifications++
	return c.portalTestAPI.VerifyApplication(ctx, cred)
}

func (c *interactionValidationAPI) SendCard(ctx context.Context, cred Cred, users []string, title, description, link, button string) (Delivery, error) {
	c.entryText = button
	return c.portalTestAPI.SendCard(ctx, cred, users, title, description, link, button)
}

func validationInteractionInput() *SendInteractiveMessageIn {
	return &SendInteractiveMessageIn{
		Users: []string{"alice"}, Title: "Review publication", Content: "Please choose the next step.",
		Buttons: []MessageButton{{ID: "publish_v2-1", Text: "Publish", Value: map[string]any{"release": "v2"}}},
		Context: map[string]any{"request": "review-42"}, PortalURL: "https://example.test/hooks/credential-1",
	}
}

func TestInteractiveOptionalRequestIDControlsOnlyExplicitDeduplication(t *testing.T) {
	a, ctx, api := portalFixture(t)
	seen := map[string]bool{}
	for _, requestID := range []string{"", " \t\n", ""} {
		in := validationInteractionInput()
		in.RequestID = requestID
		out, err := a.sendInteractiveMessage(ctx, in)
		if err != nil || out.Reused || out.InteractionID == "" || seen[out.InteractionID] {
			t.Fatalf("omitted request ID suppressed a deliberate send: %+v %v", out, err)
		}
		seen[out.InteractionID] = true
		stored, err := a.getInteractiveMessage(ctx, &GetInteractiveMessageIn{InteractionID: out.InteractionID})
		if err != nil || stored.RequestID != "" {
			t.Fatalf("blank request ID was persisted as a business identifier: %+v %v", stored, err)
		}
	}
	in := validationInteractionInput()
	in.RequestID = "explicit-retry-key"
	first, err := a.sendInteractiveMessage(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.sendInteractiveMessage(ctx, in)
	if err != nil || !second.Reused || second.InteractionID != first.InteractionID || api.count != 4 {
		t.Fatalf("explicit retry key did not suppress only its own retry: %+v %v sends=%d", second, err, api.count)
	}
}

func TestInteractiveInvalidInputsDoNotAuthenticateSendOrPersist(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SendInteractiveMessageIn)
	}{
		{"missing buttons", func(in *SendInteractiveMessageIn) { in.Buttons = nil }},
		{"nine buttons", func(in *SendInteractiveMessageIn) {
			for i := 1; i < 9; i++ {
				in.Buttons = append(in.Buttons, MessageButton{ID: fmt.Sprintf("choice-%d", i), Text: "Choose"})
			}
		}},
		{"duplicate IDs", func(in *SendInteractiveMessageIn) { in.Buttons = append(in.Buttons, in.Buttons[0]) }},
		{"missing ID", func(in *SendInteractiveMessageIn) { in.Buttons[0].ID = "" }},
		{"ID with whitespace", func(in *SendInteractiveMessageIn) { in.Buttons[0].ID = "publish now" }},
		{"non ASCII ID", func(in *SendInteractiveMessageIn) { in.Buttons[0].ID = "同意" }},
		{"ID over 64 bytes", func(in *SendInteractiveMessageIn) { in.Buttons[0].ID = strings.Repeat("a", 65) }},
		{"blank button text", func(in *SendInteractiveMessageIn) { in.Buttons[0].Text = " \n\t" }},
		{"41 character button text", func(in *SendInteractiveMessageIn) { in.Buttons[0].Text = strings.Repeat("同", 41) }},
		{"invalid UTF8 button text", func(in *SendInteractiveMessageIn) { in.Buttons[0].Text = string([]byte{0xff}) }},
		{"five character link entry", func(in *SendInteractiveMessageIn) { in.Button = "查看消息详情" }},
		{"invalid UTF8 link entry", func(in *SendInteractiveMessageIn) { in.Button = string([]byte{0xff}) }},
		{"request ID over byte limit", func(in *SendInteractiveMessageIn) { in.RequestID = strings.Repeat("界", 86) }},
		{"invalid UTF8 request ID", func(in *SendInteractiveMessageIn) { in.RequestID = string([]byte{0xff}) }},
		{"combined JSON size", func(in *SendInteractiveMessageIn) {
			in.Context = map[string]any{"payload": strings.Repeat("a", 9000)}
			in.Buttons[0].Value = map[string]any{"payload": strings.Repeat("b", 9000)}
		}},
		{"combined values across buttons", func(in *SendInteractiveMessageIn) {
			in.Buttons[0].Value = map[string]any{"payload": strings.Repeat("a", 9000)}
			in.Buttons = append(in.Buttons, MessageButton{ID: "other", Text: "Other", Value: map[string]any{"payload": strings.Repeat("b", 9000)}})
		}},
		{"encoded JSON size includes escaping", func(in *SendInteractiveMessageIn) {
			in.Context = map[string]any{"payload": strings.Repeat("\"", 9000)}
		}},
		{"non JSON numeric value", func(in *SendInteractiveMessageIn) { in.Buttons[0].Value = map[string]any{"number": math.Inf(1)} }},
		{"cyclic context", func(in *SendInteractiveMessageIn) { in.Context["cycle"] = in.Context }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ctx, base := portalFixture(t)
			api := &interactionValidationAPI{portalTestAPI: base}
			a.api = api
			in := validationInteractionInput()
			tc.mutate(in)
			if out, err := a.sendInteractiveMessage(ctx, in); err == nil || out != nil {
				t.Fatalf("invalid input was accepted: %+v %v", out, err)
			}
			if api.verifications != 0 || base.count != 0 || len(a.store.data.Alerts) != 0 {
				t.Fatalf("invalid input caused side effects: auth=%d sends=%d records=%d", api.verifications, base.count, len(a.store.data.Alerts))
			}
		})
	}
}

func TestInteractiveDocumentedLimitsAcceptMultibyteTextAndEightActions(t *testing.T) {
	a, ctx, base := portalFixture(t)
	api := &interactionValidationAPI{portalTestAPI: base}
	a.api = api
	in := validationInteractionInput()
	in.RequestID = strings.Repeat("r", 256)
	in.Title = strings.Repeat("文", 128)
	in.Content = strings.Repeat("x", 16384)
	in.Button = " 查看消息 "
	in.Buttons = nil
	for i := 0; i < 8; i++ {
		in.Buttons = append(in.Buttons, MessageButton{
			ID: fmt.Sprintf("%s%d", strings.Repeat("a", 63), i), Text: " " + strings.Repeat("同", 40) + " ",
			Value: map[string]any{"payload": strings.Repeat("v", 1000)},
		})
	}
	in.Context = map[string]any{"payload": strings.Repeat("c", 7000)}
	out, err := a.sendInteractiveMessage(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := a.getInteractiveMessage(ctx, &GetInteractiveMessageIn{InteractionID: out.InteractionID})
	if err != nil || len(stored.Buttons) != 8 || stored.Buttons[0].Text != strings.Repeat("同", 40) || stored.Content != in.Content {
		t.Fatalf("documented boundary data was rejected or truncated: %+v %v", stored, err)
	}
	if api.entryText != "查看消息" || base.count != 1 {
		t.Fatalf("link entry was not independent of page actions: %q sends=%d", api.entryText, base.count)
	}
}

func TestInteractiveDefaultEntryIsNeutralAndIndependentOfActionText(t *testing.T) {
	a, ctx, base := portalFixture(t)
	api := &interactionValidationAPI{portalTestAPI: base}
	a.api = api
	in := validationInteractionInput()
	// A long page action must not become the four-character textcard entry.
	in.Buttons[0].Text = "请审核本次产品发布计划后选择是否继续"
	if _, err := a.sendInteractiveMessage(ctx, in); err != nil || api.entryText != "查看消息" {
		t.Fatalf("default entry is not independent of workflow actions: %q %v", api.entryText, err)
	}
}

func TestInteractiveNativeCardWrapperKeepsEnvelopeIdentityServerControlled(t *testing.T) {
	var sent map[string]json.RawMessage
	client := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			if r.URL.Query().Get("corpid") != "ww-test-corp" || r.URL.Query().Get("corpsecret") != "TEST-SECRET" {
				t.Error("template card did not use selected credential")
			}
			io.WriteString(w, documentedTokenResponse)
		case "/cgi-bin/message/send":
			if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
				t.Error("template card did not use authenticated send endpoint")
			}
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Error(err)
			}
			io.WriteString(w, documentedDeliveryResponse)
		default:
			t.Errorf("unexpected HTTP request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	app := &featureApplication{client: client}
	ctx := &portalTestContext{Context: context.Background(), fields: map[string]string{"corp_id": "ww-test-corp", "agent_id": "1000002", "secret": "TEST-SECRET"}}
	card := map[string]any{
		"card_type": "button_interaction", "task_id": "review-42",
		"main_title":  map[string]any{"title": "Release review"},
		"button_list": []any{map[string]any{"text": "Publish", "key": "publish_v2"}},
		// Even similarly named fields inside the opaque card cannot select targets.
		"touser": "@all", "agentid": 999, "msgtype": "text", "toparty": "1", "totag": "2",
	}
	before, _ := json.Marshal(card)
	out, err := app.sendTemplateCard(ctx, &SendTemplateCardIn{Users: []string{"alice", "bob"}, Card: card})
	if err != nil {
		t.Fatal(err)
	}
	if string(sent["touser"]) != `"alice|bob"` || string(sent["agentid"]) != "1000002" || string(sent["msgtype"]) != `"template_card"` {
		t.Fatalf("untrusted card changed envelope identity: %s", sent)
	}
	if len(sent) != 4 || compactJSONForTest(t, sent["template_card"]) != compactJSONForTest(t, before) {
		t.Fatalf("card fields were flattened, wrapped twice or lost: %s", sent)
	}
	after, _ := json.Marshal(card)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("sending mutated the caller's reusable card object")
	}
	if out.MsgID != "xxxx" || out.ResponseCode != "xyzxyz" || out.InvalidUser != "userid1|userid2" || out.UnlicensedUser != "userid3|userid4" || out.AllAccepted {
		t.Fatalf("native card wrapper lost update/partial-delivery information: %+v", out)
	}
	// The opaque vendor schema may add card variants without a plugin release.
	if _, err := app.sendTemplateCard(ctx, &SendTemplateCardIn{Users: []string{"alice"}, Card: map[string]any{"card_type": "future_vendor_card"}}); err != nil {
		t.Fatalf("nonempty vendor card_type was unnecessarily restricted: %v", err)
	}
	if compactJSONForTest(t, sent["template_card"]) != `{"card_type":"future_vendor_card"}` {
		t.Fatal("future vendor card variant was not preserved")
	}
}

func TestInteractiveNativeCardWrapperRejectsEmptyCardsAndBroadTargets(t *testing.T) {
	client := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid native card request reached HTTP: %s", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	app := &featureApplication{client: client}
	ctx := &portalTestContext{Context: context.Background(), fields: map[string]string{"corp_id": "ww-test-corp", "agent_id": "1000002", "secret": "TEST-SECRET"}}
	for _, card := range []map[string]any{nil, {}} {
		if _, err := app.sendTemplateCard(ctx, &SendTemplateCardIn{Users: []string{"alice"}, Card: card}); err == nil {
			t.Fatal("empty native card accepted")
		}
	}
	for _, users := range [][]string{nil, {"@all"}, {"alice|bob"}, {"alice\nbob"}} {
		if _, err := app.sendTemplateCard(ctx, &SendTemplateCardIn{Users: users, Card: map[string]any{"card_type": "button_interaction"}}); err == nil {
			t.Fatalf("unsafe native card recipients accepted: %q", users)
		}
	}
}

func TestInteractiveNativeCardRequiresCardTypeBeforeHTTP(t *testing.T) {
	client := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("missing or invalid card_type reached HTTP: %s", r.URL.Path)
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
		} else {
			io.WriteString(w, documentedDeliveryResponse)
		}
	})
	app := &featureApplication{client: client}
	ctx := &portalTestContext{Context: context.Background(), fields: map[string]string{"corp_id": "ww-test-corp", "agent_id": "1000002", "secret": "TEST-SECRET"}}
	for _, tc := range []struct {
		name string
		card map[string]any
	}{
		{"missing", map[string]any{"main_title": map[string]any{"title": "Review"}}},
		{"empty", map[string]any{"card_type": ""}},
		{"blank", map[string]any{"card_type": " \n\t"}},
		{"null", map[string]any{"card_type": nil}},
		{"number", map[string]any{"card_type": 1}},
		{"object", map[string]any{"card_type": map[string]any{"type": "button_interaction"}}},
		{"complete send envelope", map[string]any{
			"msgtype": "template_card", "touser": "alice", "agentid": 1000002,
			"template_card": map[string]any{"card_type": "button_interaction"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := app.sendTemplateCard(ctx, &SendTemplateCardIn{Users: []string{"alice"}, Card: tc.card})
			if err == nil || out != nil || !strings.Contains(err.Error(), "card_type") {
				t.Fatalf("expected a local card_type input error, got %+v %v", out, err)
			}
		})
	}
}
