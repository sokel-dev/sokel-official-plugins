package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/baserow/schema"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func positiveID(s string) bool {
	n, e := strconv.ParseInt(s, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == s
}
func number(s json.Number, zero bool) (int64, error) {
	n, e := strconv.ParseInt(s.String(), 10, 64)
	if e != nil || n < 0 || (!zero && n == 0) || strconv.FormatInt(n, 10) != s.String() {
		return 0, errors.New("Baserow returned an invalid numeric identifier or count.")
	}
	return n, nil
}
func object(s string, empty bool) (json.RawMessage, error) {
	var v map[string]json.RawMessage
	if len(s) > maxRequest || !utf8.ValidString(s) || decodeJSON([]byte(s), &v) != nil || v == nil || (!empty && len(v) == 0) {
		return nil, errors.New("Provide a valid JSON object within 1 MiB.")
	}
	return json.RawMessage(s), nil
}
func rowValue(b json.RawMessage, want string) (schema.Row, error) {
	var v struct {
		ID json.Number `json:"id"`
	}
	if requireKeys(b, "id") != nil || decodeJSON(b, &v) != nil {
		return schema.Row{}, errors.New("Baserow returned an invalid row.")
	}
	if _, e := number(v.ID, false); e != nil {
		return schema.Row{}, e
	}
	if want != "" && v.ID.String() != want {
		return schema.Row{}, errors.New("Baserow returned a different row ID.")
	}
	return schema.Row{ID: v.ID.String(), JSON: string(b)}, nil
}
func tablePath(id string) (string, error) {
	if !positiveID(id) {
		return "", errors.New("Table ID must be a positive signed 64-bit decimal string.")
	}
	return "/api/database/rows/table/" + id + "/", nil
}
func rowPath(table, id string) (string, error) {
	p, e := tablePath(table)
	if e != nil {
		return "", e
	}
	if !positiveID(id) {
		return "", errors.New("Row ID must be a positive signed 64-bit decimal string.")
	}
	return p + id + "/", nil
}
func nameQuery(names bool) url.Values {
	return url.Values{"user_field_names": {strconv.FormatBool(names)}}
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := c.list_tables(ctx, &ListTablesIn{})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Baserow accepted the database token and returned its accessible tables."}, nil
}
func (c *client) list_tables(ctx context.Context, in *ListTablesIn) (*ListTablesOut, error) {
	b, e := c.request(ctx, "GET", "/api/database/tables/all-tables/", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var raw []struct {
		ID         json.Number `json:"id"`
		Name       string      `json:"name"`
		DatabaseID json.Number `json:"database_id"`
	}
	if decodeJSON(b, &raw) != nil || raw == nil {
		return nil, errors.New("Baserow returned an invalid table list.")
	}
	out := &ListTablesOut{Tables: []schema.Table{}}
	seen := map[string]bool{}
	for _, v := range raw {
		if _, e = number(v.ID, false); e != nil {
			return nil, e
		}
		if _, e = number(v.DatabaseID, false); e != nil {
			return nil, e
		}
		if v.Name == "" || seen[v.ID.String()] {
			return nil, errors.New("Baserow returned invalid or repeated table metadata.")
		}
		seen[v.ID.String()] = true
		out.Tables = append(out.Tables, schema.Table{ID: v.ID.String(), Name: v.Name, DatabaseID: v.DatabaseID.String()})
	}
	return out, nil
}
func (c *client) list_fields(ctx context.Context, in *ListFieldsIn) (*ListFieldsOut, error) {
	if !positiveID(in.TableID) {
		return nil, errors.New("Provide a positive table ID.")
	}
	b, e := c.request(ctx, "GET", "/api/database/fields/table/"+in.TableID+"/", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var rows []json.RawMessage
	if decodeJSON(b, &rows) != nil || rows == nil {
		return nil, errors.New("Baserow returned an invalid field list.")
	}
	out := &ListFieldsOut{Fields: []schema.TableField{}}
	seen := map[string]bool{}
	for _, raw := range rows {
		var v struct {
			ID      json.Number `json:"id"`
			TableID json.Number `json:"table_id"`
			Name    string      `json:"name"`
			Type    string      `json:"type"`
			Primary *bool       `json:"primary"`
		}
		if decodeJSON(raw, &v) != nil {
			return nil, errors.New("Baserow returned invalid field metadata.")
		}
		if _, e = number(v.ID, false); e != nil {
			return nil, e
		}
		if v.TableID.String() != in.TableID || v.Name == "" || v.Type == "" || v.Primary == nil || seen[v.ID.String()] {
			return nil, errors.New("Baserow returned incomplete or repeated field metadata.")
		}
		seen[v.ID.String()] = true
		out.Fields = append(out.Fields, schema.TableField{ID: v.ID.String(), Name: v.Name, Type: v.Type, Primary: *v.Primary, DefinitionJSON: string(raw)})
	}
	return out, nil
}
func (c *client) get_row(ctx context.Context, in *GetRowIn) (*GetRowOut, error) {
	p, e := rowPath(in.TableID, in.RowID)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", p, nameQuery(in.UserFieldNames), nil, 200)
	if e != nil {
		return nil, e
	}
	v, e := rowValue(b, in.RowID)
	if e != nil {
		return nil, e
	}
	return &GetRowOut{Row: v}, nil
}
func writeObject(s string, empty bool) (json.RawMessage, error) {
	b, e := object(s, empty)
	if e != nil {
		return nil, e
	}
	var v map[string]json.RawMessage
	_ = decodeJSON(b, &v)
	if _, ok := v["id"]; ok {
		return nil, errors.New("Row ID is assigned by Baserow; do not include id in field values.")
	}
	if _, ok := v["order"]; ok {
		return nil, errors.New("Row order is managed by Baserow; do not include order in field values.")
	}
	return b, nil
}
func (c *client) create_row(ctx context.Context, in *CreateRowIn) (*CreateRowOut, error) {
	p, e := tablePath(in.TableID)
	if e != nil {
		return nil, e
	}
	data, e := writeObject(in.DataJSON, true)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "POST", p, nameQuery(in.UserFieldNames), data, 200)
	if e != nil {
		return nil, e
	}
	v, e := rowValue(b, "")
	if e != nil {
		return nil, e
	}
	return &CreateRowOut{Row: v}, nil
}
func (c *client) update_row(ctx context.Context, in *UpdateRowIn) (*UpdateRowOut, error) {
	p, e := rowPath(in.TableID, in.RowID)
	if e != nil {
		return nil, e
	}
	data, e := writeObject(in.DataJSON, false)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "PATCH", p, nameQuery(in.UserFieldNames), data, 200)
	if e != nil {
		return nil, e
	}
	v, e := rowValue(b, in.RowID)
	if e != nil {
		return nil, e
	}
	return &UpdateRowOut{Row: v}, nil
}
func (c *client) delete_row(ctx context.Context, in *DeleteRowIn) (*DeleteRowOut, error) {
	p, e := rowPath(in.TableID, in.RowID)
	if e != nil {
		return nil, e
	}
	_, e = c.request(ctx, "DELETE", p, nil, nil, 204)
	if e != nil {
		return nil, e
	}
	return &DeleteRowOut{Deleted: true}, nil
}
func (c *client) list_rows(ctx context.Context, in *ListRowsIn) (*ListRowsOut, error) {
	p, e := tablePath(in.TableID)
	if e != nil {
		return nil, e
	}
	page, size := 1, 50
	if in.Options.Page != nil {
		page = *in.Options.Page
	}
	if in.Options.Size != nil {
		size = *in.Options.Size
	}
	if page < 1 || page > 1000000 || size < 1 || size > 200 {
		return nil, errors.New("Page must be 1–1000000 and size 1–200.")
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(size)}}
	if in.Options.UserFieldNames != nil {
		q.Set("user_field_names", strconv.FormatBool(*in.Options.UserFieldNames))
	}
	for k, v := range map[string]*string{"search": in.Options.Search, "order_by": in.Options.OrderBy} {
		if v != nil {
			if len(*v) > 16384 || !utf8.ValidString(*v) || strings.ContainsRune(*v, 0) {
				return nil, errors.New("Search and sort text must be valid UTF-8 within 16 KiB.")
			}
			q.Set(k, *v)
		}
	}
	if in.Options.FiltersJSON != nil {
		if len(*in.Options.FiltersJSON) > 16384 {
			return nil, errors.New("Filter JSON exceeds 16 KiB.")
		}
		if _, e = object(*in.Options.FiltersJSON, true); e != nil {
			return nil, e
		}
		q.Set("filters", *in.Options.FiltersJSON)
	}
	b, e := c.request(ctx, "GET", p, q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Count   json.Number       `json:"count"`
		Next    json.RawMessage   `json:"next"`
		Results []json.RawMessage `json:"results"`
	}
	if decodeJSON(b, &v) != nil || v.Results == nil || len(v.Next) == 0 || len(v.Results) > size {
		return nil, errors.New("Baserow returned incomplete pagination metadata.")
	}
	count, e := number(v.Count, true)
	if e != nil {
		return nil, e
	}
	out := &ListRowsOut{Rows: []schema.Row{}, Total: v.Count.String()}
	seen := map[string]bool{}
	for _, r := range v.Results {
		row, e := rowValue(r, "")
		if e != nil {
			return nil, e
		}
		if seen[row.ID] {
			return nil, errors.New("Baserow returned a repeated row ID.")
		}
		seen[row.ID] = true
		out.Rows = append(out.Rows, row)
	}
	if count < int64((page-1)*size+len(v.Results)) && len(v.Results) > 0 {
		return nil, errors.New("Baserow returned an inconsistent row count.")
	}
	if string(v.Next) != "null" {
		var next string
		if decodeJSON(v.Next, &next) != nil || len(next) > 32768 || len(v.Results) == 0 || page == 1000000 {
			return nil, errors.New("Baserow returned an invalid continuation.")
		}
		base, _ := c.origin()
		u, e := url.Parse(next)
		expected, _ := url.Parse(base + p)
		if e != nil || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.RawPath != "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("Baserow returned a continuation outside the configured table endpoint.")
		}
		q.Set("page", strconv.Itoa(page+1))
		if u.Query().Encode() != q.Encode() || count <= int64(page*size) {
			return nil, errors.New("Baserow returned a non-advancing or inconsistent continuation.")
		}
		out.NextPage = page + 1
		out.HasMore = true
	} else if count > int64(page*size) {
		return nil, errors.New("Baserow omitted a required continuation.")
	}
	return out, nil
}
