package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func workbookPath(drive, book string) (string, error) {
	d, e := segment(drive)
	if e != nil {
		return "", e
	}
	b, e := segment(book)
	return "/v1.0/drives/" + d + "/items/" + b + "/workbook", e
}
func sessionHeaders(id string) (http.Header, error) {
	if !safeHeader(id) {
		return nil, errors.New("Invalid workbook session ID.")
	}
	h := http.Header{}
	if id != "" {
		h.Set("workbook-session-id", id)
	}
	return h, nil
}
func (c *client) createSession(ctx context.Context, in *CreateSessionIn) (*CreateSessionOut, error) {
	p, e := workbookPath(in.DriveID, in.WorkbookID)
	if e != nil {
		return nil, e
	}
	var r struct {
		ID      string `json:"id"`
		Persist *bool  `json:"persistChanges"`
	}
	if e = c.json(ctx, "POST", graphOrigin+p+"/createSession", map[string]any{"persistChanges": in.PersistChanges}, nil, &r, 201, 200); e != nil {
		return nil, e
	}
	if r.ID == "" || !safeHeader(r.ID) || r.Persist == nil || *r.Persist != in.PersistChanges {
		return nil, errors.New("Microsoft Graph returned an invalid workbook session receipt. Inspect the workbook before retrying.")
	}
	return &CreateSessionOut{SessionID: r.ID, PersistChanges: *r.Persist}, nil
}
func (c *client) closeSession(ctx context.Context, in *CloseSessionIn) (*CloseSessionOut, error) {
	p, e := workbookPath(in.DriveID, in.WorkbookID)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.SessionID) == "" {
		return nil, errors.New("Provide the workbook session ID to close.")
	}
	h, e := sessionHeaders(in.SessionID)
	if e != nil {
		return nil, e
	}
	_, _, e = c.send(ctx, "POST", graphOrigin+p+"/closeSession", nil, h, 204)
	if e != nil {
		return nil, e
	}
	return &CloseSessionOut{Closed: true}, nil
}
func (c *client) collection(ctx context.Context, path, link, session string, q url.Values) ([]json.RawMessage, string, error) {
	target, e := pageURL(path, link, q)
	if e != nil {
		return nil, "", e
	}
	h, e := sessionHeaders(session)
	if e != nil {
		return nil, "", e
	}
	var p struct {
		Value []json.RawMessage `json:"value"`
		Next  string            `json:"@odata.nextLink"`
	}
	if e = c.json(ctx, "GET", target, nil, h, &p, 200); e != nil {
		return nil, "", e
	}
	if p.Value == nil {
		return nil, "", errors.New("Microsoft Graph response is missing the workbook collection.")
	}
	next, e := checkedNext(path, target, p.Next)
	return p.Value, next, e
}
func (c *client) listWorksheets(ctx context.Context, in *ListWorksheetsIn) (*ListWorksheetsOut, error) {
	p, e := workbookPath(in.DriveID, in.WorkbookID)
	if e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, p+"/worksheets", in.NextLink, in.SessionID, nil)
	if e != nil {
		return nil, e
	}
	out := &ListWorksheetsOut{Worksheets: []Worksheet{}, NextLink: next}
	for _, b := range rows {
		var r struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Position   *int64 `json:"position"`
			Visibility string `json:"visibility"`
		}
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if r.ID == "" || r.Name == "" || r.Position == nil || *r.Position < 0 {
			return nil, errors.New("Microsoft Graph returned incomplete worksheet metadata.")
		}
		out.Worksheets = append(out.Worksheets, Worksheet{ID: r.ID, Name: r.Name, Position: *r.Position, Visibility: r.Visibility})
	}
	return out, nil
}
func (c *client) listTables(ctx context.Context, in *ListTablesIn) (*ListTablesOut, error) {
	p, e := workbookPath(in.DriveID, in.WorkbookID)
	if e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, p+"/tables", in.NextLink, in.SessionID, nil)
	if e != nil {
		return nil, e
	}
	out := &ListTablesOut{Tables: []Table{}, NextLink: next}
	for _, b := range rows {
		var r struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			ShowHeaders bool   `json:"showHeaders"`
		}
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if r.ID == "" || r.Name == "" {
			return nil, errors.New("Microsoft Graph returned incomplete table metadata.")
		}
		out.Tables = append(out.Tables, Table{ID: r.ID, Name: r.Name, ShowHeaders: r.ShowHeaders})
	}
	return out, nil
}

var a1 = regexp.MustCompile(`^\$?([A-Z]{1,3})\$?([1-9][0-9]{0,6})(?::\$?([A-Z]{1,3})\$?([1-9][0-9]{0,6}))?$`)

