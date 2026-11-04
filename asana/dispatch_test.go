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
		if r.Method == "PUT" {
			b := bodyOf(t, r)["data"].(map[string]any)
			v, ok := b["assignee"]
			if b["completed"] != false || !ok || v != nil {
				t.Fatal("false or clear input lost", b)
			}
		}
		if r.URL.Path == "/api/1.0/workspaces" || r.URL.Path == "/api/1.0/projects" || r.URL.Path == "/api/1.0/tasks" && r.Method == "GET" {
			return `{"data":[]}`
		}
		return `{"data":{"gid":"1","name":"Task","permalink_url":"https://example.com"}}`
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.fields) != 1 || h.fields[0].Name != "access_token" || h.fields[0].Type != "secret" {
		t.Fatal(h.fields)
	}
	inputs := map[string]string{"health_check": `{}`,
		"list_workspaces": `{}`,
		"list_projects":   `{"workspace_gid": "1"}`,
		"list_tasks":      `{"project_gid": "1"}`,
		"get_task":        `{"task_gid": "1"}`,
		"create_task":     `{"workspace_gid": "1", "name": "Task"}`,
		"update_task":     `{"task_gid": "1", "changes": {"completed": false, "assignee_gid": ""}}`,
		"add_comment":     `{"task_gid": "1", "text": "Comment"}`,
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
			if id == "get_task" {
				raw, _ := json.Marshal(vars)
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				if m["task"].(map[string]any)["url"] != "https://example.com" {
					t.Fatal(m)
				}
			}
			if e := h.ops[id](c, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
