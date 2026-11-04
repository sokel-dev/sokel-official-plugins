package main

import (
	"context"
	"encoding/json"
	"errors"
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
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/limits"):
			return 200, `{"DailyApiRequests":{"Max":1000,"Remaining":999}}`
		case strings.HasSuffix(p, "/describe"):
			return 200, `{"name":"Account","label":"Account","createable":true,"updateable":true,"fields":[{"name":"External_ID__c","label":"External ID","type":"string","externalId":true}]}`
		case strings.Contains(p, "/query"):
			if p == apiPath+"/query" && r.URL.Query().Get("q") != "SELECT Id, Count__c FROM Account" {
				t.Fatal(r.URL)
			}
			return 200, `{"records":[{"Id":"001000000000001AAA","Count__c":9007199254740993}],"totalSize":1,"done":true}`
		case r.Method == "GET":
			if r.URL.Query().Get("fields") != "Id,Name" {
				t.Fatal(r.URL)
			}
			return 200, `{"Id":"001000000000001AAA","Name":"Name","Count__c":9007199254740993}`
		case r.Method == "POST":
			if bodyOf(t, r)["Name"] != "New" {
				t.Fatal("fields")
			}
			return 201, `{"id":"001000000000001AAA","success":true,"errors":[]}`
		case strings.Contains(p, "/External_ID__c/"):
			return 201, `{"id":"001000000000001AAA","success":true,"errors":[],"created":true}`
		case r.Method == "PATCH":
			b := bodyOf(t, r)
			v, ok := b["Description"]
			if !ok || v != nil || b["Active__c"] != false {
				t.Fatal(b)
			}
			return 204, ""
		}
		t.Fatal(r.URL)
		return 500, `{}`
	})
	dispatchAll(t, a, map[string]string{"health_check": `{}`, "describe_object": `{"object_name":"Account"}`, "query": `{"soql":"SELECT Id, Count__c FROM Account"}`, "query_more": `{"next_records_url":"/services/data/v65.0/query/01g000000000001AAA-2000"}`, "get_record": `{"object_name":"Account","record_id":"001000000000001AAA","field_names":["Name"]}`, "create_record": `{"object_name":"Account","fields":{"Name":"New"}}`, "update_record": `{"object_name":"Account","record_id":"001000000000001AAA","fields":{"Description":null,"Active__c":false}}`, "upsert_record": `{"object_name":"Account","external_id_field":"External_ID__c","external_id":"ext-1","fields":{"Name":"Name"}}`}, func(id string, v map[string]any) {
		if id == "describe_object" && v["fields"].([]any)[0].(map[string]any)["external_id"] != true {
			t.Fatal(v)
		}
	})
	v, e := a.query(context.Background(), &QueryIn{Soql: "SELECT Id, Count__c FROM Account"})
	if e != nil {
		t.Fatal(e)
	}
	if n, ok := v.Records[0].(map[string]any)["Count__c"].(json.Number); !ok || n.String() != "9007199254740993" {
		t.Fatal(v)
	}
}
func TestPaginationAndErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"records":null,"totalSize":0,"done":true}`, `{"records":[],"done":true}`, `{"records":[null],"totalSize":1,"done":true}`, `{"records":["bad"],"totalSize":1,"done":true}`, `{"records":[],"totalSize":1,"done":false,"nextRecordsUrl":"https://evil.example"}`, `{"records":[],"totalSize":1,"done":true,"nextRecordsUrl":"/services/data/v65.0/query/id-1"}`} {
		a := fakeAPI(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := a.query(context.Background(), &QueryIn{Soql: "SELECT Id FROM Account"}); e == nil {
			t.Fatal(body)
		}
	}
	a := fakeAPI(t, func(*http.Request) (int, string) { return 200, `{"records":[],"totalSize":0,"done":true}` })
	if v, e := a.query(context.Background(), &QueryIn{Soql: "SELECT Id FROM Account"}); e != nil || !v.Done || v.Records == nil {
		t.Fatal(v, e)
	}
	for _, body := range []string{`{"id":"001000000000001AAA","success":false,"errors":[]}`, `{"id":"001000000000001AAA","success":true}`, `{"success":true,"errors":[]}`, `{"id":"001000000000001AAA","success":true,"errors":[{"message":"test-secret"}]}`} {
		a = fakeAPI(t, func(*http.Request) (int, string) { return 201, body })
		if _, e := a.create_record(context.Background(), &CreateRecordIn{ObjectName: "Account", Fields: map[string]any{"Name": "x"}}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(e)
		}
	}
	a = fakeAPI(t, func(*http.Request) (int, string) {
		return 200, `{"id":"001000000000001AAA","success":true,"errors":[],"created":true}`
	})
	if _, e := a.upsert_record(context.Background(), &UpsertRecordIn{ObjectName: "Account", ExternalIDField: "Ext__c", ExternalID: "x", Fields: map[string]any{"Name": "x"}}); e == nil {
		t.Fatal("inconsistent upsert")
	}
}
func TestInputValidation(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) (int, string) { t.Fatal("invalid input reached provider"); return 200, `{}` })
	for _, host := range []string{"http://example.my.salesforce.com", "https://example.my.salesforce.com.evil.example", "https://user:pass@example.my.salesforce.com", "https://example.my.salesforce.com:443", "https://login.salesforce.com", "https://127.0.0.1", "https://example.my.salesforce.com/path", "https://example.my.salesforce.com?secret=x"} {
		a.cred.InstanceURL = host
		if _, e := a.origin(); e == nil {
			t.Fatal(host)
		}
	}
	for _, host := range []string{"https://example.my.salesforce.com", "https://example--test.sandbox.my.salesforce.com", "https://na123.salesforce.com"} {
		a.cred.InstanceURL = host
		if _, e := a.origin(); e != nil {
			t.Fatal(host, e)
		}
	}
	a.cred = testCred()
	for _, q := range []string{"DELETE FROM Account", "SELECT Id FROM Account FOR UPDATE", "SELECT Id FROM Account FOR VIEW", "SELECT Id FROM Account FOR REFERENCE", "SELECT Id FROM Account;", "SELECT /*comment*/ Id FROM Account"} {
		if _, e := a.query(context.Background(), &QueryIn{Soql: q}); e == nil {
			t.Fatal(q)
		}
	}
	for _, next := range []string{"https://example.my.salesforce.com/services/data/v65.0/query/x-1", "/services/data/v65.0/sobjects/Account", "/services/data/v64.0/query/x-1", "/services/data/v65.0/query/x-1?token=x"} {
		if _, e := a.query_more(context.Background(), &QueryMoreIn{NextRecordsURL: next}); e == nil {
			t.Fatal(next)
		}
	}
	for _, fields := range []map[string]any{{}, {"Id": "x"}, {"bad.name": true}, {"Count__c": float64(9007199254740992)}} {
		if _, e := a.create_record(context.Background(), &CreateRecordIn{ObjectName: "Account", Fields: fields}); e == nil {
			t.Fatal(fields)
		}
	}
	if _, e := a.get_record(context.Background(), &GetRecordIn{ObjectName: "../", RecordID: "x"}); e == nil {
		t.Fatal("object")
	}
	if _, e := a.upsert_record(context.Background(), &UpsertRecordIn{ObjectName: "Account", ExternalIDField: "Ext__c", ExternalID: "../x", Fields: map[string]any{"Name": "x"}}); e == nil {
		t.Fatal("external ID")
	}
	a.cred.AccessToken = "bad\nsecret"
	if h, e := a.health_check(context.Background(), &HealthCheckIn{}); e != nil || h.OK {
		t.Fatal(h, e)
	}
}
func TestWireNumberPreservationAndContinuation(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		return 200, `{"records":[{"Count__c":9007199254740993}],"totalSize":2,"done":false,"nextRecordsUrl":"/services/data/v65.0/query/01g000000000001AAA-2000"}`
	})
	v, e := a.query(context.Background(), &QueryIn{Soql: "SELECT Count__c FROM Account"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(contract.StructToVars(v))
	if e != nil || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatal(string(raw), e)
	}
	if _, e = a.query_more(context.Background(), &QueryMoreIn{NextRecordsURL: v.NextRecordsURL}); e == nil {
		t.Fatal("repeated continuation accepted")
	}
}