func columnNumber(s string) int {
	n := 0
	for _, c := range s {
		n = n*26 + int(c-'A'+1)
	}
	return n
}
func rangeShape(address string) (string, int, int, error) {
	s := strings.ToUpper(address)
	m := a1.FindStringSubmatch(s)
	if m == nil {
		return "", 0, 0, errors.New("Provide a bounded A1 cell or rectangular range without a sheet prefix.")
	}
	c1 := columnNumber(m[1])
	r1, _ := strconv.Atoi(m[2])
	c2, r2 := c1, r1
	if m[3] != "" {
		c2 = columnNumber(m[3])
		r2, _ = strconv.Atoi(m[4])
	}
	rows, cols := r2-r1+1, c2-c1+1
	if c2 > 16384 || r2 > 1048576 || rows < 1 || cols < 1 || rows > 1000 || cols > 100 || rows*cols > 10000 {
		return "", 0, 0, errors.New("Range must contain at most 1000 rows, 100 columns and 10000 cells within Excel limits.")
	}
	return s, rows, cols, nil
}
func rangePath(drive, book, worksheet, address string) (string, int, int, error) {
	p, e := workbookPath(drive, book)
	if e != nil {
		return "", 0, 0, e
	}
	w, e := textSegment(worksheet)
	if e != nil {
		return "", 0, 0, e
	}
	a, rows, cols, e := rangeShape(address)
	if e != nil {
		return "", 0, 0, e
	}
	return p + "/worksheets/" + w + "/range(address='" + url.PathEscape(a) + "')", rows, cols, nil
}
func matrixShape(values []any) (int, int, error) {
	if len(values) == 0 || len(values) > 1000 {
		return 0, 0, errors.New("Provide 1 to 1000 rows of cell values.")
	}
	cols := 0
	for i, row := range values {
		cells, ok := row.([]any)
		if !ok || len(cells) == 0 || len(cells) > 100 {
			return 0, 0, errors.New("Each row must be an array of 1 to 100 cells.")
		}
		if i == 0 {
			cols = len(cells)
		}
		if len(cells) != cols {
			return 0, 0, errors.New("Cell rows must have the same number of columns.")
		}
		for _, v := range cells {
			switch v := v.(type) {
			case nil, bool, int, int32, int64, uint, uint32, uint64:
			case string:
				if len(v) > 32767 {
					return 0, 0, errors.New("A text cell exceeds the 32767-byte plugin limit.")
				}
			case float64:
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return 0, 0, errors.New("Cell numbers must be finite.")
				}
			case json.Number:
				if n, e := v.Float64(); e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return 0, 0, errors.New("Cell numbers must be finite.")
				}
			default:
				return 0, 0, errors.New("Cells must be strings, numbers, booleans or null.")
			}
		}
	}
	if len(values)*cols > 10000 {
		return 0, 0, errors.New("A request may contain at most 10000 cells.")
	}
	return len(values), cols, nil
}
func parseRange(raw []byte, expectedRows, expectedCols int) (CellRange, error) {
	var r struct {
		Address string `json:"address"`
		Rows    *int64 `json:"rowCount"`
		Cols    *int64 `json:"columnCount"`
		Values  []any  `json:"values"`
	}
	if e := decodeJSON(raw, &r); e != nil {
		return CellRange{}, e
	}
	if r.Address == "" || r.Rows == nil || r.Cols == nil || *r.Rows != int64(expectedRows) || *r.Cols != int64(expectedCols) {
		return CellRange{}, errors.New("Microsoft Graph returned a range with unexpected dimensions.")
	}
	rows, cols, e := matrixShape(r.Values)
	if e != nil || rows != expectedRows || cols != expectedCols {
		return CellRange{}, errors.New("Microsoft Graph returned incomplete range values.")
	}
	return CellRange{Address: r.Address, RowCount: *r.Rows, ColumnCount: *r.Cols, Values: r.Values}, nil
}
func (c *client) rangeCall(ctx context.Context, method, drive, book, worksheet, address, session string, values []any) (CellRange, error) {
	p, rows, cols, e := rangePath(drive, book, worksheet, address)
	if e != nil {
		return CellRange{}, e
	}
	h, e := sessionHeaders(session)
	if e != nil {
		return CellRange{}, e
	}
	var b []byte
	if method == "PATCH" {
		vr, vc, e := matrixShape(values)
		if e != nil {
			return CellRange{}, e
		}
		if vr != rows || vc != cols {
			return CellRange{}, errors.New("The cell matrix must exactly match the requested A1 range dimensions.")
		}
		b, e = json.Marshal(map[string]any{"values": values})
		if e != nil {
			return CellRange{}, errors.New("Invalid cell values.")
		}
		if len(b) > 1<<20 {
			return CellRange{}, errors.New("JSON request exceeds 1 MiB.")
		}
		h.Set("Content-Type", "application/json")
	}
	raw, _, e := c.send(ctx, method, graphOrigin+p, b, h, 200)
	if e != nil {
		return CellRange{}, e
	}
	out, e := parseRange(raw, rows, cols)
	if e != nil && method == "PATCH" {
		return CellRange{}, errors.New("Microsoft Graph returned an invalid range write receipt. Inspect the workbook before retrying.")
	}
	return out, e
}
func (c *client) getRange(ctx context.Context, in *GetRangeIn) (*GetRangeOut, error) {
	r, e := c.rangeCall(ctx, "GET", in.DriveID, in.WorkbookID, in.WorksheetID, in.Address, in.SessionID, nil)
	return &GetRangeOut{Range: r}, e
}
func (c *client) updateRange(ctx context.Context, in *UpdateRangeIn) (*UpdateRangeOut, error) {
	r, e := c.rangeCall(ctx, "PATCH", in.DriveID, in.WorkbookID, in.WorksheetID, in.Address, in.SessionID, in.Values)
	return &UpdateRangeOut{Range: r}, e
}
func tablePath(drive, book, table string) (string, error) {
	p, e := workbookPath(drive, book)
	if e != nil {
		return "", e
	}
	t, e := textSegment(table)
	return p + "/tables/" + t, e
}
func parseRow(b []byte) (TableRow, error) {
	var r struct {
		Index  *int64 `json:"index"`
		Values []any  `json:"values"`
	}
	if e := decodeJSON(b, &r); e != nil {
		return TableRow{}, e
	}
	if r.Index == nil || *r.Index < 0 {
		return TableRow{}, errors.New("Microsoft Graph returned a row without its index.")
	}
	if _, _, e := matrixShape(r.Values); e != nil {
		return TableRow{}, errors.New("Microsoft Graph returned incomplete table row values.")
	}
	return TableRow{Index: *r.Index, Values: r.Values}, nil
}
func (c *client) appendRows(ctx context.Context, in *AppendRowsIn) (*AppendRowsOut, error) {
	p, e := tablePath(in.DriveID, in.WorkbookID, in.TableID)
	if e != nil {
		return nil, e
	}
	inputRows, inputCols, e := matrixShape(in.Values)
	if e != nil {
		return nil, e
	}
	h, e := sessionHeaders(in.SessionID)
	if e != nil {
		return nil, e
	}
	var raw json.RawMessage
	if e = c.json(ctx, "POST", graphOrigin+p+"/rows/add", map[string]any{"values": in.Values}, h, &raw, 200, 201); e != nil {
		return nil, e
	}
	row, e := parseRow(raw)
	if e != nil {
		return nil, errors.New("Microsoft Graph returned an invalid append receipt. Inspect the table before retrying.")
	}
	outputRows, outputCols, e := matrixShape(row.Values)
	if e != nil || outputRows != inputRows || outputCols != inputCols {
		return nil, errors.New("Microsoft Graph returned a truncated or mismatched append receipt. Inspect the table before retrying.")
	}
	return &AppendRowsOut{Row: row}, nil
}
func (c *client) listRows(ctx context.Context, in *ListRowsIn) (*ListRowsOut, error) {
	p, e := tablePath(in.DriveID, in.WorkbookID, in.TableID)
	if e != nil {
		return nil, e
	}
	skip := int64(0)
	if in.Skip != "" {
		if strings.HasPrefix(in.Skip, "+") || strings.HasPrefix(in.Skip, "-") {
			return nil, errors.New("Row offset must be a nonnegative integer string.")
		}
		skip, e = strconv.ParseInt(in.Skip, 10, 64)
		if e != nil || skip < 0 || skip > 1000000 {
			return nil, errors.New("Row offset must be between 0 and 1000000.")
		}
	}
	rows, next, e := c.collection(ctx, p+"/rows", "", in.SessionID, url.Values{"$top": {"100"}, "$skip": {strconv.FormatInt(skip, 10)}})
	if e != nil {
		return nil, e
	}
	if len(rows) > 100 {
		return nil, errors.New("Microsoft Graph returned more table rows than requested.")
	}
	if len(rows) < 100 && next != "" {
		return nil, errors.New("Microsoft Graph returned a short row page with a server continuation. This operation supports offset paging only; completion cannot be determined safely.")
	}
	out := &ListRowsOut{Rows: []TableRow{}, HasMore: len(rows) == 100}
	for i, b := range rows {
		row, e := parseRow(b)
		if e != nil {
			return nil, e
		}
		if row.Index != skip+int64(i) {
			return nil, errors.New("Microsoft Graph returned unexpected row indexes. Restart pagination after concurrent changes.")
		}
		out.Rows = append(out.Rows, row)
	}
	if out.HasMore {
		out.NextSkip = strconv.FormatInt(skip+int64(len(rows)), 10)
	}
	return out, nil
}
