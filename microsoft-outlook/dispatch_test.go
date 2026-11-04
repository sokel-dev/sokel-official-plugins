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
	inputs := map[string]string{"health_check": `{}`, "list_messages": `{"folder_id":"inbox","limit":2,"filter":"isRead eq false"}`, "get_message": `{"message_id":"m"}`, "send_mail": `{"to":["a@example.com"],"bcc":["hidden@example.com"],"subject":"s","body":"b"}`, "list_attachments": `{"message_id":"m"}`, "download_attachment": `{"message_id":"m","attachment_id":"a"}`, "calendar_view": `{"start":"2026-11-03T00:00:00Z","end":"2026-11-04T00:00:00Z"}`, "create_event": `{"subject":"s","start":"2026-11-03T00:00:00Z","end":"2026-11-04T00:00:00Z","attendees":["a@example.com"],"transaction_id":"txn"}`}
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
