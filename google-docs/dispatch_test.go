package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"testing"
)

type captureHost struct {
	ops    map[string]plugin.Invoke
	auth   contract.AuthMeta
	fields []contract.Field
}

func (h *captureHost) Register(op contract.Operation, fn plugin.Invoke)       { h.ops[op.ID] = fn }
func (h *captureHost) SetCredentialContract(f []contract.Field)               { h.fields = f }
func (h *captureHost) SetAuthFlow(m contract.AuthMeta, _ plugin.AuthHandlers) { h.auth = m }
func (h *captureHost) DeclareEvent(contract.Event)                            {}
func (h *captureHost) DeclareEventsCommon([]contract.Field, []string)         {}

type captureSink struct{ v any }

func (s *captureSink) Vars(v any) { s.v = v }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}

type testCtx struct{ context.Context }

func (c *testCtx) Credential() map[string]string {
	return map[string]string{"access_token": "test-token", "api_key": "test-key"}
}
func (c *testCtx) Fetch(*plugin.File) ([]byte, error)                  { panic("unexpected file fetch") }
func (c *testCtx) Upload(string, string, []byte) (*plugin.File, error) { panic("unexpected upload") }
func (c *testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("unexpected upload")
}
func TestGeneratedDispatchAndCredential(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		return `{"documentId":"doc","title":"Doc","email":"reader@example.com","tabs":[],"replies":[{"replaceAllText":{"occurrencesChanged":0}}],"writeControl":{"requiredRevisionId":"r2"}}`
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	RegisterAuth(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if h.auth.Provider != "google" || len(h.auth.Scopes) != 2 {
		t.Fatal(h.auth)
	}
	c := &testCtx{Context: context.Background()}
	if apiOf(c).token != "test-token" {
		t.Fatal("credential binding")
	}
	inputs := map[string]string{"get_document": `{"document_id":"doc"}`, "create_document": `{"title":"Doc"}`, "insert_text": `{"document_id":"doc","tab_id":"t1","text":"Hello","index":1}`, "replace_text": `{"document_id":"doc","tab_id":"t1","find":"Hello","replacement":""}`, "delete_text": `{"document_id":"doc","tab_id":"t1","start_index":1,"end_index":2}`, "health_check": `{}`}
	if len(h.ops) != len(inputs) {
		t.Fatal(len(h.ops))
	}
	for id, in := range inputs {
		t.Run(id, func(t *testing.T) {
			s := &captureSink{}
			if e := h.ops[id](c, json.RawMessage(in), s); e != nil || s.v == nil {
				t.Fatal(e, s.v)
			}
			if e := h.ops[id](c, json.RawMessage(`{broken`), s); e == nil {
				t.Fatal("malformed JSON accepted")
			}
		})
	}
}
