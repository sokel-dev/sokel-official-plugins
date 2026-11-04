package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/pocketbase/schema"
	"net/http"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func TestRecordPathsAndInputs(t *testing.T) {
	for _, s := range []string{"", "../posts", "_superusers", "a/b", "a?token=x", "密集", strings.Repeat("a", 129)} {
		if _, e := collectionPath(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"posts", "pbc_12345", "a98f514eb05f454"} {
		if _, e := collectionPath(s); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := recordPath("posts", "../bad"); e == nil {
		t.Fatal("path traversal")
	}
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request reached network")
		return nil, nil
	})
	for _, raw := range []string{`null`, `[]`, `{}`, `{"id":"abc"}`, `{"collectionId":"x"}`, `{"collectionName":"x"}`, `{"expand":{}}`, `{} {}`, string([]byte{'{', 0xff, '}'}), strings.Repeat("x", maxRequest+1)} {
		if _, e := c.update_record(context.Background(), &UpdateRecordIn{Collection: "posts", RecordID: "ae40239d2bc4477", DataJSON: raw}); e == nil {
			t.Fatal(raw[:min(len(raw), 30)])
		}
	}
	for _, raw := range []string{`{}`, `{"zero":0,"false":false,"large":9007199254740993}`, `{"id":"ae40239d2bc4477"}`} {
		if _, e := writeBody(raw, false); e != nil {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{`{"id":4}`, `{"id":"../x"}`} {
		if _, e := writeBody(raw, false); e == nil {
			t.Fatal("invalid created ID")
		}
	}
	for _, o := range []schema.ListOptions{{Page: ptr(0)}, {Page: ptr(1000001)}, {PerPage: ptr(0)}, {PerPage: ptr(201)}, {Filter: ptr("x\n")}, {Sort: ptr(strings.Repeat("x", 8193))}, {Expand: ptr("a\x00")}} {
		if _, e := c.list_records(context.Background(), &ListRecordsIn{Collection: "posts", Options: o}); e == nil {
			t.Fatal(o)
		}
	}
	if _, e := c.find_record(context.Background(), &FindRecordIn{Collection: "posts"}); e == nil {
		t.Fatal("missing lookup filter")
	}
	if _, e := c.get_record(context.Background(), &GetRecordIn{Collection: "posts", RecordID: "x", Expand: "x\r"}); e == nil {
		t.Fatal("bad expand")
	}
	if _, e := c.create_record(context.Background(), &CreateRecordIn{Collection: "../bad", DataJSON: `{}`}); e == nil {
		t.Fatal("bad collection")
	}
	if _, e := c.update_record(context.Background(), &UpdateRecordIn{Collection: "posts", RecordID: "../bad", DataJSON: `{"a":false}`}); e == nil {
		t.Fatal("bad ID")
	}
	if _, e := c.delete_record(context.Background(), &DeleteRecordIn{Collection: "posts", RecordID: "../bad"}); e == nil {
		t.Fatal("bad ID")
	}
}
func TestRecordValidationAndPrecision(t *testing.T) {
	raw := fixtures(t)[3].Response
	rec, e := recordValue([]byte(raw), "posts", "ae40239d2bc4477")
	if e != nil || !strings.Contains(rec.JSON, "9007199254740993") {
		t.Fatal(rec, e)
	}
	if _, e = recordValue([]byte(raw), "a98f514eb05f454", ""); e != nil {
		t.Fatal(e)
	}
	for _, b := range []string{`{}`, `{"id":1,"collectionId":"a","collectionName":"posts"}`, strings.Replace(raw, `"posts"`, `"other"`, 1), strings.Replace(raw, `ae40239d2bc4477`, `different`, 1), `null`, `{} {}`} {
		if _, e = recordValue([]byte(b), "posts", "ae40239d2bc4477"); e == nil {
			t.Fatal("invalid record", b)
		}
	}
}
func TestPaginationAndEmptyLookup(t *testing.T) {
	c := fresh()
	withReply(c, 200, `{"page":1,"perPage":1,"totalItems":0,"totalPages":0,"items":[]}`)
	o, e := c.find_record(context.Background(), &FindRecordIn{Collection: "posts", Filter: "title='missing'"})
	if e != nil || o.Found || len(o.Records) != 0 {
		t.Fatal(o, e)
	}
	base := fixtures(t)[1].Response
	for _, patch := range []map[string]any{{"page": 2}, {"perPage": 2}, {"totalItems": -1}, {"totalPages": -1}, {"totalItems": json.Number("9007199254740993"), "totalPages": json.Number("9007199254740993")}, {"items": nil}, {"items": []any{}}, {"totalPages": 1}, {"items": []any{json.RawMessage(fixtures(t)[3].Response), json.RawMessage(fixtures(t)[3].Response)}}, {"totalItems": "1e9"}} {
		var v map[string]any
		_ = decodeJSON([]byte(base), &v)
		for k, x := range patch {
			v[k] = x
		}
		b, _ := json.Marshal(v)
		withReply(c, 200, string(b))
		out, e := c.list_records(context.Background(), &ListRecordsIn{Collection: "posts", Options: schema.ListOptions{PerPage: ptr(1)}})
		if _, ok := patch["totalItems"].(json.Number); ok {
			if e != nil || out.Total != "9007199254740993" {
				t.Fatal(out, e)
			}
		} else if e == nil {
			t.Fatal("bad pagination accepted", patch, out)
		}
	}
	withReply(c, 200, `{"page":1,"perPage":30,"totalItems":0,"totalPages":0,"items":[]}`)
	list, e := c.list_records(context.Background(), &ListRecordsIn{Collection: "posts"})
	if e != nil || list.HasMore || list.NextPage != 0 || list.Total != "0" {
		t.Fatal(list, e)
	}
	withReply(c, 200, `{"page":1000000,"perPage":1,"totalItems":1000001,"totalPages":1000001,"items":[`+fixtures(t)[3].Response+`]}`)
	if _, e = c.list_records(context.Background(), &ListRecordsIn{Collection: "posts", Options: schema.ListOptions{Page: ptr(1000000), PerPage: ptr(1)}}); e == nil {
		t.Fatal("page cap")
	}
}
