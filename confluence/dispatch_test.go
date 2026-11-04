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
		if r.Method == "GET" {
			reply(w, `{"id":"20","status":"current","version":{"number":1}}`)
			return
		}
		b := body(t, r)
		if b["version"].(map[string]any)["number"] != float64(2) || b["version"].(map[string]any)["message"] != "change" {
			t.Fatal(b)
		}
		reply(w, `{"id":"20","status":"current","version":{"number":2}}`)
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.ops) != 7 || len(h.fields) != 4 {
		t.Fatal("incomplete runtime contract")
	}
	ctx := &testCtx{context.Background()}
	for id, fn := range h.ops {
		if e := fn(ctx, json.RawMessage(`{broken`), &captureSink{}); e == nil {
			t.Fatal("invalid JSON accepted", id)
		}
	}
	sink := &captureSink{}
	if e := h.ops["update_page"](ctx, json.RawMessage(`{"page_id":"20","expected_version":1,"title":"T","body":"<p>x</p>","version_message":"change"}`), sink); e != nil || sink.v == nil {
		t.Fatalf("dispatch %v %v", sink.v, e)
	}
}
