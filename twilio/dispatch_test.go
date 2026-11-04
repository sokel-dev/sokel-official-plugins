package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"testing"
)

type captureHost struct {
	ops    map[string]plugin.Invoke
	fields []contract.Field
}

func (h *captureHost) Register(o contract.Operation, f plugin.Invoke)   { h.ops[o.ID] = f }
func (h *captureHost) SetCredentialContract(f []contract.Field)         { h.fields = f }
func (*captureHost) SetAuthFlow(contract.AuthMeta, plugin.AuthHandlers) {}
func (*captureHost) DeclareEvent(contract.Event)                        {}
func (*captureHost) DeclareEventsCommon([]contract.Field, []string)     {}

type testCtx struct {
	context.Context
	cred map[string]string
}

func (c *testCtx) Credential() map[string]string    { return c.cred }
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unexpected file fetch") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unexpected upload")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unexpected upload")
}

type captureSink struct{ vars map[string]any }

func (s *captureSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}
func TestGeneratedDispatch(t *testing.T) {
	cases := []struct{ op, input, fixture string }{
		{"get_account", `{}`, "account"},
		{"send_message", `{"message":{"to":"+15558675310","from":"+15017122661","body":"Hi","smart_encoded":false}}`, "send"},
		{"get_message", `{"message_sid":"SMb7c0a2ce80504485a6f653a7110836f5"}`, "message"},
		{"list_messages", `{}`, "messages"},
		{"get_incoming_number", `{"number_sid":"` + numberSID + `"}`, "number"},
		{"list_incoming_numbers", `{}`, "numbers"},
		{"list_message_media", `{"message_sid":"` + messageSID + `"}`, "media"},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			c := fake(t, fixture(t, tc.fixture), nil)
			h := &captureHost{ops: map[string]plugin.Invoke{}}
			RegisterCredential(h)
			registerWithClient(h, func(cred Cred) *client {
				if cred.AccountSid != accountSID || cred.AuthToken != "fixture_secret" {
					t.Fatal("credential binding")
				}
				return c
			})
			if len(h.ops) != 7 || len(h.fields) != 2 {
				t.Fatal("registration")
			}
			sink := &captureSink{}
			ctx := &testCtx{context.Background(), map[string]string{"account_sid": accountSID, "auth_token": "fixture_secret"}}
			if e := h.ops[tc.op](ctx, json.RawMessage(tc.input), sink); e != nil || len(sink.vars) == 0 {
				t.Fatalf("%v %v", sink.vars, e)
			}
			if tc.op == "get_account" {
				raw, _ := json.Marshal(sink.vars)
				var v map[string]any
				json.Unmarshal(raw, &v)
				a := v["account"].(map[string]any)
				if _, ok := a["auth_token"]; ok {
					t.Fatal("secret leaked")
				}
				if a["sid"] != accountSID {
					t.Fatal("account SID lost")
				}
			}
			if e := h.ops[tc.op](ctx, json.RawMessage(`{bad`), sink); e == nil {
				t.Fatal("malformed input")
			}
		})
	}
}
