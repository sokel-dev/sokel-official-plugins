package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/zendesk/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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

type captureSink struct{ v any }

func (s *captureSink) Vars(v any) { s.v = v }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}

type testCtx struct{ context.Context }

func (*testCtx) Credential() map[string]string                       { return nil }
func (*testCtx) Fetch(*plugin.File) ([]byte, error)                  { panic("unexpected fetch") }
func (*testCtx) Upload(string, string, []byte) (*plugin.File, error) { panic("unexpected upload") }
func (*testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("unexpected upload")
}
func dispatchAll(t *testing.T, a *api, inputs map[string]string, check func(string, map[string]any)) {
	t.Helper()
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.ops) != len(inputs) || len(h.fields) == 0 {
		t.Fatal("incomplete registration")
	}
	found := false
	for _, f := range h.fields {
		if f.Type == "secret" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing secret control")
	}
	for id, raw := range inputs {
		t.Run(id, func(t *testing.T) {
			fn := h.ops[id]
			if fn == nil {
				t.Fatal("missing operation")
			}
			sink := &captureSink{}
			if e := fn(&testCtx{context.Background()}, json.RawMessage(raw), sink); e != nil || sink.v == nil {
				t.Fatalf("%s: %v %#v", id, e, sink.v)
			}
			vars := contract.StructToVars(sink.v)
			wire, _ := json.Marshal(vars)
			var m map[string]any
			_ = json.Unmarshal(wire, &m)
			check(id, m)
			if e := fn(&testCtx{context.Background()}, json.RawMessage(`{broken`), sink); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errors.New("test-secret") }
func (badReader) Close() error             { return nil }
func TestClientBoundaries(t *testing.T) {
	for _, code := range []int{301, 400, 401, 403, 409, 412, 422, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			calls := 0
			a := fakeAPI(t, func(r *http.Request) (int, string) { calls++; return code, `{"error":"test-secret"}` })
			var out map[string]any
			_, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out)
			if e == nil || strings.Contains(e.Error(), "test-secret") || calls != 1 {
				t.Fatal(e, calls)
			}
		})
	}
	for _, body := range []string{`{bad`, `{} {}`, strings.Repeat("x", int(jsonLimit)+1)} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		var out map[string]any
		if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil {
			t.Fatal("bad body accepted")
		}
	}
	a := fakeAPI(t, func(*http.Request) (int, string) { t.Fatal("request should be rejected locally"); return 200, `{}` })
	var out map[string]any
	for _, path := range []string{"relative", "//evil.example/", "/bad?q=x", "/bad#fragment"} {
		if _, e := a.request(context.Background(), "GET", path, nil, nil, &out); e == nil {
			t.Fatal(path)
		}
	}
	if _, e := a.request(context.Background(), "POST", "/probe", nil, map[string]any{"body": strings.Repeat("x", int(jsonLimit)+1)}, &out); e == nil {
		t.Fatal("oversize body accepted")
	}
	if _, e := a.request(context.Background(), "POST", "/probe", nil, make(chan int), &out); e == nil {
		t.Fatal("invalid JSON accepted")
	}
	a = newAPI(testCred())
	if a.client.Timeout != 55*time.Second {
		t.Fatal("missing timeout")
	}
	a.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.request(ctx, "GET", "/probe", nil, nil, &out); e == nil {
		t.Fatal("cancel ignored")
	}
	a.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: badReader{}, Header: http.Header{}, Request: r}, nil
	})
	if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal(e)
	}
	calls := 0
	a.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example/token"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, e := a.request(context.Background(), "GET", "/probe", nil, nil, &out); e == nil || calls != 1 {
		t.Fatal("redirect followed", e, calls)
	}
}
func TestGeneratedDispatch(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/api/v2/users/me.json":
			return 200, `{"user":{"id":123,"role":"admin"}}`
		case "/api/v2/search.json":
			if r.URL.Query().Get("query") != "type:ticket status:open" {
				t.Fatal(r.URL)
			}
			return 200, `{"results":[{"id":123,"result_type":"ticket"}],"count":1500,"next_page":"https://example.zendesk.com/api/v2/search.json?page=2"}`
		case "/api/v2/tickets/123/comments.json":
			return 200, `{"comments":[{"id":9007199254740993,"author_id":456,"body":"Text","public":false}],"meta":{"has_more":false,"after_cursor":null}}`
		case "/api/v2/tickets.json":
			if r.Method == "GET" {
				if r.URL.Query().Get("page[size]") != "2" || r.URL.Query().Get("page[after]") != "a+/=" {
					t.Fatal(r.URL)
				}
				return 200, `{"tickets":[{"id":123,"requester_id":456,"assignee_id":null}],"meta":{"has_more":true,"after_cursor":"next"}}`
			}
			b := bodyOf(t, r)["ticket"].(map[string]any)
			if b["comment"].(map[string]any)["public"] != false {
				t.Fatal(b)
			}
			return 201, `{"ticket":{"id":123}}`
		case "/api/v2/tickets/123.json":
			if r.Method == "PUT" {
				b := bodyOf(t, r)["ticket"].(map[string]any)
				if _, ok := b["comment"]; !ok {
					v, has := b["assignee_id"]
					if !has || v != nil || b["safe_update"] != true || b["updated_stamp"] != "2026-11-04T01:02:03Z" {
						t.Fatal(b)
					}
				}
			}
			return 200, `{"ticket":{"id":123,"status":"open","updated_at":"2026-11-04T01:02:03Z"}}`
		}
		t.Fatal(r.URL)
		return 500, `{}`
	})
	dispatchAll(t, a, map[string]string{"health_check": `{}`, "list_tickets": `{"limit":2,"cursor":"a+/="}`, "search_tickets": `{"query":"status:open"}`, "get_ticket": `{"ticket_id":"123"}`, "create_ticket": `{"subject":"Help","body":"Text","requester_id":"456","public":false}`, "update_ticket": `{"ticket_id":"123","changes":{"assignee_id":"","tags":[]},"updated_stamp":"2026-11-04T01:02:03Z"}`, "list_comments": `{"ticket_id":"123"}`, "add_comment": `{"ticket_id":"123","body":"Text","public":false}`}, func(id string, v map[string]any) {
		if id == "list_comments" && v["comments"].([]any)[0].(map[string]any)["id"] != "9007199254740993" {
			t.Fatal(v)
		}
	})
}
func TestPaginationAndErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"tickets":[],"meta":{}}`, `{"tickets":null,"meta":{"has_more":false}}`, `{"tickets":[],"meta":{"has_more":true,"after_cursor":"same"}}`, `{"tickets":[{}],"meta":{"has_more":false}}`} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := a.list_tickets(context.Background(), &ListTicketsIn{Cursor: "same"}); e == nil {
			t.Fatal(body)
		}
	}
	a := fakeAPI(t, func(*http.Request) (int, string) {
		return 200, `{"tickets":[],"meta":{"has_more":false,"after_cursor":null}}`
	})
	if v, e := a.list_tickets(context.Background(), &ListTicketsIn{}); e != nil || v.HasMore || v.Tickets == nil {
		t.Fatal(v, e)
	}
	for _, next := range []string{"https://evil.example/api/v2/search.json?page=2", "https://example.zendesk.com/api/v2/users.json?page=2", "https://example.zendesk.com/api/v2/search.json?page=1"} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, `{"results":[],"count":1,"next_page":"` + next + `"}` })
		if _, e := a.search_tickets(context.Background(), &SearchTicketsIn{Query: "x"}); e == nil {
			t.Fatal(next)
		}
	}
	a = fakeAPI(t, func(*http.Request) (int, string) {
		return 200, `{"results":[],"count":1500,"next_page":"https://example.zendesk.com/api/v2/search.json?page=11"}`
	})
	if v, e := a.search_tickets(context.Background(), &SearchTicketsIn{Query: "x", Page: 10}); e != nil || v.HasMore || !v.Truncated {
		t.Fatal(v, e)
	}
	for _, body := range []string{`{"ticket":{}}`, `{"error":"test-secret"}`} {
		a = fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := a.get_ticket(context.Background(), &GetTicketIn{TicketID: "123"}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(e)
		}
	}
	a = fakeAPI(t, func(*http.Request) (int, string) { return 200, `{"user":{"id":1,"role":"end-user"}}` })
	if h, e := a.health_check(context.Background(), &HealthCheckIn{}); e != nil || h.OK {
		t.Fatal(h, e)
	}
}
func TestInputValidation(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) (int, string) { t.Fatal("invalid input reached provider"); return 200, `{}` })
	for _, sub := range []string{"evil.example", "-bad", "https://example.zendesk.com"} {
		a.cred.Subdomain = sub
		if _, e := a.origin(); e == nil {
			t.Fatal(sub)
		}
	}
	a.cred = testCred()
	if _, e := a.get_ticket(context.Background(), &GetTicketIn{TicketID: "../123"}); e == nil {
		t.Fatal("ID")
	}
	if _, e := a.search_tickets(context.Background(), &SearchTicketsIn{Query: "x", Page: 11}); e == nil {
		t.Fatal("page")
	}
	if _, e := a.add_comment(context.Background(), &AddCommentIn{TicketID: "123", Body: "x", UpdatedStamp: "yesterday"}); e == nil {
		t.Fatal("timestamp")
	}
	for _, change := range []schema.TicketChanges{{}, {Status: ptr("closed")}, {Priority: ptr("BAD")}, {Subject: ptr("")}, {AssigneeID: ptr("../")}} {
		if _, e := a.update_ticket(context.Background(), &UpdateTicketIn{TicketID: "123", Changes: change}); e == nil {
			t.Fatal("change")
		}
	}
	a.cred.APIToken = "bad\nsecret"
	if h, e := a.health_check(context.Background(), &HealthCheckIn{}); e != nil || h.OK {
		t.Fatal(h, e)
	}
}
func ptr(s string) *string { return &s }
