package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/google-sheets/schema"
	"net/url"
	"strings"
)

const sheetsOrigin = "https://sheets.googleapis.com"

func sheetPath(id string) (string, error) {
	id, e := segment(id, "spreadsheet_id")
	return "/v4/spreadsheets/" + id, e
}
func (a *api) metadata(ctx context.Context, id string) (*GetSpreadsheetOut, error) {
	p, e := sheetPath(id)
	if e != nil {
		return nil, e
	}
	var r struct {
		SpreadsheetID string `json:"spreadsheetId"`
		Properties    struct {
			Title    string `json:"title"`
			TimeZone string `json:"timeZone"`
		} `json:"properties"`
		Sheets []struct {
			Properties struct {
				SheetID int    `json:"sheetId"`
				Title   string `json:"title"`
				Grid    struct {
					Rows int `json:"rowCount"`
					Cols int `json:"columnCount"`
				} `json:"gridProperties"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if e = a.json(ctx, "GET", sheetsOrigin, p, url.Values{"fields": {"spreadsheetId,properties(title,timeZone),sheets(properties(sheetId,title,gridProperties))"}}, nil, &r); e != nil {
		return nil, e
	}
	out := &GetSpreadsheetOut{SpreadsheetID: r.SpreadsheetID, Title: r.Properties.Title, TimeZone: r.Properties.TimeZone, Sheets: []schema.Sheet{}}
	for _, s := range r.Sheets {
		out.Sheets = append(out.Sheets, schema.Sheet{ID: s.Properties.SheetID, Title: s.Properties.Title, RowCount: s.Properties.Grid.Rows, ColumnCount: s.Properties.Grid.Cols})
	}
	return out, nil
}
func (a *api) read(ctx context.Context, in *ReadValuesIn) (*ReadValuesOut, error) {
	p, e := sheetPath(in.SpreadsheetID)
	if e != nil {
		return nil, e
	}
	rng, e := segment(in.Range, "range")
	if e != nil {
		return nil, e
	}
	render := in.ValueRenderOption
	if render == "" {
		render = "FORMATTED_VALUE"
	}
	if render != "FORMATTED_VALUE" && render != "UNFORMATTED_VALUE" && render != "FORMULA" {
		return nil, fmt.Errorf("unsupported value_render_option")
	}
	var out ReadValuesOut
	e = a.json(ctx, "GET", sheetsOrigin, p+"/values/"+rng, url.Values{"majorDimension": {"ROWS"}, "valueRenderOption": {render}}, nil, &out)
	if out.Values == nil {
		out.Values = []any{}
	}
	return &out, e
}
func valueMode(mode string) (string, error) {
	if mode == "" {
		return "RAW", nil
	}
	if mode != "RAW" && mode != "USER_ENTERED" {
		return "", fmt.Errorf("value_input_option must be RAW or USER_ENTERED")
	}
	return mode, nil
}
func validateValues(rows []any) error {
	if len(rows) == 0 || len(rows) > 10000 {
		return fmt.Errorf("values must contain 1 to 10000 rows")
	}
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok {
			return fmt.Errorf("each values row must be a JSON array")
		}
		if len(row) > 10000 {
			return fmt.Errorf("a row may contain at most 10000 cells")
		}
		for _, cell := range row {
			switch cell.(type) {
			case nil, string, bool, float64, float32, int, int64, int32, json.Number:
			default:
				return fmt.Errorf("cells must be strings, numbers, booleans or null")
			}
		}
	}
	return nil
}
func (a *api) write(ctx context.Context, id, rng string, rows []any, mode string, appendRows bool) (*UpdateValuesOut, error) {
	if e := validateValues(rows); e != nil {
		return nil, e
	}
	mode, e := valueMode(mode)
	if e != nil {
		return nil, e
	}
	p, e := sheetPath(id)
	if e != nil {
		return nil, e
	}
	r, e := segment(rng, "range")
	if e != nil {
		return nil, e
	}
	q := url.Values{"valueInputOption": {mode}}
	method := "PUT"
	suffix := ""
	if appendRows {
		method = "POST"
		suffix = ":append"
		q.Set("insertDataOption", "INSERT_ROWS")
	}
	var response struct {
		UpdatedRange string `json:"updatedRange"`
		UpdatedRows  int    `json:"updatedRows"`
		UpdatedCells int    `json:"updatedCells"`
		Updates      struct {
			Range string `json:"updatedRange"`
			Rows  int    `json:"updatedRows"`
			Cells int    `json:"updatedCells"`
		} `json:"updates"`
	}
	e = a.json(ctx, method, sheetsOrigin, p+"/values/"+r+suffix, q, map[string]any{"range": rng, "majorDimension": "ROWS", "values": rows}, &response)
	if e != nil {
		return nil, e
	}
	if appendRows {
		return &UpdateValuesOut{UpdatedRange: response.Updates.Range, UpdatedRows: response.Updates.Rows, UpdatedCells: response.Updates.Cells}, nil
	}
	return &UpdateValuesOut{UpdatedRange: response.UpdatedRange, UpdatedRows: response.UpdatedRows, UpdatedCells: response.UpdatedCells}, nil
}
func (a *api) batch(ctx context.Context, in *BatchUpdateValuesIn) (*BatchUpdateValuesOut, error) {
	if len(in.Data) == 0 || len(in.Data) > 100 {
		return nil, fmt.Errorf("data must contain 1 to 100 ranges")
	}
	for _, r := range in.Data {
		if strings.TrimSpace(r.Range) == "" {
			return nil, fmt.Errorf("every batch range is required")
		}
		if e := validateValues(r.Values); e != nil {
			return nil, e
		}
	}
	mode, e := valueMode(in.ValueInputOption)
	if e != nil {
		return nil, e
	}
	p, e := sheetPath(in.SpreadsheetID)
	if e != nil {
		return nil, e
	}
	var out struct {
		Rows  int `json:"totalUpdatedRows"`
		Cells int `json:"totalUpdatedCells"`
	}
	e = a.json(ctx, "POST", sheetsOrigin, p+"/values:batchUpdate", nil, map[string]any{"valueInputOption": mode, "data": in.Data}, &out)
	return &BatchUpdateValuesOut{UpdatedRows: out.Rows, UpdatedCells: out.Cells}, e
}

type rowCursor struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	Hash  string `json:"hash"`
}

func rowsHash(rows []any) string { b, _ := json.Marshal(rows); return eventKey(string(b)) }
func (a *api) poll(ctx context.Context, cred Cred, raw string, emit func(string, *RowAddedEvent) error) (string, error) {
	out, e := a.read(ctx, &ReadValuesIn{SpreadsheetID: cred.WatchSpreadsheetID, Range: cred.WatchRange, ValueRenderOption: "UNFORMATTED_VALUE"})
	if e != nil {
		return raw, e
	}
	if len(out.Values) > 10000 {
		return raw, fmt.Errorf("watched range exceeds 10000 populated rows; narrow the range")
	}
	key := eventKey(cred.WatchSpreadsheetID, cred.WatchRange)
	cur := rowCursor{Key: key}
	if raw != "" {
		if json.Unmarshal([]byte(raw), &cur) != nil {
			return raw, fmt.Errorf("invalid polling cursor; clear it to establish a new baseline")
		}
		if cur.Key != key {
			cur = rowCursor{Key: key}
		} else {
			if cur.Count < 0 || cur.Count > len(out.Values) || rowsHash(out.Values[:cur.Count]) != cur.Hash {
				return raw, fmt.Errorf("watched rows changed or were removed; append-only polling requires clearing the cursor to rebaseline")
			}
			for i := cur.Count; i < len(out.Values); i++ {
				row, ok := out.Values[i].([]any)
				if !ok {
					return raw, fmt.Errorf("Google returned an invalid row")
				}
				ev := &RowAddedEvent{SpreadsheetID: cred.WatchSpreadsheetID, Range: cred.WatchRange, RowOffset: i + 1, Values: row}
				if e = emit(eventKey(key, fmt.Sprint(i+1), rowsHash([]any{row})), ev); e != nil {
					return raw, e
				}
			}
		}
	}
	next, e := json.Marshal(rowCursor{Key: key, Count: len(out.Values), Hash: rowsHash(out.Values)})
	return string(next), e
}
