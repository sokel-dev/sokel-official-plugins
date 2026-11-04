package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
)

type captureHost struct {
	ops    map[string]plugin.Invoke
	auth   contract.AuthMeta
	fields []contract.Field
	events []contract.Event
}

func (h *captureHost) Register(op contract.Operation, fn plugin.Invoke)       { h.ops[op.ID] = fn }
func (h *captureHost) SetCredentialContract(f []contract.Field)               { h.fields = f }
func (h *captureHost) SetAuthFlow(m contract.AuthMeta, _ plugin.AuthHandlers) { h.auth = m }
func (h *captureHost) DeclareEvent(e contract.Event)                          { h.events = append(h.events, e) }

type captureSink struct{ v any }

func (s *captureSink) Vars(v any) { s.v = v }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}

type fileCtx struct {
	context.Context
	fetched, uploaded int
	fail              bool
}

func (c *fileCtx) Credential() map[string]string {
	return map[string]string{"access_token": "test-secret"}
}
func (c *fileCtx) Fetch(*plugin.File) ([]byte, error) {
	c.fetched++
	if c.fail {
		return nil, fmt.Errorf("test-secret must not leak")
	}
	return []byte("abc"), nil
}
func (c *fileCtx) Upload(n, m string, b []byte) (*plugin.File, error) {
	c.uploaded++
	if c.fail {
		return nil, fmt.Errorf("test-secret must not leak")
	}
	return &plugin.File{ID: "stored", Name: n, Mime: m, Size: int64(len(b))}, nil
}
func (c *fileCtx) UploadReader(n, m string, r io.Reader) (*plugin.File, error) {
	b, e := io.ReadAll(r)
	if e != nil {
		return nil, e
	}
	return c.Upload(n, m, b)
}
func TestGeneratedDispatchAndContracts(t *testing.T) {
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Query().Get("alt") == "media" || strings.HasSuffix(r.URL.Path, "/export") {
			_, _ = w.Write([]byte("abc"))
			return
		}
		reply(w, `{"id":"f","name":"a.txt","mimeType":"text/plain","email":"test@example.com","files":[],"items":[],"values":[],"calendars":{"primary":{"busy":[]}},"updates":{"updatedCells":1}}`)
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	RegisterAuth(h)
	DeclareEvents(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if h.auth.Provider != "google" || len(h.auth.Scopes) < 2 || len(h.events) != 1 || len(h.fields) < 3 {
		t.Fatal("incomplete runtime contract")
	}
	ctx := &fileCtx{Context: context.Background()}
	if apiOf(ctx).token != "test-secret" {
		t.Fatal("credential binding")
	}
	inputs := map[string]string{"list_files": "{}", "upload_file": "{\"file\":{\"id\":\"f\",\"name\":\"a.txt\",\"mime\":\"text/plain\",\"size\":3}}", "download_file": "{\"file_id\":\"f\"}", "export_file": "{\"file_id\":\"f\",\"mime_type\":\"application/pdf\",\"filename\":\"report.pdf\"}", "health_check": "{}"}
	if len(h.ops) != len(inputs) {
		t.Fatalf("operation count %d", len(h.ops))
	}
	for id, input := range inputs {
		t.Run(id, func(t *testing.T) {
			sink := &captureSink{}
			if e := h.ops[id](ctx, json.RawMessage(input), sink); e != nil {
				t.Fatal(e)
			}
			if sink.v == nil {
				t.Fatal("no typed output")
			}
			if e := h.ops[id](ctx, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("invalid JSON input accepted")
			}
		})
	}
}
func TestDisabledSourceHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := &sourceFixture{Ctx: contextOnly{Context: ctx}, fields: map[string]string{}}
	if e := runSource(source); e != context.Canceled {
		t.Fatalf("cancellation: %v", e)
	}
}

func (h *captureHost) DeclareEventsCommon([]contract.Field, []string) {}
