package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"math"
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
	return &client{base: s.URL, token: "secret-token-do-not-log", http: s.Client()}
}
func jsonResponse(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, s)
}

const sampleRecord = `{"id":"rec123","createdTime":"2022-09-08T16:34:34.000Z","fields":{"Name":"alpha","Large":9007199254740993}}`

func TestMetadataGetUpdateDelete(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/meta/bases":
			if r.URL.Query().Get("offset") != "" && r.URL.Query().Get("offset") != "next/=" {
				t.Error("offset altered")
			}
			jsonResponse(w, `{"bases":[{"id":"app123","name":"Demo","permissionLevel":"create"}],"offset":"next/="}`)
		case "/meta/bases/app123/tables":
			jsonResponse(w, `{"tables":[{"id":"tbl123","name":"Tasks","primaryFieldId":"fld123","fields":[{"id":"fld123","name":"Name","type":"singleLineText"}]}]}`)
		case "/app123/tbl123/rec123":
			switch r.Method {
			case "GET":
				jsonResponse(w, sampleRecord)
			case "PATCH":
				var b map[string]any
				json.NewDecoder(r.Body).Decode(&b)
				fs := b["fields"].(map[string]any)
				if fs["Name"] != "" || fs["Active"] != false || fs["Clear"] != nil {
					t.Errorf("clear values lost %+v", b)
				}
				if _, ok := b["typecast"]; ok {
					t.Error("unspecified typecast sent")
				}
				jsonResponse(w, sampleRecord)
			case "DELETE":
				jsonResponse(w, `{"id":"rec123","deleted":true}`)
			}
		}
	})
	ctx := context.Background()
	if r, e := c.health(ctx); e != nil || !r.OK {
		t.Fatalf("%+v %v", r, e)
	}
	if r, e := c.bases(ctx, &AirtableBasesIn{Offset: "next/="}); e != nil || r.Offset != "next/=" || len(r.Bases) != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	if r, e := c.tables(ctx, &AirtableTablesIn{BaseID: "app123"}); e != nil || len(r.Tables) != 1 || r.Tables[0].Fields[0].Type != "singleLineText" {
		t.Fatalf("%+v %v", r, e)
	}
	r, e := c.get(ctx, &AirtableGetIn{BaseID: "app123", TableID: "tbl123", RecordID: "rec123"})
	if e != nil || r.Record.Fields["Large"].(json.Number).String() != "9007199254740993" {
		t.Fatalf("precision lost %+v %v", r, e)
	}
	if _, e = c.update(ctx, &AirtableUpdateIn{BaseID: "app123", TableID: "tbl123", RecordID: "rec123", Fields: map[string]any{"Name": "", "Active": false, "Clear": nil}}); e != nil {
		t.Fatal(e)
	}
	if r, e := c.remove(ctx, &AirtableDeleteIn{BaseID: "app123", TableID: "tbl123", RecordID: "rec123"}); e != nil || !r.Deleted {
		t.Fatalf("%+v %v", r, e)
	}
	t.Setenv("AIRTABLE_API_BASE_URL", "")
	if newClient(" x ").base != "https://api.airtable.com/v0" {
		t.Fatal("bad default")
	}
}
func TestErrorsRedactedAndWritesNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"401", 401, "secret-token-do-not-log"}, {"403", 403, "secret-token-do-not-log"}, {"404", 404, "secret-token-do-not-log"}, {"422", 422, "secret-token-do-not-log"}, {"429", 429, "secret-token-do-not-log"}, {"500", 500, "secret-token-do-not-log"}, {"business", 200, `{"error":{"message":"secret-token-do-not-log"}}`}, {"invalid", 200, "secret-token-do-not-log"}, {"null", 200, "null"}, {"emptyreceipt", 200, "{}"}, {"large", 200, strings.Repeat("a", maxResponse+1)}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "90")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, e := c.create(context.Background(), &AirtableCreateIn{BaseID: "app123", TableID: "tbl123", Fields: map[string]any{"a": 1}})
			if e == nil || strings.Contains(e.Error(), c.token) || calls != 1 {
				t.Fatalf("calls=%d error=%v", calls, e)
			}
			if tc.status == 429 {
				var rate *rateLimitError
				if !errors.As(e, &rate) || rate.after != 90*time.Second {
					t.Fatal(e)
				}
			}
		})
	}
}
func TestRedirectDoesNotLeakOrWriteTwice(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/?secret-token-do-not-log", 307)
	})
	_, e := c.create(context.Background(), &AirtableCreateIn{BaseID: "app123", TableID: "tbl123", Fields: map[string]any{"a": 1}})
	if e == nil || called || strings.Contains(e.Error(), c.token) {
		t.Fatalf("called=%v err=%v", called, e)
	}
}
func TestInvalidInputsNeverReachUpstream(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached network") })
	ctx := context.Background()
	for _, in := range []*AirtableRecordsIn{{BaseID: "../app123", TableID: "tbl123"}, {BaseID: "app123", TableID: "tbl/123"}, {BaseID: "app123", TableID: "tbl123", Offset: strings.Repeat("x", 4097)}, {BaseID: "app123", TableID: "tbl123", Fields: []string{" "}}, {BaseID: "app123", TableID: "tbl123", FilterFormula: strings.Repeat("x", 8001)}} {
		if _, e := c.records(ctx, in); e == nil {
			t.Fatal("invalid query accepted")
		}
	}
	for _, fields := range []map[string]any{nil, {"": 1}, {"Bad": math.NaN()}, {"Text": strings.Repeat("x", 1<<20)}} {
		if _, e := c.create(ctx, &AirtableCreateIn{BaseID: "app123", TableID: "tbl123", Fields: fields}); e == nil {
			t.Fatal("invalid body accepted")
		}
	}
	if _, e := c.update(ctx, &AirtableUpdateIn{BaseID: "app123", TableID: "tbl123", RecordID: "rec/../", Fields: map[string]any{"a": 1}}); e == nil {
		t.Fatal("invalid id accepted")
	}
	if _, e := c.get(ctx, &AirtableGetIn{}); e == nil {
		t.Fatal("missing ids")
	}
	if _, e := c.remove(ctx, &AirtableDeleteIn{}); e == nil {
		t.Fatal("missing ids")
	}
	if _, e := c.tables(ctx, &AirtableTablesIn{}); e == nil {
		t.Fatal("missing base")
	}
	if _, e := c.bases(ctx, &AirtableBasesIn{Offset: strings.Repeat("x", 4097)}); e == nil {
		t.Fatal("long offset")
	}
	c.token = ""
	if _, e := c.health(ctx); e == nil {
		t.Fatal("missing token")
	}
	c.token = "secret"
	c.base = "http://user:secret@host"
	if _, e := c.health(ctx); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}
func TestMalformedSuccessfulResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"records":[{"id":"bad"}]}`, `{"records":[{"id":"rec123","createdTime":"bad"}]}`, `{"records":null}`} {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, body) })
		if _, e := c.records(context.Background(), &AirtableRecordsIn{BaseID: "app123", TableID: "tbl123"}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	for _, kind := range []string{"bases", "tables", "delete"} {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, `{}`) })
		var e error
		switch kind {
		case "bases":
			_, e = c.bases(context.Background(), &AirtableBasesIn{})
		case "tables":
			_, e = c.tables(context.Background(), &AirtableTablesIn{BaseID: "app123"})
		case "delete":
			_, e = c.remove(context.Background(), &AirtableDeleteIn{BaseID: "app123", TableID: "tbl123", RecordID: "rec123"})
		}
		if e == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}
func TestCancellationAndBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := newClient("secret").health(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	for _, s := range []string{"", "bad", "-1", "9000000"} {
		if retryDelay(s) != 30*time.Second {
			t.Fatal(s)
		}
	}
	if retryDelay(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)) < 59*time.Second {
		t.Fatal("date ignored")
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
func (f *fakeSource) Fetch(*plugin.File) ([]byte, error)                           { return nil, nil }
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
func TestPollFullScanBaselineAndChanges(t *testing.T) {
	round := 0
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		round++
		switch round {
		case 1:
			jsonResponse(w, `{"records":[`+sampleRecord+`],"offset":"next"}`)
		case 2:
			if r.URL.Query().Get("offset") != "next" {
				t.Error("offset missing")
			}
			jsonResponse(w, `{"records":[]}`)
		case 3:
			jsonResponse(w, `{"records":[{"id":"rec123","createdTime":"2022-09-08T16:34:34.000Z","fields":{"Name":"changed"}},{"id":"rec456","createdTime":"2022-09-08T16:34:34.000Z","fields":{}}]}`)
		case 4:
			jsonResponse(w, `{"records":[`+sampleRecord+`]}`)
		}
	})
	f := &fakeSource{Context: context.Background()}
	s, e := pollOnce(f, c, "app123", "tbl123", pollState{})
	if e != nil || !s.Ready || len(f.events) != 0 || len(s.Hashes) != 1 {
		t.Fatalf("baseline %+v %v", s, e)
	}
	s, e = pollOnce(f, c, "app123", "tbl123", s)
	if e != nil || len(f.events) != 2 || f.events[0]["change"] != "updated" || f.events[1]["change"] != "created" {
		t.Fatalf("events %+v %v", f.events, e)
	}
	if f.events[0]["base_id"] != "app123" || f.events[0]["table_id"] != "tbl123" || f.events[0]["event"] != "record_changed" {
		t.Fatal("events not flattened")
	}
	s, e = pollOnce(f, c, "app123", "tbl123", s)
	if e != nil || len(f.events) != 3 || f.ids[0] == f.ids[2] || s.Revision != 3 {
		t.Fatalf("revision %+v %v", s, e)
	}
}
func TestPollIncompleteOrEmissionFailurePreservesState(t *testing.T) {
	for _, tc := range []string{"cursor", "duplicate", "overflow", "emit", "http"} {
		t.Run(tc, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch tc {
				case "cursor":
					jsonResponse(w, `{"records":[],"offset":"same"}`)
				case "duplicate":
					jsonResponse(w, `{"records":[`+sampleRecord+`,`+sampleRecord+`]}`)
				case "overflow":
					rows := make([]Record, 501)
					for i := range rows {
						rows[i] = Record{ID: fmt.Sprintf("rec%d", i), CreatedTime: "2022-09-08T16:34:34.000Z", Fields: map[string]any{}}
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"records": rows})
				case "emit":
					jsonResponse(w, `{"records":[`+sampleRecord+`]}`)
				case "http":
					w.WriteHeader(500)
				}
			})
			old := pollState{Base: "app123", Table: "tbl123", Ready: true, Revision: 1, Hashes: map[string]string{}}
			f := &fakeSource{Context: context.Background(), failEmit: tc == "emit"}
			s, e := pollOnce(f, c, "app123", "tbl123", old)
			if e == nil || !reflect.DeepEqual(old, s) || len(f.events) != 0 {
				t.Fatalf("state changed %+v %v", s, e)
			}
		})
	}
}
func TestPollNoChangesAndTargetReset(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, `{"records":[`+sampleRecord+`]}`) })
	f := &fakeSource{Context: context.Background()}
	s, e := pollOnce(f, c, "app123", "tbl123", pollState{})
	if e != nil {
		t.Fatal(e)
	}
	s, e = pollOnce(f, c, "app123", "tbl123", s)
	if e != nil || len(f.events) != 0 {
		t.Fatal("unchanged emitted")
	}
	s.Base = "appOld"
	_, e = pollOnce(f, c, "app123", "tbl123", s)
	if e != nil || len(f.events) != 0 {
		t.Fatal("target change replayed history")
	}
}
func TestSourcePersistAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeSource{Context: ctx, cred: map[string]string{"token": "secret", "watch_base": "app123", "watch_table": "tbl123"}, cancel: cancel}
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, `{"records":[]}`) })
	t.Setenv("AIRTABLE_API_BASE_URL", c.base)
	if e := runSource(f); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if f.cred[stateKey] == "" {
		t.Fatal("no persisted baseline")
	}
	f.cred = map[string]string{}
	if e := runSource(f); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	f.cred = map[string]string{"watch_base": "app123"}
	if e := runSource(f); e == nil {
		t.Fatal("incomplete configuration")
	}
	f.cred = map[string]string{"watch_base": "app123", "watch_table": "tbl123", stateKey: "invalid"}
	if e := runSource(f); e == nil {
		t.Fatal("invalid state")
	}
	t.Setenv("AIRTABLE_POLL_SECONDS", "0")
	if pollInterval() != 5*time.Minute {
		t.Fatal("unsafe default")
	}
	p := sokel.New(sokel.Config{Name: "airtable"})
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
		if r.Header.Get("Authorization") == "Bearer bad" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/meta/bases":
			jsonResponse(w, `{"bases":[]}`)
		case "/meta/bases/app123/tables":
			jsonResponse(w, `{"tables":[]}`)
		case "/app123/tbl123":
			if r.Method == "GET" {
				jsonResponse(w, `{"records":[]}`)
			} else {
				jsonResponse(w, sampleRecord)
			}
		case "/app123/tbl123/rec123":
			if r.Method == "DELETE" {
				jsonResponse(w, `{"id":"rec123","deleted":true}`)
			} else {
				jsonResponse(w, sampleRecord)
			}
		}
	})
	t.Setenv("AIRTABLE_API_BASE_URL", c.base)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	f := &fakeSource{Context: context.Background(), cred: map[string]string{"token": "secret"}}
	for op, raw := range map[string]string{"health_check": `{}`, "airtable_bases": `{}`, "airtable_tables": `{"base_id":"app123"}`, "airtable_records": `{"base_id":"app123","table_id":"tbl123"}`, "airtable_get": `{"base_id":"app123","table_id":"tbl123","record_id":"rec123"}`, "airtable_create": `{"base_id":"app123","table_id":"tbl123","fields":{"Count":0}}`, "airtable_update": `{"base_id":"app123","table_id":"tbl123","record_id":"rec123","fields":{"Count":0}}`, "airtable_delete": `{"base_id":"app123","table_id":"tbl123","record_id":"rec123"}`} {
		sink := &testSink{}
		if e := h.calls[op](f, json.RawMessage(raw), sink); e != nil || sink.vars == nil {
			t.Fatalf("%s %v", op, e)
		}
		if op == "health_check" && (sink.vars["ok"] != true || sink.vars["message"] == "") {
			t.Fatalf("health %+v", sink.vars)
		}
	}
	f.cred["token"] = "bad"
	sink := &testSink{}
	if e := h.calls["health_check"](f, json.RawMessage(`{}`), sink); e != nil || sink.vars["ok"] != false || sink.vars["message"] == "" {
		t.Fatalf("failed health %+v %v", sink.vars, e)
	}
}

func TestRestartHonorsPersistedPollSchedule(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonResponse(w, `{"records":[]}`)
	}))
	defer s.Close()
	t.Setenv("AIRTABLE_API_BASE_URL", s.URL)
	raw, _ := json.Marshal(map[string]any{"next_poll_at": time.Now().Add(time.Minute).UnixMilli()})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	f := &fakeSource{Context: ctx, cred: map[string]string{"token": "test-token", "watch_base": "app123", "watch_table": "tbl123", stateKey: string(raw)}}
	if err := runSource(f); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected source result: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("source restart bypassed polling interval: %d requests", calls.Load())
	}
}
