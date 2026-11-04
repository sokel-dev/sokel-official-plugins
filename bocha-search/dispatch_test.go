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
		if r.URL.Path != "/v1/web-search" {
			t.Fatal(r.URL)
		}
		if bodyOf(t, r)["summary"] != false {
			t.Fatal("false input lost")
		}
		return `{"data":{"webPages":{"value":[{"url":"https://example.com","siteName":"Site"}]}}}`
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.fields) != 1 || h.fields[0].Name != "access_token" || h.fields[0].Type != "secret" {
		t.Fatal(h.fields)
	}
	inputs := map[string]string{"verify_credential": `{"probe_query":"test"}`,
		"web_search": `{"query": "q", "options": {"summary": false}}`,
	}
	if len(h.ops) != len(inputs) {
		t.Fatal(len(h.ops))
	}
	c := &testCtx{Context: context.Background()}
	for id, in := range inputs {
		t.Run(id, func(t *testing.T) {
			sink := &captureSink{}
			if e := h.ops[id](c, json.RawMessage(in), sink); e != nil || sink.v == nil {
				t.Fatal(e, sink.v)
			}
			vars := contract.StructToVars(sink.v)
			if len(vars) == 0 {
				t.Fatal("empty platform output")
			}
			if id == "web_search" {
				raw, _ := json.Marshal(vars)
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				if m["results"].([]any)[0].(map[string]any)["site_name"] != "Site" {
					t.Fatal(m)
				}
			}
			if e := h.ops[id](c, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
