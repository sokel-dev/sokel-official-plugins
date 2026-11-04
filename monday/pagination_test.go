package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/monday/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

const itemFixture = `{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":null,"column_values":[]}`

func TestItemPagination(t *testing.T) {
	for _, tc := range []struct {
		cursor string
		rows   string
		ok     bool
		more   bool
	}{{`null`, itemFixture, true, false}, {`"next+/="`, itemFixture, true, true}, {`"prior"`, itemFixture, false, false}, {`""`, itemFixture, false, false}, {`{}`, itemFixture, false, false}, {`"next"`, "", false, false}} {
		c := fresh()
		withReply(c, 200, `{"data":{"boards":[{"id":"9007199254740993","items_page":{"cursor":`+tc.cursor+`,"items":[`+tc.rows+`]}}]}}`)
		o, e := c.list_items(context.Background(), &ListItemsIn{BoardID: "9007199254740993", Page: schema.Page{Cursor: ptr("prior")}})
		if (e == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, e)
		}
		if tc.ok && (o.HasMore != tc.more || (len(o.Items) > 0 && o.Items[0].ID != "9007199254740995")) {
			t.Fatal(o)
		}
	}
	for _, p := range []schema.Page{{Limit: ptr(0)}, {Limit: ptr(501)}, {Cursor: ptr("")}, {Cursor: ptr("\x00")}} {
		if _, e := fresh().list_items(context.Background(), &ListItemsIn{BoardID: "1", Page: p}); e == nil {
			t.Fatal(p)
		}
	}
}
func TestBoardPagination(t *testing.T) {
	c := fresh()
	withReply(c, 200, `{"data":{"boards":[{"id":"1","name":"Board","state":"active","url":"https://acme.monday.com/boards/1"}]}}`)
	o, e := c.list_boards(context.Background(), &ListBoardsIn{Page: schema.BoardPage{Limit: ptr(1), Number: ptr(2)}})
	if e != nil || o.NextPage != 3 {
		t.Fatal(o, e)
	}
	for _, p := range []schema.BoardPage{{Limit: ptr(0)}, {Limit: ptr(101)}, {Number: ptr(0)}, {Number: ptr(10001)}} {
		if _, e := fresh().list_boards(context.Background(), &ListBoardsIn{Page: p}); e == nil {
			t.Fatal(p)
		}
	}
}
func TestGraphQLVariablesAndValidation(t *testing.T) {
	c := fresh()
	name := "A\" ) { delete_board(id:1) { id } } #"
	columns := `{"numbers":9007199254740993,"check":{"checked":false}}`
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		var env struct {
			Query     string                     `json:"query"`
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if json.Unmarshal(b, &env) != nil || strings.Contains(env.Query, name) {
			t.Fatal(string(b))
		}
		var n, v string
		_ = json.Unmarshal(env.Variables["name"], &n)
		_ = json.Unmarshal(env.Variables["values"], &v)
		if n != name || v != columns || string(env.Variables["labels"]) != "false" {
			t.Fatal(string(b))
		}
		return httpResponse(200, `{"data":{"create_item":`+itemFixture+`}}`), nil
	})
	o, e := c.create_item(context.Background(), &CreateItemIn{BoardID: "9007199254740993", Name: name, ColumnValuesJSON: columns, Options: schema.MutationOptions{CreateLabels: ptr(false)}})
	if e != nil || o.Item.ID != "9007199254740995" {
		t.Fatal(o, e)
	}
	for _, s := range []string{"null", "[]", "{}", "{", "{} {}", strings.Repeat("x", (256<<10)+1)} {
		if columnJSON(s) == nil {
			t.Fatal(s)
		}
	}
	for _, id := range []string{"0", "-1", "1.5", "https://evil", "1/../2", "1?key=secret", "01", strings.Repeat("1", 41)} {
		if checkID(id) == nil {
			t.Fatal(id)
		}
	}
	c = fresh()
	c.http.Transport = transport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input sent"); return nil, nil })
	for _, in := range []*CreateItemIn{{BoardID: "1", Name: " "}, {BoardID: "1", Name: "n", GroupID: "../x"}, {BoardID: "1", Name: "n", ColumnValuesJSON: "[]"}} {
		if _, e := c.create_item(context.Background(), in); e == nil {
			t.Fatal(in)
		}
	}
	if _, e := c.add_update(context.Background(), &AddUpdateIn{ItemID: "1", Body: " "}); e == nil {
		t.Fatal("empty update")
	}
}
