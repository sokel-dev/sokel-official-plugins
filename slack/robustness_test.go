package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mockClient(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return &client{base: s.URL, token: "secret-token-do-not-log", http: s.Client(), uploadOrigin: s.URL}
}
func jsonResponse(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, s)
}
func TestHealthRepliesAndDefaults(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			if r.Method != "POST" {
				t.Error("auth.test must use POST")
			}
			jsonResponse(w, `{"ok":true,"team_id":"T123","user_id":"U123"}`)
		case "/conversations.replies":
			if q := r.URL.Query(); q.Get("ts") != "1512085950.000216" || q.Get("oldest") != "1512085940.000000" || q.Get("latest") != "1512085960.000000" {
				t.Errorf("wrong query %v", q)
			}
			jsonResponse(w, `{"ok":true,"messages":[],"has_more":false}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	if r, e := c.health(context.Background()); e != nil || !r.OK {
		t.Fatalf("%+v %v", r, e)
	}
	if r, e := c.replies(context.Background(), &SlackRepliesIn{Channel: "C123", Ts: "1512085950.000216", Oldest: "1512085940.000000", Latest: "1512085960.000000"}); e != nil || len(r.Messages) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	t.Setenv("SLACK_API_BASE_URL", "")
	if newClient(" x ").base != "https://slack.com/api" {
		t.Fatal("bad default")
	}
}
func TestErrorsRedactedAndNeverRetryWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"rate", 429, `secret-token-do-not-log`}, {"server", 500, `secret-token-do-not-log`}, {"business", 200, `{"ok":false,"error":"secret-token-do-not-log"}`}, {"auth", 200, `{"ok":false,"error":"invalid_auth"}`}, {"missingok", 200, `{}`}, {"invalid", 200, `secret-token-do-not-log`}, {"missingreceipt", 200, `{"ok":true}`}, {"large", 200, strings.Repeat("a", maxResponse+1)}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "120")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, e := c.send(context.Background(), &SlackSendIn{Channel: "C123", Text: "hi"})
			if e == nil || strings.Contains(e.Error(), c.token) || calls != 1 {
				t.Fatalf("calls=%d error=%v", calls, e)
			}
			if tc.status == 429 {
				var rate *rateLimitError
				if !errors.As(e, &rate) || rate.after != 120*time.Second {
					t.Fatalf("%v", e)
				}
			}
		})
	}
}
func TestRedirectNeverForwardsCredential(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/?secret-token-do-not-log", 307)
	})
	if _, e := c.send(context.Background(), &SlackSendIn{Channel: "C123", Text: "hello"}); e == nil || strings.Contains(e.Error(), c.token) {
		t.Fatalf("%v", e)
	}
	if calls != 0 {
		t.Fatal("redirect followed")
	}
}
func TestInputValidationBeforeNetwork(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached network") })
	for _, in := range []*SlackSendIn{{Channel: "../oops", Text: "hello"}, {Channel: "C123"}, {Channel: "C123", Text: strings.Repeat("字", 40001)}, {Channel: "C123", Text: "hi", ThreadTs: "123"}} {
		if _, e := c.send(context.Background(), in); e == nil {
			t.Fatal("accepted invalid send")
		}
	}
	for _, in := range []*SlackHistoryIn{{Channel: "C123", Oldest: "bad"}, {Channel: "C123", Latest: "bad"}, {Channel: "C123", Cursor: strings.Repeat("a", 4097)}} {
		if _, e := c.history(context.Background(), in); e == nil {
			t.Fatal("accepted invalid query")
		}
	}
	if _, e := c.replies(context.Background(), &SlackRepliesIn{Channel: "C123"}); e == nil {
		t.Fatal("missing thread timestamp")
	}
	if _, e := c.upload(context.Background(), &SlackUploadIn{Channel: "C123"}, []byte("x")); e == nil {
		t.Fatal("missing file")
	}
	c.token = ""
	if _, e := c.health(context.Background()); e == nil {
		t.Fatal("missing token")
	}
	c.token = "secret"
	c.base = "http://user:secret@invalid/"
	if _, e := c.health(context.Background()); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatalf("%v", e)
	}
}
func TestUploadThreeStepsBytesAndNoBearer(t *testing.T) {
	step := 0
	payload := []byte("你好\x00\xff")
	var c *client
	c = mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		step++
		switch step {
		case 1:
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if r.URL.Path != "/files.getUploadURLExternal" || b["length"] != float64(len(payload)) || b["filename"] != "report.txt" {
				t.Errorf("bad lease %+v", b)
			}
			jsonResponse(w, `{"ok":true,"file_id":"F123","upload_url":"`+c.base+`/upload/bytes?signature=private"}`)
		case 2:
			b, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || !reflect.DeepEqual(b, payload) || r.Header.Get("Authorization") != "" {
				t.Error("upload bytes or credential boundary")
			}
			w.WriteHeader(200)
		case 3:
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if r.URL.Path != "/files.completeUploadExternal" || b["channel_id"] != "C123" || b["thread_ts"] != "1512085950.000216" || b["initial_comment"] != "attached" {
				t.Errorf("bad complete %+v", b)
			}
			jsonResponse(w, `{"ok":true,"files":[{"id":"F123"}]}`)
		default:
			t.Error("retried upload")
		}
	})
	r, e := c.upload(context.Background(), &SlackUploadIn{Channel: "C123", File: &plugin.File{Name: "report.txt"}, Title: "报告", InitialComment: "attached", ThreadTs: "1512085950.000216"}, payload)
	if e != nil || r.FileID != "F123" || step != 3 {
		t.Fatalf("%+v %v step=%d", r, e, step)
	}
}
func TestUploadRejectsForeignURLAndFailedComplete(t *testing.T) {
	for _, tc := range []string{"foreign", "redirect", "complete"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			var c *client
			c = mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch r.URL.Path {
				case "/files.getUploadURLExternal":
					u := c.base + "/upload/bytes"
					if tc == "foreign" {
						u = "https://attacker.example/upload/private"
					}
					jsonResponse(w, `{"ok":true,"file_id":"F123","upload_url":"`+u+`"}`)
				case "/upload/bytes":
					if tc == "redirect" {
						http.Redirect(w, r, "https://attacker.example/upload/private", 307)
					} else {
						w.WriteHeader(200)
					}
				case "/files.completeUploadExternal":
					jsonResponse(w, `{"ok":false,"error":"secret-token-do-not-log"}`)
				}
			})
			_, e := c.upload(context.Background(), &SlackUploadIn{Channel: "C123", File: &plugin.File{Name: "file.txt"}}, []byte("hello"))
			if e == nil || strings.Contains(e.Error(), "private") || strings.Contains(e.Error(), c.token) {
				t.Fatalf("%v", e)
			}
			expected := map[string]int{"foreign": 1, "redirect": 2, "complete": 3}[tc]
			if calls != expected {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
func TestCancellationAndRetryHeader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newClient("secret")
	if _, e := c.health(ctx); !errors.Is(e, context.Canceled) {
		t.Fatalf("%v", e)
	}
	for _, s := range []string{"", "-1", "bad", "9000000"} {
		if retryDelay(s) != time.Minute {
			t.Fatal(s)
		}
	}
	if retryDelay(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)) < 59*time.Second {
		t.Fatal("HTTP date ignored")
	}
}

type fakeSource struct {
	context.Context
	cred               map[string]string
	events             []map[string]any
	ids                []string
	failEmit, failSave bool
	cancel             context.CancelFunc
}

func (f *fakeSource) Credential() map[string]string                                { return f.cred }
func (f *fakeSource) Fetch(*plugin.File) ([]byte, error)                           { return []byte("file"), nil }
func (f *fakeSource) Upload(string, string, []byte) (*plugin.File, error)          { return nil, nil }
func (f *fakeSource) UploadReader(string, string, io.Reader) (*plugin.File, error) { return nil, nil }
func (f *fakeSource) Trigger(name, id string, p any) error {
	if f.failEmit {
		return errors.New("emit failed")
	}
	m := contract.StructToVars(p)
	m["event"] = name
	f.events = append(f.events, m)
	f.ids = append(f.ids, id)
	return nil
}
func (f *fakeSource) UpdateCredential(m map[string]string) error {
	if f.cancel != nil {
		f.cancel()
	}
	if f.failSave {
		return errors.New("save failed")
	}
	for k, v := range m {
		f.cred[k] = v
	}
	return nil
}
func (f *fakeSource) ReportStatus(string, string) {}
func TestPollBaselinePagingAndFiltering(t *testing.T) {
	ctx := &fakeSource{Context: context.Background()}
	round := 0
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		round++
		switch round {
		case 1:
			jsonResponse(w, `{"ok":true,"messages":[{"type":"message","ts":"100.000001","text":"old","user":"U123"}]}`)
		case 2:
			if r.URL.Query().Get("oldest") != "100.000001" {
				t.Error("missing oldest")
			}
			jsonResponse(w, `{"ok":true,"messages":[{"type":"message","ts":"104.000001","text":"bot","bot_id":"B123"},{"type":"message","ts":"103.000001","text":"new","user":"U123"}],"has_more":true,"response_metadata":{"next_cursor":"next"}}`)
		case 3:
			if r.URL.Query().Get("cursor") != "next" || r.URL.Query().Get("latest") != "110.123456" {
				t.Error("lost stable window")
			}
			jsonResponse(w, `{"ok":true,"messages":[{"type":"message","ts":"102.000001","text":"reply","user":"U123","thread_ts":"100.000001"},{"type":"message","ts":"101.000001","text":"newer","user":"U456"}]}`)
		}
	})
	now := time.Unix(110, 123456000)
	s, e := pollOnce(ctx, c, "C123", pollState{}, now)
	if e != nil || len(ctx.events) != 0 || s.Oldest != "100.000001" {
		t.Fatalf("baseline %+v %v", s, e)
	}
	s, e = pollOnce(ctx, c, "C123", s, now)
	if e != nil || s.Cursor != "next" || s.Oldest != "100.000001" {
		t.Fatalf("page %+v %v", s, e)
	}
	s, e = pollOnce(ctx, c, "C123", s, time.Unix(120, 0))
	if e != nil || s.Oldest != "104.000001" || s.Cursor != "" || len(ctx.events) != 2 {
		t.Fatalf("final %+v %v %+v", s, e, ctx.events)
	}
	if ctx.events[0]["channel"] != "C123" || ctx.events[0]["ts"] != "103.000001" || ctx.events[0]["event"] != "message_received" {
		t.Fatalf("not flattened %+v", ctx.events)
	}
}
func TestPollFailureDoesNotAdvance(t *testing.T) {
	old := pollState{Channel: "C123", Oldest: "100.000001"}
	for _, tc := range []string{"emit", "cursor", "repeat"} {
		t.Run(tc, func(t *testing.T) {
			f := &fakeSource{Context: context.Background(), failEmit: tc == "emit"}
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				suffix := ""
				if tc == "cursor" {
					suffix = `,"has_more":true`
				}
				if tc == "repeat" {
					suffix = `,"has_more":true,"response_metadata":{"next_cursor":"same"}`
				}
				jsonResponse(w, `{"ok":true,"messages":[{"type":"message","ts":"101.000001","text":"new","user":"U123"}]`+suffix+`}`)
			})
			state := old
			if tc == "repeat" {
				state.Cursor = "same"
			}
			next, e := pollOnce(f, c, "C123", state, time.Unix(110, 0))
			if e == nil || !reflect.DeepEqual(next, state) {
				t.Fatalf("advanced on failure %+v %v", next, e)
			}
		})
	}
}
func TestSourcePersistenceAndIdleCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeSource{Context: ctx, cred: map[string]string{"token": "secret", "watch_channel": "C123"}, cancel: cancel}
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, `{"ok":true,"messages":[]}`) })
	t.Setenv("SLACK_API_BASE_URL", c.base)
	if e := runSource(f); !errors.Is(e, context.Canceled) {
		t.Fatalf("%v", e)
	}
	if f.cred[stateKey] == "" {
		t.Fatal("no persisted baseline")
	}
	f.cred = map[string]string{}
	if e := runSource(f); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	t.Setenv("SLACK_POLL_SECONDS", "0")
	if pollInterval() != time.Minute {
		t.Fatal("unsafe interval")
	}
	p := sokel.New(sokel.Config{Name: "slack"})
	register(p)
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(op contract.Operation, fn plugin.Invoke) { h.calls[op.ID] = fn }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func TestGeneratedHandlersBindAndReturnHealthFailures(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			if r.Method != "POST" {
				t.Error("auth.test must use POST")
			}
			if r.Header.Get("Authorization") == "Bearer bad" {
				jsonResponse(w, `{"ok":false,"error":"invalid_auth"}`)
			} else {
				jsonResponse(w, `{"ok":true,"team_id":"T123","user_id":"U123"}`)
			}
		case "/conversations.history", "/conversations.replies":
			jsonResponse(w, `{"ok":true,"messages":[]}`)
		case "/chat.postMessage":
			jsonResponse(w, `{"ok":true,"channel":"C123","ts":"1512085950.000216"}`)
		}
	})
	t.Setenv("SLACK_API_BASE_URL", c.base)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	f := &fakeSource{Context: context.Background(), cred: map[string]string{"token": "secret"}}
	for op, raw := range map[string]string{"health_check": `{}`, "slack_history": `{"channel":"C123"}`, "slack_replies": `{"channel":"C123","ts":"1512085950.000216"}`, "slack_send": `{"channel":"C123","text":"hello"}`} {
		sink := &testSink{}
		if e := h.calls[op](f, json.RawMessage(raw), sink); e != nil || sink.vars == nil {
			t.Fatalf("%s %v", op, e)
		}
		if op == "health_check" && (sink.vars["ok"] != true || sink.vars["message"] == "") {
			t.Fatalf("invalid health %+v", sink.vars)
		}
	}
	f.cred["token"] = "bad"
	sink := &testSink{}
	if e := h.calls["health_check"](f, json.RawMessage(`{}`), sink); e != nil || sink.vars["ok"] != false || sink.vars["message"] == "" {
		t.Fatalf("failed health %+v %v", sink.vars, e)
	}
	if e := h.calls["slack_upload"](f, json.RawMessage(`{"channel":"C123"}`), &testSink{}); e == nil {
		t.Fatal("missing file accepted")
	}
}

func TestRestartHonorsPersistedPollSchedule(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonResponse(w, `{"ok":true,"messages":[]}`)
	}))
	defer s.Close()
	t.Setenv("SLACK_API_BASE_URL", s.URL)
	raw, _ := json.Marshal(map[string]any{"next_poll_at": time.Now().Add(time.Minute).UnixMilli()})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	f := &fakeSource{Context: ctx, cred: map[string]string{"token": "test-token", "watch_channel": "C123", stateKey: string(raw)}}
	if err := runSource(f); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected source result: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("source restart bypassed polling interval: %d requests", calls.Load())
	}
}

type fetchFailureContext struct{ *fakeSource }

func (fetchFailureContext) Fetch(*plugin.File) ([]byte, error) {
	return nil, errors.New("signed-url-secret")
}
func TestUploadFileFetchErrorIsRedacted(t *testing.T) {
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	ctx := fetchFailureContext{&fakeSource{Context: context.Background(), cred: map[string]string{"token": "secret"}}}
	err := h.calls["slack_upload"](ctx, json.RawMessage(`{"channel":"C123","file":{"id":"f_test","name":"sample.txt","size":4}}`), &testSink{})
	if err == nil || strings.Contains(err.Error(), "signed-url-secret") {
		t.Fatalf("unsafe file error: %v", err)
	}
}
