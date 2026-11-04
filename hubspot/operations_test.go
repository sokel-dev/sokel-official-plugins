package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"strings"
	"testing"
)

const sampleRecord = `{"id":"512","properties":{"email":"x@example.com","empty":null},"createdAt":"2019-10-30T03:30:17.883Z","updatedAt":"2019-10-30T03:30:17.883Z","archived":false}`

func TestGeneratedSearchUsesCamelCaseFilterWire(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if r.URL.Path != "/crm/v3/objects/deals/search" || r.Method != "POST" || json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Error("bad search")
		}
		group := b["filterGroups"].([]any)[0].(map[string]any)
		filter := group["filters"].([]any)[0].(map[string]any)
		if filter["propertyName"] != "dealstage" || filter["value"] != "closedwon" || b["after"] != "100" {
			t.Errorf("bad wire body %+v", b)
		}
		reply(w, `{"total":101,"results":[`+sampleRecord+`],"paging":{"next":{"after":200}}}`)
	})
	t.Setenv("HUBSPOT_API_BASE_URL", c.base)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	sink := &testSink{}
	ctx := &testCtx{Context: context.Background(), cred: map[string]string{"token": "test-secret"}}
	input := json.RawMessage(`{"object_type":"deals","after":"100","filters":[{"property_name":"dealstage","operator":"EQ","value":"closedwon"}]}`)
	if e := h.calls["search_records"](ctx, input, sink); e != nil || sink.vars["next_after"] != "200" {
		t.Fatalf("binding or output: %v %+v", e, sink.vars)
	}
}
func TestGetUpdateAndMetadata(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/crm/v3/properties/") {
			reply(w, `{"results":[{"name":"stage","label":"Stage","type":"enumeration","fieldType":"select","groupName":"sales","options":[{"label":"New","value":"new","hidden":false}]}]}`)
			return
		}
		if r.Method == "PATCH" {
			var b map[string]map[string]string
			if json.NewDecoder(r.Body).Decode(&b) != nil || b["properties"]["email"] != "" {
				t.Error("PATCH must preserve explicit clearing")
			}
		}
		reply(w, sampleRecord)
	})
	r, e := c.getRecord(context.Background(), &GetRecordIn{ObjectType: "contacts", RecordID: "512", Properties: []string{"email"}})
	if e != nil || r.Record.Properties["empty"] != nil {
		t.Fatalf("get %+v %v", r, e)
	}
	if _, e = c.updateRecord(context.Background(), &UpdateRecordIn{ObjectType: "contacts", RecordID: "512", Properties: map[string]any{"email": ""}}); e != nil {
		t.Fatal(e)
	}
	p, e := c.listProperties(context.Background(), &ListPropertiesIn{ObjectType: "deals"})
	if e != nil || p.Properties[0].FieldType != "select" || p.Properties[0].GroupName != "sales" {
		t.Fatalf("metadata %+v %v", p, e)
	}
}
func TestValidationBeforeNetwork(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached network") })
	for _, kind := range []string{"tickets", "../contacts", ""} {
		if _, e := c.listRecords(context.Background(), &ListRecordsIn{ObjectType: kind}); e == nil {
			t.Fatal("invalid type")
		}
	}
	for _, p := range []map[string]any{nil, {"amount": 12}, {"email": nil}, {"bad-name": "x"}} {
		if _, e := c.createRecord(context.Background(), &CreateRecordIn{ObjectType: "contacts", Properties: p}); e == nil {
			t.Fatal("bad properties")
		}
	}
	for _, in := range []*SearchRecordsIn{{ObjectType: "contacts", After: "opaque"}, {ObjectType: "contacts", After: "10000"}, {ObjectType: "contacts", Query: strings.Repeat("x", 3001)}, {ObjectType: "contacts", Filters: []SearchFilter{{PropertyName: "email", Operator: "IN"}}}, {ObjectType: "contacts", Filters: []SearchFilter{{PropertyName: "email", Operator: "HAS_PROPERTY", Value: "x"}}}} {
		if _, e := c.searchRecords(context.Background(), in); e == nil {
			t.Fatal("bad search")
		}
	}
	if _, e := c.updateRecord(context.Background(), &UpdateRecordIn{ObjectType: "contacts", RecordID: "../x", Properties: map[string]any{"email": "x"}}); e == nil {
		t.Fatal("bad ID")
	}
}
func TestMalformedReceiptsAndPagination(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"results":[{"id":"bad","properties":{}}]}`, `{"results":[{"id":"1","properties":{"x":1}}]}`, `{"results":[],"paging":{"next":{"after":"same"}}}`} {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
		if _, e := c.listRecords(context.Background(), &ListRecordsIn{ObjectType: "contacts", After: "same"}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if out, _ := c.health(context.Background()); out.OK {
		t.Fatal("false health success")
	}
	if _, e := c.createRecord(context.Background(), &CreateRecordIn{ObjectType: "contacts", Properties: map[string]any{"email": "x"}}); e == nil {
		t.Fatal("missing write receipt")
	}
}
