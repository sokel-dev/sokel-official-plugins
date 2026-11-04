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
	a := fakeAPI(func(r *http.Request) (int, string) {
		if r.Method == "HEAD" {
			return 200, ""
		}
		if r.URL.Path == "/storage/v1/object/sign/b/a.txt" {
			return 200, `{"signedURL":"/object/sign/b/a.txt?token=x"}`
		}
		return 200, `[{"id":"1","name":"a.txt"}]`
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.fields) != 2 {
		t.Fatal(h.fields)
	}
	c := &testCtx{Context: context.Background()}
	t.Setenv("SUPABASE_URL", "https://example.supabase.co")
	if apiOf(c).key != "test-key" || apiOf(c).token != "test-token" {
		t.Fatal("credential binding")
	}
	inputs := map[string]string{"select_rows": `{"table":"items","filters":[{"column":"id","operator":"eq","value":"1"}]}`, "insert_rows": `{"table":"items","rows":[{"id":1}]}`, "upsert_rows": `{"table":"items","rows":[{"id":1}],"on_conflict":"id"}`, "update_rows": `{"table":"items","filters":[{"column":"id","operator":"eq","value":"1"}],"values":{"active":false}}`, "delete_rows": `{"table":"items","filters":[{"column":"id","operator":"eq","value":"1"}]}`, "call_function": `{"function":"f","arguments":{"flag":false}}`, "list_objects": `{"bucket":"b"}`, "create_signed_url": `{"bucket":"b","path":"a.txt"}`, "health_check": `{"table":"items"}`}
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
