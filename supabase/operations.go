package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func tablePath(table string) (string, error) {
	if !identifier.MatchString(table) {
		return "", fmt.Errorf("table must be a simple table or view name")
	}
	return "/rest/v1/" + table, nil
}
func schemaName(s string) string {
	if s == "" {
		return "public"
	}
	return s
}
func page(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 || offset < 0 || offset > 1000000000 {
		return 0, fmt.Errorf("limit must be 1 to 1000 and offset must be 0 to 1000000000")
	}
	return limit, nil
}
func filterQuery(filters []RowFilter, required bool) (url.Values, error) {
	q := url.Values{}
	if required && len(filters) == 0 {
		return nil, fmt.Errorf("at least one filter is required for update or delete")
	}
	if len(filters) > 100 {
		return nil, fmt.Errorf("at most 100 filters are allowed")
	}
	reserved := map[string]bool{"select": true, "order": true, "limit": true, "offset": true, "and": true, "or": true, "not": true, "columns": true, "on_conflict": true}
	allowed := map[string]bool{"eq": true, "neq": true, "gt": true, "gte": true, "lt": true, "lte": true, "like": true, "ilike": true, "in": true, "is": true, "cs": true, "cd": true, "ov": true}
	for _, f := range filters {
		if !identifier.MatchString(f.Column) || reserved[f.Column] {
			return nil, fmt.Errorf("filter column must be a simple, non-reserved column name")
		}
		if !allowed[f.Operator] {
			return nil, fmt.Errorf("unsupported filter operator")
		}
		q.Add(f.Column, f.Operator+"."+f.Value)
	}
	return q, nil
}
func parseRows(raw []byte) ([]any, error) {
	var rows []any
	if e := decode(raw, &rows); e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, fmt.Errorf("Supabase did not return a row array")
	}
	for _, row := range rows {
		if _, ok := row.(map[string]any); !ok {
			return nil, fmt.Errorf("Supabase returned an invalid row")
		}
	}
	return rows, nil
}
func (a *api) selectRows(ctx context.Context, in *SelectRowsIn) (*SelectRowsOut, error) {
	p, e := tablePath(in.Table)
	if e != nil {
		return nil, e
	}
	limit, e := page(in.Limit, in.Offset)
	if e != nil {
		return nil, e
	}
	q, e := filterQuery(in.Filters, false)
	if e != nil {
		return nil, e
	}
	sel := in.Select
	if sel == "" {
		sel = "*"
	}
	q.Set("select", sel)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(in.Offset))
	if in.Order != "" {
		q.Set("order", in.Order)
	}
	raw, h, e := a.request(ctx, "GET", p, schemaName(in.Schema), q, nil, "count=exact")
	if e != nil {
		return nil, e
	}
	rows, e := parseRows(raw)
	if e != nil {
		return nil, e
	}
	return &SelectRowsOut{Rows: rows, Count: len(rows), NextOffset: in.Offset + len(rows), ContentRange: h.Get("Content-Range")}, nil
}
func validateRows(rows []any) error {
	if len(rows) < 1 || len(rows) > 1000 {
		return fmt.Errorf("rows must contain 1 to 1000 objects")
	}
	for _, row := range rows {
		v, ok := row.(map[string]any)
		if !ok || len(v) == 0 {
			return fmt.Errorf("each row must be a nonempty JSON object")
		}
	}
	return nil
}
func (a *api) writeRows(ctx context.Context, method, table, schema string, q url.Values, body any, prefer string) ([]any, error) {
	p, e := tablePath(table)
	if e != nil {
		return nil, e
	}
	raw, _, e := a.request(ctx, method, p, schemaName(schema), q, body, prefer)
	if e != nil {
		return nil, e
	}
	return parseRows(raw)
}
func (a *api) insertRows(ctx context.Context, in *InsertRowsIn) (*InsertRowsOut, error) {
	if e := validateRows(in.Rows); e != nil {
		return nil, e
	}
	rows, e := a.writeRows(ctx, "POST", in.Table, in.Schema, nil, in.Rows, "return=representation")
	if e != nil {
		return nil, e
	}
	return &InsertRowsOut{Rows: rows, Count: len(rows)}, nil
}
func (a *api) upsertRows(ctx context.Context, in *UpsertRowsIn) (*UpsertRowsOut, error) {
	if e := validateRows(in.Rows); e != nil {
		return nil, e
	}
	q := url.Values{}
	if in.OnConflict != "" {
		for _, c := range strings.Split(in.OnConflict, ",") {
			if !identifier.MatchString(c) {
				return nil, fmt.Errorf("on_conflict must contain comma-separated simple column names")
			}
		}
		q.Set("on_conflict", in.OnConflict)
	}
	rows, e := a.writeRows(ctx, "POST", in.Table, in.Schema, q, in.Rows, "return=representation,resolution=merge-duplicates")
	if e != nil {
		return nil, e
	}
	return &UpsertRowsOut{Rows: rows, Count: len(rows)}, nil
}
func (a *api) updateRows(ctx context.Context, in *UpdateRowsIn) (*UpdateRowsOut, error) {
	q, e := filterQuery(in.Filters, true)
	if e != nil {
		return nil, e
	}
	if len(in.Values) == 0 {
		return nil, fmt.Errorf("values must be a nonempty JSON object")
	}
	rows, e := a.writeRows(ctx, "PATCH", in.Table, in.Schema, q, in.Values, "return=representation")
	if e != nil {
		return nil, e
	}
	return &UpdateRowsOut{Rows: rows, Count: len(rows)}, nil
}
func (a *api) deleteRows(ctx context.Context, in *DeleteRowsIn) (*DeleteRowsOut, error) {
	q, e := filterQuery(in.Filters, true)
	if e != nil {
		return nil, e
	}
	rows, e := a.writeRows(ctx, "DELETE", in.Table, in.Schema, q, nil, "return=representation")
	if e != nil {
		return nil, e
	}
	return &DeleteRowsOut{Rows: rows, Count: len(rows)}, nil
}
func (a *api) callFunction(ctx context.Context, in *CallFunctionIn) (*CallFunctionOut, error) {
	if !identifier.MatchString(in.Function) {
		return nil, fmt.Errorf("function must be a simple database function name")
	}
	args := in.Arguments
	if args == nil {
		args = map[string]any{}
	}
	raw, _, e := a.request(ctx, "POST", "/rest/v1/rpc/"+in.Function, schemaName(in.Schema), nil, args, "")
	if e != nil {
		return nil, e
	}
	if len(raw) == 0 {
		raw = []byte("null")
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("Supabase returned malformed function result JSON")
	}
	return &CallFunctionOut{ResultJSON: string(raw)}, nil
}
func objectPath(s string, empty bool) (string, error) {
	if s == "" && empty {
		return "", nil
	}
	if s == "" || strings.ContainsAny(s, "\\\r\n\x00") {
		return "", fmt.Errorf("storage path must be relative and valid")
	}
	parts := strings.Split(s, "/")
	for i, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", fmt.Errorf("storage path must not contain empty or traversal segments")
		}
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/"), nil
}
func bucketPath(s string) (string, error) {
	if strings.Contains(s, "/") {
		return "", fmt.Errorf("bucket must be a single name")
	}
	return objectPath(s, false)
}
func (a *api) listObjects(ctx context.Context, in *ListObjectsIn) (*ListObjectsOut, error) {
	bucket, e := bucketPath(in.Bucket)
	if e != nil {
		return nil, e
	}
	if _, e = objectPath(in.Prefix, true); e != nil {
		return nil, e
	}
	limit, e := page(in.Limit, in.Offset)
	if e != nil {
		return nil, e
	}
	raw, _, e := a.request(ctx, "POST", "/storage/v1/object/list/"+bucket, "", nil, map[string]any{"prefix": in.Prefix, "limit": limit, "offset": in.Offset, "sortBy": map[string]string{"column": "name", "order": "asc"}}, "")
	if e != nil {
		return nil, e
	}
	var objects []StorageObject
	if e = decode(raw, &objects); e != nil {
		return nil, e
	}
	if objects == nil {
		return nil, fmt.Errorf("Supabase did not return a storage object array")
	}
	return &ListObjectsOut{Objects: objects, NextOffset: in.Offset + len(objects)}, nil
}
func (a *api) signedURL(ctx context.Context, in *CreateSignedURLIn) (*CreateSignedURLOut, error) {
	bucket, e := bucketPath(in.Bucket)
	if e != nil {
		return nil, e
	}
	p, e := objectPath(in.Path, false)
	if e != nil {
		return nil, e
	}
	ttl := in.ExpiresIn
	if ttl == 0 {
		ttl = 3600
	}
	if ttl < 1 || ttl > 604800 {
		return nil, fmt.Errorf("expires_in must be 1 to 604800 seconds")
	}
	raw, _, e := a.request(ctx, "POST", "/storage/v1/object/sign/"+bucket+"/"+p, "", nil, map[string]int{"expiresIn": ttl}, "")
	if e != nil {
		return nil, e
	}
	var r struct {
		URL string `json:"signedURL"`
	}
	if e = decode(raw, &r); e != nil {
		return nil, e
	}
	u, e := url.Parse(r.URL)
	if e != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/object/sign/") || u.Fragment != "" || u.Query().Get("token") == "" {
		return nil, fmt.Errorf("Supabase returned an unexpected signed URL")
	}
	base, e := origin(a.base)
	if e != nil {
		return nil, e
	}
	return &CreateSignedURLOut{SignedURL: base + "/storage/v1" + u.String(), ExpiresIn: ttl}, nil
}
func (a *api) health(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	if in.Table == "" {
		return &HealthCheckOut{Message: "Provide table to verify credentials and read access with a read-only request"}, nil
	}
	p, e := tablePath(in.Table)
	if e == nil {
		_, _, e = a.request(ctx, "HEAD", p, schemaName(in.Schema), url.Values{"select": {"*"}, "limit": {"1"}}, nil, "")
	}
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Supabase accepted the credential for this table; row visibility is governed by grants and RLS"}, nil
}
