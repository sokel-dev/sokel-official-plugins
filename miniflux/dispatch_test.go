package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"testing"
)

type testCtx struct {
	context.Context
	key string
}

func (c *testCtx) Credential() map[string]string    { return map[string]string{"api_key": c.key} }
func (*testCtx) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unexpected file fetch") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unexpected file upload")
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(o contract.Operation, f plugin.Invoke) { h.calls[o.ID] = f }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func invoke(t *testing.T, c *client, op string, in any) map[string]any {
	t.Helper()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerWithClient(h, func(cred Cred) *client {
		if cred.APIKey != c.cred.APIKey {
			t.Fatal("credential lost")
		}
		return c
	})
	if len(h.calls) != 9 {
		t.Fatal("registration count")
	}
	b, _ := json.Marshal(in)
	sink := &testSink{}
	if e := h.calls[op](&testCtx{Context: context.Background(), key: c.cred.APIKey}, b, sink); e != nil {
		t.Fatalf("%s: %v", op, e)
	}
	return sink.vars
}
func TestAllGeneratedOperations(t *testing.T) {
	cases := []struct {
		op, path string
		in       any
		status   int
		body     string
	}{
		{"get_current_user", "/v1/me", map[string]any{}, 200, `{"id":1,"username":"reader","password":"hidden"}`},
		{"list_categories", "/v1/categories", map[string]any{}, 200, `[{"id":2,"title":"News"}]`},
		{"create_category", "/v1/categories", map[string]any{"title": "News"}, 201, `{"id":2,"title":"News"}`},
		{"list_feeds", "/v1/feeds", map[string]any{}, 200, `[{"id":3,"category":{"id":2},"password":"hidden"}]`},
		{"create_feed", "/v1/feeds", map[string]any{"feed_url": "http://rss.local/feed.xml", "category_id": "2"}, 201, `{"feed_id":3}`},
		{"refresh_feed", "/v1/feeds/3/refresh", map[string]any{"feed_id": "3"}, 204, ``},
		{"list_entries", "/v1/entries", map[string]any{}, 200, `{"entries":[]}`},
		{"get_entry", "/v1/entries/4", map[string]any{"entry_id": "4"}, 200, `{"id":4,"feed_id":3}`},
		{"update_entries", "/v1/entries", map[string]any{"entry_ids": []string{"4"}, "changes": map[string]any{"starred": false}}, 204, ``},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			c := newClient(Cred{APIKey: "key"})
			c.base = "http://reader.local"
			c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != tc.path {
					t.Fatalf("path %s", r.URL.Path)
				}
				return fixture(tc.status, tc.body), nil
			})}
			vars := invoke(t, c, tc.op, tc.in)
			b, _ := json.Marshal(vars)
			if len(b) == 0 {
				t.Fatal("empty output")
			}
		})
	}
}
