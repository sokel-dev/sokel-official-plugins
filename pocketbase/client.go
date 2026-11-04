package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/pocketbase/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,127}$`)

func collectionPath(collection string) (string, error) {
	if !namePattern.MatchString(collection) || strings.HasPrefix(collection, "_") {
		return "", errors.New("Use a regular collection ID or name containing letters, digits, underscores or hyphens; system collections are excluded.")
	}
	return "/api/collections/" + collection + "/records", nil
}
func recordPath(collection, id string) (string, error) {
	p, e := collectionPath(collection)
	if e != nil {
		return "", e
	}
	if !namePattern.MatchString(id) {
		return "", errors.New("Invalid record ID.")
	}
	return p + "/" + id, nil
}
func boundedText(v string, limit int) bool {
	return utf8.ValidString(v) && len(v) <= limit && !strings.ContainsAny(v, "\x00\r\n")
}
func queryOptions(o schema.ListOptions) (url.Values, int, int, error) {
	page, size := 1, 30
	if o.Page != nil {
		page = *o.Page
	}
	if o.PerPage != nil {
		size = *o.PerPage
	}
	if page < 1 || page > 1000000 || size < 1 || size > 200 {
		return nil, 0, 0, errors.New("Page must be 1–1000000 and records per page must be 1–200.")
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "perPage": {strconv.Itoa(size)}, "skipTotal": {"false"}}
	for k, v := range map[string]*string{"filter": o.Filter, "sort": o.Sort, "expand": o.Expand} {
		if v != nil {
			if !boundedText(*v, 8192) {
				return nil, 0, 0, errors.New("Filter, sort and expansion values must be valid single-line text of at most 8192 bytes.")
			}
			if *v != "" {
				q.Set(k, *v)
			}
		}
	}
	return q, page, size, nil
}
func recordValue(raw json.RawMessage, collection, id string) (schema.Record, error) {
	var out schema.Record
	if e := requireKeys(raw, "id", "collectionId", "collectionName"); e != nil {
		return out, e
	}
	var v struct {
		ID             string `json:"id"`
		CollectionID   string `json:"collectionId"`
		CollectionName string `json:"collectionName"`
	}
	if e := decodeJSON(raw, &v); e != nil {
		return out, e
	}
	if !namePattern.MatchString(v.ID) || !namePattern.MatchString(v.CollectionID) || !namePattern.MatchString(v.CollectionName) || (id != "" && id != v.ID) || (collection != v.CollectionID && collection != v.CollectionName) {
		return out, errors.New("PocketBase returned mismatched record metadata.")
	}
	return schema.Record{ID: v.ID, CollectionID: v.CollectionID, CollectionName: v.CollectionName, JSON: string(raw)}, nil
}
func writeBody(raw string, update bool) (json.RawMessage, error) {
	if len(raw) > maxRequest || !utf8.ValidString(raw) {
		return nil, errors.New("Record JSON must be UTF-8 and at most 1 MiB.")
	}
	var obj map[string]json.RawMessage
	if e := decodeJSON([]byte(raw), &obj); e != nil || obj == nil {
		return nil, errors.New("Field values must be a JSON object string.")
	}
	if update && len(obj) == 0 {
		return nil, errors.New("Provide at least one field to update.")
	}
	for _, key := range []string{"collectionId", "collectionName", "expand"} {
		if _, ok := obj[key]; ok {
			return nil, errors.New("Record JSON must not set collection metadata or expanded records.")
		}
	}
	if id, ok := obj["id"]; ok {
		var s string
		if update || json.Unmarshal(id, &s) != nil || !namePattern.MatchString(s) {
			return nil, errors.New("An ID can only be specified as a valid string when creating a record.")
		}
	}
	return json.RawMessage(raw), nil
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	b, e := c.request(ctx, "GET", "/api/health", nil, nil, 200)
	if e == nil {
		var v struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		}
		e = decodeJSON(b, &v)
		if e == nil && (v.Status != 200 || v.Message == "") {
			e = errors.New("PocketBase returned invalid health metadata.")
		}
	}
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "PocketBase is reachable. This public health endpoint does not verify the token or collection permissions."}, nil
}
func (c *client) list_records(ctx context.Context, in *ListRecordsIn) (*ListRecordsOut, error) {
	p, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	q, page, size, e := queryOptions(in.Options)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", p, q, nil, 200)
	if e != nil {
		return nil, e
	}
	if e = requireKeys(b, "page", "perPage", "totalItems", "totalPages", "items"); e != nil {
		return nil, e
	}
	var v struct {
		Page       int               `json:"page"`
		PerPage    int               `json:"perPage"`
		Total      json.Number       `json:"totalItems"`
		TotalPages json.Number       `json:"totalPages"`
		Items      []json.RawMessage `json:"items"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	total, e1 := strconv.ParseInt(v.Total.String(), 10, 64)
	pages, e2 := strconv.ParseInt(v.TotalPages.String(), 10, 64)
	if e1 != nil || e2 != nil || total < 0 || pages < 0 || v.Page != page || v.PerPage != size || len(v.Items) > size || int64(len(v.Items)) > total {
		return nil, errors.New("PocketBase returned invalid pagination metadata.")
	}
	expected := total / int64(size)
	if total%int64(size) != 0 {
		expected++
	}
	if pages != expected {
		return nil, errors.New("PocketBase returned inconsistent pagination totals.")
	}
	more := int64(page) < pages
	if more && len(v.Items) == 0 {
		return nil, errors.New("PocketBase returned an empty page before the final page.")
	}
	out := &ListRecordsOut{Total: v.Total.String(), HasMore: more, Records: []schema.Record{}}
	if more {
		if page == 1000000 {
			return nil, errors.New("Pagination exceeds the supported page limit; narrow the filter.")
		}
		out.NextPage = page + 1
	}
	seen := map[string]bool{}
	for _, raw := range v.Items {
		rec, e := recordValue(raw, in.Collection, "")
		if e != nil {
			return nil, e
		}
		if seen[rec.ID] {
			return nil, errors.New("PocketBase returned duplicate record IDs in one page.")
		}
		seen[rec.ID] = true
		out.Records = append(out.Records, rec)
	}
	return out, nil
}
func (c *client) find_record(ctx context.Context, in *FindRecordIn) (*FindRecordOut, error) {
	if strings.TrimSpace(in.Filter) == "" {
		return nil, errors.New("Provide a filter expression for the record lookup.")
	}
	one := 1
	list, e := c.list_records(ctx, &ListRecordsIn{Collection: in.Collection, Options: schema.ListOptions{PerPage: &one, Filter: &in.Filter, Sort: &in.Sort, Expand: &in.Expand}})
	if e != nil {
		return nil, e
	}
	return &FindRecordOut{Found: len(list.Records) > 0, Records: list.Records}, nil
}
func (c *client) get_record(ctx context.Context, in *GetRecordIn) (*GetRecordOut, error) {
	p, e := recordPath(in.Collection, in.RecordID)
	if e != nil {
		return nil, e
	}
	if !boundedText(in.Expand, 8192) {
		return nil, errors.New("Invalid relation expansion value.")
	}
	q := url.Values{}
	if in.Expand != "" {
		q.Set("expand", in.Expand)
	}
	b, e := c.request(ctx, "GET", p, q, nil, 200)
	if e != nil {
		return nil, e
	}
	rec, e := recordValue(b, in.Collection, in.RecordID)
	if e != nil {
		return nil, e
	}
	return &GetRecordOut{Record: rec}, nil
}
func (c *client) create_record(ctx context.Context, in *CreateRecordIn) (*CreateRecordOut, error) {
	p, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	raw, e := writeBody(in.DataJSON, false)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "POST", p, nil, raw, 200)
	if e != nil {
		return nil, e
	}
	rec, e := recordValue(b, in.Collection, "")
	if e != nil {
		return nil, e
	}
	return &CreateRecordOut{Record: rec}, nil
}
func (c *client) update_record(ctx context.Context, in *UpdateRecordIn) (*UpdateRecordOut, error) {
	p, e := recordPath(in.Collection, in.RecordID)
	if e != nil {
		return nil, e
	}
	raw, e := writeBody(in.DataJSON, true)
	if e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "PATCH", p, nil, raw, 200)
	if e != nil {
		return nil, e
	}
	rec, e := recordValue(b, in.Collection, in.RecordID)
	if e != nil {
		return nil, e
	}
	return &UpdateRecordOut{Record: rec}, nil
}
func (c *client) delete_record(ctx context.Context, in *DeleteRecordIn) (*DeleteRecordOut, error) {
	p, e := recordPath(in.Collection, in.RecordID)
	if e != nil {
		return nil, e
	}
	_, e = c.request(ctx, "DELETE", p, nil, nil, 204)
	if e != nil {
		return nil, e
	}
	return &DeleteRecordOut{Deleted: true}, nil
}
