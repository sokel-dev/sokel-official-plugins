package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
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
func TestGeneratedDispatchPreservesNestedInput(t *testing.T) {
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		b := body(t, r)
		m := b["variables"].(map[string]any)["input"].(map[string]any)
		if len(m) != 3 || m["priority"] != float64(0) || m["assigneeId"] != nil || m["description"] != "" {
			t.Fatal(m)
		}
		reply(w, `{"data":{"issueUpdate":{"success":true,"issue":{"id":"i"}}}}`)
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.ops) != 7 || len(h.fields) != 1 {
		t.Fatal("incomplete runtime contract")
	}
	ctx := &testCtx{context.Background()}
	for id, fn := range h.ops {
		if e := fn(ctx, json.RawMessage(`{broken`), &captureSink{}); e == nil {
			t.Fatal("invalid JSON accepted", id)
		}
	}
	sink := &captureSink{}
	if e := h.ops["update_issue"](ctx, json.RawMessage(`{"issue_id":"DEMO-1","changes":{"priority":0,"assignee_id":"","description":""}}`), sink); e != nil || sink.v == nil {
		t.Fatalf("dispatch %v %v", sink.v, e)
	}
}
