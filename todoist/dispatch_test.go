package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
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

type testCtx struct{ context.Context }

func (*testCtx) Credential() map[string]string      { return nil }
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, fmt.Errorf("unexpected file access") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, fmt.Errorf("unexpected file upload")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, fmt.Errorf("unexpected file upload")
}

type captureSink struct{ v any }

func (s *captureSink) Vars(v any) { s.v = v }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}
func TestAllGeneratedOperations(t *testing.T) {
	cases := []struct{ id, method, path, input, response string }{
		{"health_check", "GET", "/api/v1/user", `{}`, `{"id":"person"}`},
		{"list_projects", "GET", "/api/v1/projects", `{}`, `{"results":[` + fixture(t, "project") + `],"next_cursor":null}`},
		{"get_project", "GET", "/api/v1/projects/PROJ", `{"project_id":"PROJ"}`, fixture(t, "project")},
		{"create_project", "POST", "/api/v1/projects", `{"name":"Plan","details":{"is_favorite":false,"parent_id":"PROJ","view_style":"board"}}`, fixture(t, "project")},
		{"list_tasks", "GET", "/api/v1/tasks", `{"project_id":"PROJ","section_id":"SEC","parent_id":"TASK","label":"next action"}`, `{"results":[` + fixture(t, "task") + `],"next_cursor":null}`},
		{"get_task", "GET", "/api/v1/tasks/TASK", `{"task_id":"TASK"}`, fixture(t, "task")},
		{"create_task", "POST", "/api/v1/tasks", `{"content":"Buy milk","details":{"project_id":"PROJ","due_date":"2030-01-02","labels":["next action"]}}`, fixture(t, "task")},
		{"update_task", "POST", "/api/v1/tasks/TASK", `{"task_id":"TASK","changes":{"content":"Updated","priority":2}}`, fixture(t, "task")},
		{"close_task", "POST", "/api/v1/tasks/TASK/close", `{"task_id":"TASK"}`, `null`},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			a, _ := fakeAPI(t, c.method, c.path, 200, c.response, nil)
			h := &captureHost{ops: map[string]plugin.Invoke{}}
			RegisterCredential(h)
			registerOperations(h, func(plugin.Ctx) *api { return a })
			if len(h.ops) != 9 || len(h.fields) != 1 {
				t.Fatal("registration")
			}
			sink := &captureSink{}
			if e := h.ops[c.id](&testCtx{context.Background()}, json.RawMessage(c.input), sink); e != nil || sink.v == nil {
				t.Fatalf("dispatch %v %v", sink.v, e)
			}
			if e := h.ops[c.id](&testCtx{context.Background()}, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
