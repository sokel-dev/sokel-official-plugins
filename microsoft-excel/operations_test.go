package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGeneratedSessionAndWorkbookDiscovery(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/drives/drive/items/book/workbook/createSession" && r.Header.Get("workbook-session-id") != "session-key" {
			t.Fatal("session header missing")
		}
		switch r.URL.Path {
		case "/v1.0/drives/drive/items/book/workbook/createSession":
			b, _ := io.ReadAll(r.Body)
			if string(b) != `{"persistChanges":false}` {
				t.Fatalf("false lost %s", b)
			}
			w.WriteHeader(201)
			reply(w, `{"id":"session-key","persistChanges":false}`)
		case "/v1.0/drives/drive/items/book/workbook/closeSession":
			if r.Method != "POST" {
				t.Fatal("close method")
			}
			w.WriteHeader(204)
		case "/v1.0/drives/drive/items/book/workbook/worksheets":
			reply(w, `{"value":[{"id":"sheet","name":"财务","position":0,"visibility":"Visible"}]}`)
		case "/v1.0/drives/drive/items/book/workbook/tables":
			reply(w, `{"value":[{"id":"table","name":"Data","showHeaders":false}]}`)
		default:
			t.Fatal(r.URL.Path)
		}
	})
	v := invoke(t, c, "create_session", `{"drive_id":"drive","workbook_id":"book","persist_changes":false}`, nil)
	if v["persist_changes"] != false {
		t.Fatal(v)
	}
	v = invoke(t, c, "list_worksheets", `{"drive_id":"drive","workbook_id":"book","session_id":"session-key"}`, nil)
	if v["worksheets"].([]any)[0].(map[string]any)["name"] != "财务" {
		t.Fatal(v)
	}
	invoke(t, c, "list_tables", `{"drive_id":"drive","workbook_id":"book","session_id":"session-key"}`, nil)
	v = invoke(t, c, "close_session", `{"drive_id":"drive","workbook_id":"book","session_id":"session-key"}`, nil)
	if v["closed"] != true {
		t.Fatal(v)
	}
}
func TestGeneratedRangeAndAppendWireValues(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("workbook-session-id") != "session-key" {
			t.Fatal("missing session")
		}
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1.0/drives/drive/items/book/workbook/worksheets/财务/range(address='A1:C2')":
			if r.Method == "PATCH" && string(b) != `{"values":[["你好",false,0],["",null,1.5]]}` {
				t.Fatalf("cell bytes %s", b)
			}
			reply(w, `{"address":"财务!A1:C2","rowCount":2,"columnCount":3,"values":[["你好",false,0],["",null,1.5]]}`)
		case "/v1.0/drives/drive/items/book/workbook/tables/table/rows/add":
			if r.Method != "POST" || string(b) != `{"values":[["one",0],["two",false]]}` {
				t.Fatalf("append bytes %s", b)
			}
			w.WriteHeader(201)
			reply(w, `{"index":4,"values":[["one",0],["two",false]]}`)
		default:
			t.Fatal(r.URL.Path)
		}
	})
	invoke(t, c, "get_range", `{"drive_id":"drive","workbook_id":"book","session_id":"session-key","worksheet_id":"财务","address":"A1:C2"}`, nil)
	v := invoke(t, c, "update_range", `{"drive_id":"drive","workbook_id":"book","session_id":"session-key","worksheet_id":"财务","address":"A1:C2","values":[["你好",false,0],["",null,1.5]]}`, nil)
	if v["range"].(map[string]any)["row_count"] != int64(2) {
		t.Fatal(v)
	}
	v = invoke(t, c, "append_rows", `{"drive_id":"drive","workbook_id":"book","session_id":"session-key","table_id":"table","values":[["one",0],["two",false]]}`, nil)
	if v["row"].(map[string]any)["index"] != int64(4) {
		t.Fatal(v)
	}
}
func TestRowPagingAndValidation(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$top") != "100" || r.URL.Query().Get("$skip") != "100" {
			t.Fatal("row page query")
		}
		reply(w, `{"value":[{"index":100,"values":[["last",1]]}]}`)
	})
	v := invoke(t, c, "list_rows", `{"drive_id":"drive","workbook_id":"book","table_id":"table","skip":"100"}`, nil)
	if v["has_more"] != false || v["next_skip"] != "" {
		t.Fatal(v)
	}
	for _, a := range []string{"A:A", "A0", "XFE1", "A1048577", "A1:XFD2", "Sheet1!A1", "A2:A1", "A1')/usedRange"} {
		if _, _, _, e := rangeShape(a); e == nil {
			t.Fatalf("unsafe/unbounded address %s", a)
		}
	}
	for _, values := range [][]any{nil, {[]any{}}, {[]any{1}, []any{1, 2}}, {[]any{map[string]any{"x": 1}}}, {[]any{json.Number("NaN")}}} {
		if _, _, e := matrixShape(values); e == nil {
			t.Fatal("invalid matrix")
		}
	}
	if _, e := c.updateRange(context.Background(), &UpdateRangeIn{DriveID: "drive", WorkbookID: "book", WorksheetID: "sheet", Address: "A1:B2", Values: []any{[]any{1}}}); e == nil {
		t.Fatal("matrix mismatch")
	}
	if _, e := c.listRows(context.Background(), &ListRowsIn{DriveID: "drive", WorkbookID: "book", TableID: "table", Skip: "-1"}); e == nil {
		t.Fatal("negative offset")
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{"id":"session","persistChanges":true}`) })
	if _, e := c.createSession(context.Background(), &CreateSessionIn{DriveID: "drive", WorkbookID: "book", PersistChanges: false}); e == nil {
		t.Fatal("changed persistence")
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if _, e := c.getRange(context.Background(), &GetRangeIn{DriveID: "drive", WorkbookID: "book", WorksheetID: "sheet", Address: "A1"}); e == nil {
		t.Fatal("malformed range")
	}
}
func TestAppendRejectsTruncatedReceipt(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{"index":3,"values":[["one",1]]}`) })
	_, e := c.appendRows(context.Background(), &AppendRowsIn{DriveID: "drive", WorkbookID: "book", TableID: "table", Values: []any{[]any{"one", 1}, []any{"two", 2}}})
	if e == nil || !strings.Contains(e.Error(), "receipt") {
		t.Fatal("append silently accepted a truncated receipt")
	}
}

func TestRowsRejectShortPageWithServerContinuation(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		reply(w, `{"value":[{"index":0,"values":[["first"]]}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/drives/drive/items/book/workbook/tables/table/rows?$skiptoken=opaque"}`)
	})
	_, err := c.listRows(context.Background(), &ListRowsIn{DriveID: "drive", WorkbookID: "book", TableID: "table"})
	if err == nil || !strings.Contains(err.Error(), "continuation") {
		t.Fatal("short page with a server continuation was silently reported as complete")
	}
}
