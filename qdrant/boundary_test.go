package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/qdrant/schema"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestPointIDsAndPrecision(t *testing.T) {
	for _, s := range []string{"0", "18446744073709551615", "76874CCE-1FB9-4E16-9B0B-F085AC06ED6F"} {
		wire, key, e := pointID(s)
		if e != nil || key != strings.ToLower(s) {
			t.Fatal(wire, key, e)
		}
		b, _ := json.Marshal(wire)
		got, e := readID(b)
		if e != nil || got != key {
			t.Fatal(got, e)
		}
	}
	for _, s := range []string{"-1", "01", "1.0", "1e3", "18446744073709551616", "not-uuid", "", "../1"} {
		if _, _, e := pointID(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{`null`, `[]`, `{}`, `"bad"`, `true`} {
		if _, e := readID([]byte(s)); e == nil {
			t.Fatal(s)
		}
	}
	rows, e := parsePoints([]byte(`[{"id":18446744073709551615,"payload":{"amount":9007199254740993,"flag":false},"score":0}]`), 1, true)
	if e != nil || rows[0].ID != "18446744073709551615" || !strings.Contains(*rows[0].PayloadJSON, "9007199254740993") || *rows[0].Score != 0 {
		t.Fatal(rows, e)
	}
	for _, s := range []string{`[{"id":1}]`, `[{"id":1,"score":null}]`, `[{"id":1,"score":1},{"id":1,"score":1}]`, `[{"id":1,"score":1,"payload":[]}]`} {
		if _, e := parsePoints([]byte(s), 2, true); e == nil {
			t.Fatal(s)
		}
	}
	if _, _, e := idsInput([]string{"76874CCE-1FB9-4E16-9B0B-F085AC06ED6F", "76874cce-1fb9-4e16-9b0b-f085ac06ed6f"}); e == nil {
		t.Fatal("UUID duplicate not normalized")
	}
}
func TestScrollAndWriteCompletion(t *testing.T) {
	for _, tc := range []struct {
		next, rows string
		ok         bool
	}{{`null`, `[{"id":0}]`, true}, {`1`, `[{"id":0}]`, true}, {`0`, `[{"id":0}]`, false}, {`1`, `[]`, false}, {`"bad"`, `[{"id":0}]`, false}} {
		c := fresh()
		withReply(c, 200, `{"status":"ok","result":{"points":`+tc.rows+`,"next_page_offset":`+tc.next+`}}`)
		o, e := c.scroll_points(context.Background(), &ScrollPointsIn{Collection: "books", Options: schema.ScrollOptions{Offset: ptr("0")}})
		if (e == nil) != tc.ok {
			t.Fatal(o, e)
		}
	}
	for _, s := range []string{`{"status":"acknowledged","operation_id":0}`, `{"status":"clock_rejected"}`, `{"status":"completed","operation_id":-1}`, `{"status":"completed","operation_id":18446744073709551616}`} {
		if _, e := writeReceipt([]byte(s)); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{`{"status":"completed","operation_id":0}`, `{"status":"completed","operation_id":null}`} {
		if _, e := writeReceipt([]byte(s)); e != nil {
			t.Fatal(s, e)
		}
	}
	c := fresh()
	withReply(c, 200, `{"status":"ok","result":[{"id":2}]}`)
	if _, e := c.retrieve_points(context.Background(), &RetrievePointsIn{Collection: "books", IDs: []string{"1"}}); e == nil {
		t.Fatal("unrequested point")
	}
}
func TestDenseNamedVectorWire(t *testing.T) {
	c := fresh()
	wire(t, c, "PUT", "/collections/books", "timeout=30", `{"vectors":{"text":{"distance":"Dot","size":2}}}`, `{"status":"ok","result":true}`, 200)
	if _, e := c.create_collection(context.Background(), &CreateCollectionIn{Collection: "books", VectorName: "text", Size: 2, Distance: "Dot"}); e != nil {
		t.Fatal(e)
	}
	c = fresh()
	wire(t, c, "PUT", "/collections/books/points", "timeout=30&wait=true", `{"points":[{"id":0,"payload":{},"vector":{"text":[1,0]}}]}`, `{"status":"ok","result":{"status":"completed"}}`, 200)
	if _, e := c.upsert_points(context.Background(), &UpsertPointsIn{Collection: "books", VectorName: "text", Points: []schema.PointInput{{ID: "0", Vector: []float64{1, 0}, PayloadJSON: ptr(`{}`)}}}); e != nil {
		t.Fatal(e)
	}
	c = fresh()
	wire(t, c, "POST", "/collections/books/points/query", "timeout=30", `{"filter":{"must":[{"key":"enabled","match":{"value":false}}]},"limit":1,"offset":0,"query":[1,0],"score_threshold":0,"using":"text","with_payload":true,"with_vector":false}`, `{"status":"ok","result":{"points":[]}}`, 200)
	if _, e := c.query_points(context.Background(), bind[QueryPointsIn](t, `{"collection":"books","vector":[1,0],"vector_name":"text","options":{"limit":1,"offset":0,"score_threshold":0,"filter_json":"{\"must\":[{\"key\":\"enabled\",\"match\":{\"value\":false}}]}"}}`)); e != nil {
		t.Fatal(e)
	}
}
func TestInvalidBusinessInputsNoRequest(t *testing.T) {
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	for _, in := range []*CreateCollectionIn{{Collection: "../x", Size: 3, Distance: "Dot"}, {Collection: "x", Size: 0, Distance: "Dot"}, {Collection: "x", Size: 3, Distance: "Bad"}, {Collection: "x", Size: 3, Distance: "Dot", VectorName: "\x00"}} {
		if _, e := c.create_collection(context.Background(), in); e == nil {
			t.Fatal(in)
		}
	}
	for _, v := range [][]float64{nil, {math.NaN()}, {math.Inf(1)}} {
		if vectorOK(v) == nil {
			t.Fatal(v)
		}
	}
	for _, s := range []string{"null", "[]", "{} {}", strings.Repeat("a", (512<<10)+1)} {
		if _, e := objectJSON(s); e == nil {
			t.Fatal("invalid object")
		}
	}
	for _, opts := range []schema.QueryOptions{{Limit: ptr(0)}, {Offset: ptr(-1)}, {ScoreThreshold: ptr(math.Inf(1))}, {FilterJSON: ptr("[]")}} {
		if _, e := c.query_points(context.Background(), &QueryPointsIn{Collection: "books", Vector: []float64{1}, Options: opts}); e == nil {
			t.Fatal(opts)
		}
	}
	for _, points := range [][]schema.PointInput{nil, {{ID: "0", Vector: []float64{1}}, {ID: "0", Vector: []float64{1}}}, {{ID: "0", Vector: []float64{1}}, {ID: "1", Vector: []float64{1, 2}}}, {{ID: "0", Vector: []float64{1}, PayloadJSON: ptr("null")}}} {
		if _, e := c.upsert_points(context.Background(), &UpsertPointsIn{Collection: "books", Points: points}); e == nil {
			t.Fatal(points)
		}
	}
}
