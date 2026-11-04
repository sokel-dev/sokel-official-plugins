package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"strings"
	"testing"
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
func TestGeneratedDispatchAndCredential(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if identity(t, w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sendMail") {
			m := body(t, r)["message"].(map[string]any)
			if m["bccRecipients"].([]any)[0].(map[string]any)["emailAddress"].(map[string]any)["address"] != "hidden@example.com" {
				t.Fatal(m)
			}
			w.WriteHeader(202)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/$value") {
			w.Write([]byte("abc"))
			return
		}
		if r.Method == "POST" {
			w.WriteHeader(201)
		}
		reply(w, `{"id":"result","value":[],"@odata.type":"#microsoft.graph.fileAttachment","name":"a.txt","size":3,"contentType":"text/plain","body":{"contentType":"text","content":"hi"}}`)
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.fields) != 1 || h.fields[0].Name != "access_token" || h.fields[0].Type != "secret" {
		t.Fatal(h.fields)
	}
	ctx := &fileCtx{Context: context.Background()}
	inputs := map[string]string{"health_check": `{}`, "list_teams": `{}`, "list_channels": `{"team_id":"t"}`, "list_chats": `{"limit":2}`, "list_channel_messages": `{"team_id":"t","channel_id":"c","limit":2}`, "list_replies": `{"team_id":"t","channel_id":"c","message_id":"m","limit":2}`, "list_chat_messages": `{"chat_id":"x","limit":2}`, "send_channel_message": `{"team_id":"t","channel_id":"c","reply_to_id":"m","content":"hi","content_type":"html"}`, "send_chat_message": `{"chat_id":"x","content":"hi"}`}
	if len(h.ops) != len(inputs) {
		t.Fatal("operation count", len(h.ops))
	}
	for id, input := range inputs {
		t.Run(id, func(t *testing.T) {
			s := &captureSink{}
			if e := h.ops[id](ctx, json.RawMessage(input), s); e != nil || s.v == nil {
				t.Fatal(e, s.v)
			}
			if e := h.ops[id](ctx, json.RawMessage(`{broken`), s); e == nil {
				t.Fatal("malformed JSON accepted")
			}
		})
	}
}
