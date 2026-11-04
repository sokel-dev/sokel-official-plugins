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
		{"health_check", "GET", "/v1/me", `{}`, `{"id":"person"}`},
		{"get_file_metadata", "GET", "/v1/files/FILE/meta", `{"file_key":"FILE"}`, `{"file":{"name":"Design","editorType":"figma","creator":{"id":"person","handle":"Example"},"last_touched_at":"2030-01-01T00:00:00Z"}}`},
		{"get_nodes", "GET", "/v1/files/FILE/nodes", `{"file_key":"FILE","node_ids":["1:2","9:9"],"version":"123"}`, fixture(t, "nodes")},
		{"render_images", "GET", "/v1/images/FILE", `{"file_key":"FILE","node_ids":["1:2","9:9"],"format":"svg","options":{"use_absolute_bounds":false}}`, fixture(t, "images")},
		{"list_team_folders", "GET", "/v2/teams/123/folders", `{"team_id":"123"}`, fixture(t, "folders")},
		{"list_subfolders", "GET", "/v2/folders/123/folders", `{"folder_id":"123"}`, `{"name":"Folder","folders":[]}`},
		{"list_folder_files", "GET", "/v2/folders/123/files", `{"folder_id":"123"}`, `{"name":"Folder","files":[{"key":"FILE","name":"Design","last_modified":"2030-01-01T00:00:00Z"}]}`},
		{"list_comments", "GET", "/v1/files/FILE/comments", `{"file_key":"FILE"}`, `{"comments":[` + fixture(t, "comment") + `]}`},
		{"create_comment", "POST", "/v1/files/FILE/comments", `{"file_key":"FILE","message":"Review ready"}`, fixture(t, "comment")},
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
			if c.id == "get_file_metadata" {
				b, _ := json.Marshal(sink.v)
				var flat map[string]any
				json.Unmarshal(b, &flat)
				file := flat["file"].(map[string]any)
				if file["editor_type"] != "figma" {
					t.Fatalf("camelCase mapping lost: %s", b)
				}
			}
			if e := h.ops[c.id](&testCtx{context.Background()}, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
