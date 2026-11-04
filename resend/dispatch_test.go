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
	cases := []struct{ op, input, reply string }{
		{"send_email", `{"email":{"from":"a@example.com","to":["b@example.com"],"subject":"Hi","text":"hello"}}`, fixture(t, "send")},
		{"get_email", `{"email_id":"` + emailID + `"}`, fixture(t, "get")},
		{"cancel_email", `{"email_id":"` + emailID + `"}`, `{"id":"` + emailID + `"}`},
		{"get_received_email", `{"email_id":"` + emailID + `"}`, fixture(t, "get-received")},
		{"list_emails", `{}`, fixture(t, "list")},
		{"list_received_emails", `{}`, fixture(t, "list-received")},
		{"get_domain", `{"domain_id":"` + domainID + `"}`, fixture(t, "domain")},
		{"list_domains", `{}`, fixture(t, "domains")},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			c := fake(t, tc.reply, nil)
			h := &captureHost{ops: map[string]plugin.Invoke{}}
			RegisterCredential(h)
			registerWithClient(h, func(cred Cred) *client {
				if cred.APIKey != "re_fixture_secret" {
					t.Fatal("credential binding")
				}
				return c
			})
			if len(h.ops) != 8 || len(h.fields) != 1 {
				t.Fatal("registration")
			}
			sink := &captureSink{}
			ctx := &testCtx{context.Background(), map[string]string{"api_key": "re_fixture_secret"}}
			if e := h.ops[tc.op](ctx, json.RawMessage(tc.input), sink); e != nil || len(sink.vars) == 0 {
				t.Fatalf("%v %v", sink.vars, e)
			}
			if tc.op == "get_received_email" {
				raw, _ := json.Marshal(sink.vars)
				var v map[string]any
				json.Unmarshal(raw, &v)
				email := v["email"].(map[string]any)
				if _, exists := email["text"]; exists {
					t.Fatal("null text must stay absent")
				}
				if email["html_format"] != "data_uri" {
					t.Fatal("lost inline format")
				}
			}
			if e := h.ops[tc.op](ctx, json.RawMessage(`{bad`), sink); e == nil {
				t.Fatal("malformed input")
			}
		})
	}
}
