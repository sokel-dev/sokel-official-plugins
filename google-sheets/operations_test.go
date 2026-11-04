package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/google-sheets/schema"
	"net/http"
	"testing"
)

func TestSheetsReadMetadataAndWrites(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			if r.URL.Query().Get("fields") == "" {
				t.Error("missing metadata projection")
			}
			reply(w, `{"spreadsheetId":"sheet","properties":{"title":"预算","timeZone":"Asia/Shanghai"},"sheets":[{"properties":{"sheetId":1,"title":"Sheet1","gridProperties":{"rowCount":100,"columnCount":5}}}]}`)
		case 2:
			if r.URL.Query().Get("valueRenderOption") != "UNFORMATTED_VALUE" {
				t.Error("render option")
			}
			reply(w, `{"range":"Sheet1!A1:B2","values":[["a",1],[true,null]]}`)
		case 3, 4:
			if r.URL.Query().Get("valueInputOption") != "RAW" {
				t.Error("must default RAW")
			}
			if calls == 3 && (r.Method != "PUT" || r.URL.Path != "/v4/spreadsheets/sheet/values/Sheet1!A1:B2") {
				t.Errorf("update path %s %s", r.Method, r.URL.Path)
			}
			if calls == 4 && (r.Method != "POST" || r.URL.Query().Get("insertDataOption") != "INSERT_ROWS") {
				t.Error("append semantics")
			}
			var b struct {
				Values [][]any `json:"values"`
			}
			if json.NewDecoder(r.Body).Decode(&b) != nil || len(b.Values) != 1 || b.Values[0][0] != "=literal" {
				t.Error("values wire format")
			}
			if calls == 4 {
				reply(w, `{"updates":{"updatedRange":"Sheet1!A3","updatedRows":1,"updatedCells":1}}`)
			} else {
				reply(w, `{"updatedRange":"Sheet1!A1","updatedRows":1,"updatedCells":1}`)
			}
		case 5:
			var b struct {
				Data   []schema.ValueRange `json:"data"`
				Option string              `json:"valueInputOption"`
			}
			if json.NewDecoder(r.Body).Decode(&b) != nil || len(b.Data) != 1 || b.Option != "USER_ENTERED" {
				t.Error("batch body")
			}
			reply(w, `{"totalUpdatedRows":1,"totalUpdatedCells":2}`)
		}
	})
	m, e := a.metadata(context.Background(), "sheet")
	if e != nil || m.Title != "预算" || m.Sheets[0].RowCount != 100 {
		t.Fatalf("metadata: %v %v", m, e)
	}
	v, e := a.read(context.Background(), &ReadValuesIn{SpreadsheetID: "sheet", Range: "Sheet1!A1:B2", ValueRenderOption: "UNFORMATTED_VALUE"})
	if e != nil || len(v.Values) != 2 {
		t.Fatalf("read: %v %v", v, e)
	}
	for _, appendRows := range []bool{false, true} {
		o, e := a.write(context.Background(), "sheet", "Sheet1!A1:B2", []any{[]any{"=literal"}}, "", appendRows)
		if e != nil || o.UpdatedCells != 1 {
			t.Fatalf("write %v %v", o, e)
		}
	}
	o, e := a.batch(context.Background(), &BatchUpdateValuesIn{SpreadsheetID: "sheet", Data: []schema.ValueRange{{Range: "A1:B1", Values: []any{[]any{1, 2}}}}, ValueInputOption: "USER_ENTERED"})
	if e != nil || o.UpdatedCells != 2 {
		t.Fatalf("batch %v %v", o, e)
	}
}
func TestSheetsRejectInvalidRowsAndOptions(t *testing.T) {
	a := newAPI("token")
	for _, rows := range [][]any{{"not a row"}, {[]any{map[string]any{"x": 1}}}, nil} {
		if _, e := a.write(context.Background(), "s", "A1", rows, "RAW", false); e == nil {
			t.Fatal("invalid values accepted")
		}
	}
	if _, e := a.write(context.Background(), "s", "A1", []any{[]any{1}}, "typo", false); e == nil {
		t.Fatal("invalid mode accepted")
	}
}
func TestSheetsAppendOnlyCursor(t *testing.T) {
	body := `{"values":[["header"],["old"]]}`
	a := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
	cred := Cred{WatchSpreadsheetID: "s", WatchRange: "A1:B100"}
	emitted := 0
	emit := func(_ string, e *RowAddedEvent) error {
		emitted++
		if e.RowOffset != 3 {
			t.Errorf("offset %d", e.RowOffset)
		}
		return nil
	}
	cur, e := a.poll(context.Background(), cred, "", emit)
	if e != nil || cur == "" || emitted != 0 {
		t.Fatalf("baseline %v", e)
	}
	body = `{"values":[["header"],["old"],["new"]]}`
	if _, e = a.poll(context.Background(), cred, cur, func(string, *RowAddedEvent) error { return errors.New("delivery failed") }); e == nil {
		t.Fatal("delivery failure lost")
	}
	next, e := a.poll(context.Background(), cred, cur, emit)
	if e != nil || next == cur || emitted != 1 {
		t.Fatalf("append %v", e)
	}
	body = `{"values":[["header"],["edited"],["new"]]}`
	if _, e = a.poll(context.Background(), cred, next, emit); e == nil {
		t.Fatal("edit must require explicit rebaseline")
	}
}
