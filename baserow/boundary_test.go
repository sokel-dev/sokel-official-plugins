package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/baserow/schema"
	"net/http"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }
func TestIdentifiersAndDynamicJSON(t *testing.T) {
	for _, s := range []string{"", "0", "-1", "01", "1e3", "1.0", "9223372036854775808", "../../x"} {
		if positiveID(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"1", "9007199254740993", "9223372036854775807"} {
		if !positiveID(s) {
			t.Fatal(s)
		}
	}
	raw := `{"id":9007199254740993,"error":"business field","code":0,"flag":false,"nested":{"value":9007199254740993}}`
	v, e := rowValue([]byte(raw), "9007199254740993")
	if e != nil || v.JSON != raw || v.ID != "9007199254740993" {
		t.Fatal(v, e)
	}
	for _, s := range []string{`{"id":0}`, `{"id":1e3}`, `{"id":9223372036854775808}`, `{"id":1}`} {
		if _, e = rowValue([]byte(s), "2"); e == nil {
			t.Fatal("bad row", s)
		}
	}
	c := fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil })
	for _, s := range []string{`null`, `[]`, `{}`, `{"id":1}`, `{"order":0}`, `{`, strings.Repeat("x", maxRequest+1), string([]byte{'{', 0xff, '}'})} {
		if _, e = c.update_row(context.Background(), &UpdateRowIn{TableID: "8", RowID: "1", DataJSON: s}); e == nil {
			t.Fatal("bad object", s[:min(len(s), 30)])
		}
	}
	if _, e = c.get_row(context.Background(), &GetRowIn{TableID: "8", RowID: "-1"}); e == nil {
		t.Fatal("invalid ID")
	}
	if _, e = c.delete_row(context.Background(), &DeleteRowIn{TableID: "../x", RowID: "1"}); e == nil {
		t.Fatal("invalid table")
	}
	withReply(c, 200, `{"id":1}`)
	if _, e = c.create_row(context.Background(), &CreateRowIn{TableID: "8", DataJSON: `{}`}); e != nil {
		t.Fatal("empty creation should use field defaults", e)
	}
}
func TestRowPaginationValidation(t *testing.T) {
	f := fixtures(t)[3]
	var original map[string]any
	if e := decodeJSON([]byte(f.Response), &original); e != nil {
		t.Fatal(e)
	}
	tests := []struct {
		name      string
		patch     map[string]any
		wantError bool
	}{
		{"terminal", map[string]any{"count": json.Number("1"), "next": nil}, false},
		{"large count", map[string]any{"count": json.Number("9007199254740993")}, false},
		{"foreign host", map[string]any{"next": "https://evil.example/api/database/rows/table/8/?page=2&size=1&user_field_names=false"}, true},
		{"repeated", map[string]any{"next": "http://service.internal/prefix/api/database/rows/table/8/?page=1&size=1&user_field_names=false"}, true},
		{"wrong query", map[string]any{"next": "http://service.internal/prefix/api/database/rows/table/8/?page=2&size=1&user_field_names=false&token=secret"}, true},
		{"missing next", map[string]any{"next": nil}, true},
		{"empty with next", map[string]any{"results": []any{}}, true},
		{"null rows", map[string]any{"results": nil}, true},
		{"negative total", map[string]any{"count": -1}, true},
		{"zero total with rows", map[string]any{"count": 0, "next": nil}, true},
		{"invalid next type", map[string]any{"next": false}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v map[string]any
			_ = decodeJSON([]byte(f.Response), &v)
			for k, x := range tt.patch {
				v[k] = x
			}
			b, _ := json.Marshal(v)
			c := fresh()
			withReply(c, 200, string(b))
			o, e := c.list_rows(context.Background(), &ListRowsIn{TableID: "8", Options: schema.RowOptions{Size: ptr(1), UserFieldNames: ptr(false)}})
			if (e != nil) != tt.wantError {
				t.Fatal(o, e)
			}
			if tt.name == "large count" && o.Total != "9007199254740993" {
				t.Fatal(o)
			}
		})
	}
}
func TestRowQueryAndInputBounds(t *testing.T) {
	c := fresh()
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Get("user_field_names") != "true" || q.Get("search") != "false" || q.Get("order_by") != "-Name" || q.Get("filters") != `{"filter_type":"AND","filters":[{"field":"Done","type":"boolean","value":"false"}]}` {
			t.Fatal(q)
		}
		return httpResponse(200, `{"count":0,"next":null,"previous":null,"results":[]}`), nil
	})
	f := `{"filter_type":"AND","filters":[{"field":"Done","type":"boolean","value":"false"}]}`
	if _, e := c.list_rows(context.Background(), &ListRowsIn{TableID: "8", Options: schema.RowOptions{Search: ptr("false"), OrderBy: ptr("-Name"), FiltersJSON: &f, UserFieldNames: ptr(true)}}); e != nil {
		t.Fatal(e)
	}
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("bad query reached transport"); return nil, nil })
	for _, o := range []schema.RowOptions{{Page: ptr(0)}, {Size: ptr(201)}, {Page: ptr(1000001)}, {Search: ptr(strings.Repeat("x", 16385))}, {OrderBy: ptr("a\x00")}, {FiltersJSON: ptr(`[]`)}, {FiltersJSON: ptr(strings.Repeat("x", 16385))}} {
		if _, e := c.list_rows(context.Background(), &ListRowsIn{TableID: "8", Options: o}); e == nil {
			t.Fatal("bad options", o)
		}
	}
}
