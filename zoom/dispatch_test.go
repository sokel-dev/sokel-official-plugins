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
	cases := []struct {
		id, method, path, input, response string
		status                            int
	}{
		{"health_check", "GET", "/v2/users/me", `{}`, fixture(t, "user"), 200},
		{"get_user", "GET", "/v2/users/person@example.invalid", `{"user_id":"person@example.invalid"}`, fixture(t, "user"), 200},
		{"list_users", "GET", "/v2/users", `{"status":"active","page_size":20}`, `{"users":[{"id":"person"}],"next_page_token":"token"}`, 200},
		{"list_meetings", "GET", "/v2/users/me/meetings", `{"type":"upcoming"}`, `{"meetings":[` + fixture(t, "meeting") + `],"next_page_token":""}`, 200},
		{"get_meeting", "GET", "/v2/meetings/123", `{"meeting_id":"123"}`, fixture(t, "meeting"), 200},
		{"create_meeting", "POST", "/v2/users/me/meetings", `{"topic":"Review","start_time":"2030-01-01T00:00:00Z","duration":30}`, fixture(t, "meeting"), 201},
		{"update_meeting", "PATCH", "/v2/meetings/123", `{"meeting_id":"123","changes":{"agenda":"","settings":{"mute_upon_entry":false}}}`, ``, 204},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			a, _ := fakeAPI(t, c.method, c.path, c.status, c.response, nil)
			h := &captureHost{ops: map[string]plugin.Invoke{}}
			RegisterCredential(h)
			registerOperations(h, func(plugin.Ctx) *api { return a })
			if len(h.ops) != 7 || len(h.fields) != 1 {
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
