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
func TestGeneratedDispatchAndNestedPlatformFields(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.URL.Path == "/services/v4/open/baseinfoV3" {
			return `{"error_code":0,"result":{"id":9007199254740993,"name":"Company","legalPersonName":"Person","estiblishTime":1700000000000}}`
		}
		if r.URL.Query().Get("pageNum") != "2" || r.URL.Query().Get("pageSize") != "1" {
			t.Fatal(r.URL)
		}
		return `{"error_code":0,"result":{"total":2,"items":[{"changeItem":"Name","contentBefore":"Old","contentAfter":"New","changeTime":"2026-01-01"}]}}`
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.fields) != 1 || h.fields[0].Type != "secret" {
		t.Fatal(h.fields)
	}
	inputs := map[string]string{"get_company": `{"keyword":"Company"}`, "list_company_changes": `{"keyword":"Company","page":{"page":2,"page_size":1}}`}
	if len(h.ops) != 2 {
		t.Fatal(len(h.ops))
	}
	c := &testCtx{Context: context.Background()}
	for id, input := range inputs {
		t.Run(id, func(t *testing.T) {
			s := &captureSink{}
			if e := h.ops[id](c, json.RawMessage(input), s); e != nil || s.v == nil {
				t.Fatal(e, s.v)
			}
			raw, _ := json.Marshal(contract.StructToVars(s.v))
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			if id == "get_company" {
				company := m["company"].(map[string]any)
				if company["id"] != "9007199254740993" || company["legal_representative"] != "Person" || company["established_at_ms"] != "1700000000000" {
					t.Fatal(m)
				}
			} else {
				row := m["changes"].([]any)[0].(map[string]any)
				if row["item"] != "Name" || row["after"] != "New" || row["changed_at"] != "2026-01-01" {
					t.Fatal(m)
				}
			}
			if e := h.ops[id](c, json.RawMessage(`{bad`), s); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
