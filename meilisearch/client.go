package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/meilisearch/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var resourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)
var taskID = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var errorCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func indexPath(id string) (string, error) {
	if !resourceID.MatchString(id) {
		return "", errors.New("Index UID must contain 1..512 ASCII letters, digits, hyphens or underscores.")
	}
	return "/indexes/" + id, nil
}
func documentPath(index, id string) (string, error) {
	p, e := indexPath(index)
	if e != nil {
		return "", e
	}
	if !resourceID.MatchString(id) {
		return "", errors.New("Document ID must contain 1..512 ASCII letters, digits, hyphens or underscores.")
	}
	return p + "/documents/" + id, nil
}
func numericID(s string) error {
	if !taskID.MatchString(s) {
		return errors.New("Task UID must be a canonical nonnegative decimal string.")
	}
	if _, e := strconv.ParseUint(s, 10, 64); e != nil {
		return errors.New("Task UID exceeds uint64.")
	}
	return nil
}
func textValid(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func primaryKey(s string) error {
	if s != "" && (!textValid(s, 512) || strings.TrimSpace(s) == "") {
		return errors.New("Primary key must be nonblank UTF-8 and at most 512 bytes.")
	}
	return nil
}
func timestamp(s string) bool { _, e := time.Parse(time.RFC3339Nano, s); return e == nil }
func (c *client) call(ctx context.Context, method, path string, q url.Values, body any, status int) ([]byte, error) {
	b, e := c.request(ctx, method, path, q, body, status)
	if e != nil {
		return nil, e
	}
	var obj map[string]json.RawMessage
	if decodeJSON(b, &obj) != nil || obj == nil {
		return nil, errors.New("Meilisearch returned no JSON object.")
	}
	return b, nil
}
func pageOptions(limit, offset *int) (int, int, error) {
	l, o := 20, 0
	if limit != nil {
		l = *limit
	}
	if offset != nil {
		o = *offset
	}
	if l < 1 || l > 1000 || o < 0 || o > 1000000 {
		return 0, 0, errors.New("Page size must be 1..1000 and offset 0..1000000.")
	}
	return l, o, nil
}

type indexWire struct {
	UID        string  `json:"uid"`
	PrimaryKey *string `json:"primaryKey"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

func (w indexWire) value() (schema.Index, error) {
	if _, e := indexPath(w.UID); e != nil || !timestamp(w.CreatedAt) || !timestamp(w.UpdatedAt) {
		return schema.Index{}, errors.New("Meilisearch returned incomplete index metadata.")
	}
	return schema.Index{UID: w.UID, PrimaryKey: w.PrimaryKey, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}, nil
}
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	one := 1
	_, e := c.list_indexes(ctx, &ListIndexesIn{Page: schema.Page{Limit: &one}})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Meilisearch index-list access verified; search, task and write permissions may differ."}, nil
}
func (c *client) list_indexes(ctx context.Context, in *ListIndexesIn) (*ListIndexesOut, error) {
	limit, offset, e := pageOptions(in.Page.Limit, in.Page.Offset)
	if e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "GET", "/indexes", url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Results []indexWire `json:"results"`
		Limit   *int        `json:"limit"`
		Offset  *int        `json:"offset"`
		Total   *int        `json:"total"`
	}
	if decodeJSON(b, &v) != nil || v.Results == nil || v.Limit == nil || *v.Limit != limit || v.Offset == nil || *v.Offset != offset || v.Total == nil || *v.Total < 0 || len(v.Results) > limit {
		return nil, errors.New("Meilisearch returned invalid index pagination metadata.")
	}
	out := &ListIndexesOut{Indexes: []schema.Index{}, Total: *v.Total}
	seen := map[string]bool{}
	for _, w := range v.Results {
		v, e := w.value()
		if e != nil {
			return nil, e
		}
		if seen[v.UID] {
			return nil, errors.New("Meilisearch returned duplicate indexes.")
		}
		seen[v.UID] = true
		out.Indexes = append(out.Indexes, v)
	}
	end := offset + len(v.Results)
	if len(v.Results) > 0 && end > *v.Total {
		return nil, errors.New("Meilisearch returned an inconsistent index total.")
	}
	if end < *v.Total {
		if len(v.Results) == 0 || end > 1000000 {
			return nil, errors.New("Index pagination cannot advance within the plugin limit.")
		}
		out.HasMore = true
		out.NextOffset = end
	}
	return out, nil
}
func (c *client) get_index(ctx context.Context, in *GetIndexIn) (*GetIndexOut, error) {
	path, e := indexPath(in.IndexUID)
	if e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "GET", path, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var w indexWire
	if e = decodeJSON(b, &w); e != nil {
		return nil, e
	}
	v, e := w.value()
	if e != nil {
		return nil, e
	}
	if v.UID != in.IndexUID {
		return nil, errors.New("Meilisearch returned a different index.")
	}
	return &GetIndexOut{Index: v}, nil
}
func taskReceipt(b []byte, index, kind string) (schema.TaskReceipt, error) {
	var v struct {
		UID        json.Number `json:"taskUid"`
		IndexUID   *string     `json:"indexUid"`
		Status     string      `json:"status"`
		Type       string      `json:"type"`
		EnqueuedAt string      `json:"enqueuedAt"`
	}
	if decodeJSON(b, &v) != nil || numericID(string(v.UID)) != nil || v.Status != "enqueued" || v.Type != kind || !timestamp(v.EnqueuedAt) || (v.IndexUID != nil && *v.IndexUID != index) {
		return schema.TaskReceipt{}, errors.New("Meilisearch returned an invalid task submission receipt. Inspect tasks before resubmitting.")
	}
	return schema.TaskReceipt{UID: string(v.UID), IndexUID: v.IndexUID, Status: v.Status, Type: v.Type, EnqueuedAt: v.EnqueuedAt}, nil
}
func (c *client) create_index(ctx context.Context, in *CreateIndexIn) (*CreateIndexOut, error) {
	if _, e := indexPath(in.IndexUID); e != nil {
		return nil, e
	}
	if e := primaryKey(in.PrimaryKey); e != nil {
		return nil, e
	}
	body := map[string]any{"uid": in.IndexUID}
	if in.PrimaryKey != "" {
		body["primaryKey"] = in.PrimaryKey
	}
	b, e := c.call(ctx, "POST", "/indexes", nil, body, 202)
	if e != nil {
		return nil, e
	}
	v, e := taskReceipt(b, in.IndexUID, "indexCreation")
	if e != nil {
		return nil, e
	}
	return &CreateIndexOut{Task: v}, nil
}
func documentJSON(b []byte) (string, error) {
	var obj map[string]json.RawMessage
	if decodeJSON(b, &obj) != nil || obj == nil || len(obj) == 0 {
		return "", errors.New("Document must be a nonempty JSON object.")
	}
	return string(b), nil
}
func (c *client) search(ctx context.Context, in *SearchIn) (*SearchOut, error) {
	path, e := indexPath(in.IndexUID)
	if e != nil {
		return nil, e
	}
	if !textValid(in.Query, 16384) {
		return nil, errors.New("Search query must be valid UTF-8 and at most 16 KiB.")
	}
	limit, offset, e := pageOptions(in.Options.Limit, in.Options.Offset)
	if e != nil {
		return nil, e
	}
	body := map[string]any{"q": in.Query, "limit": limit, "offset": offset}
	if in.Options.Filter != nil {
		if !textValid(*in.Options.Filter, 16384) {
			return nil, errors.New("Filter must be valid UTF-8 and at most 16 KiB.")
		}
		body["filter"] = *in.Options.Filter
	}
	for k, values := range map[string][]string{"sort": in.Options.Sort, "attributesToRetrieve": in.Options.Attributes} {
		if values != nil {
			if len(values) > 100 {
				return nil, errors.New("At most 100 sort or attribute values are accepted.")
			}
			for _, v := range values {
				if !textValid(v, 512) || strings.TrimSpace(v) == "" {
					return nil, errors.New("Sort and attribute values must be nonblank UTF-8 and at most 512 bytes.")
				}
			}
			body[k] = values
		}
	}
	b, e := c.call(ctx, "POST", path+"/search", nil, body, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Hits   []json.RawMessage `json:"hits"`
		Offset *int              `json:"offset"`
		Limit  *int              `json:"limit"`
		Total  *int              `json:"estimatedTotalHits"`
		Time   *int              `json:"processingTimeMs"`
	}
	if decodeJSON(b, &v) != nil || v.Hits == nil || len(v.Hits) > limit || v.Offset == nil || *v.Offset != offset || v.Limit == nil || *v.Limit != limit || v.Total == nil || *v.Total < 0 || v.Time == nil || *v.Time < 0 {
		return nil, errors.New("Meilisearch returned incomplete search metadata.")
	}
	out := &SearchOut{Hits: []schema.Document{}, EstimatedTotalHits: *v.Total, ProcessingTimeMs: *v.Time}
	for _, raw := range v.Hits {
		var obj map[string]json.RawMessage
		if decodeJSON(raw, &obj) != nil || obj == nil {
			return nil, errors.New("Meilisearch returned a non-object hit.")
		}
		out.Hits = append(out.Hits, schema.Document{JSON: string(raw)})
	}
	if len(v.Hits) == limit {
		if offset+limit > 1000000 {
			return nil, errors.New("Search pagination exceeds the plugin offset limit.")
		}
		out.NextOffset = offset + limit
		out.MayHaveMore = true
	}
	return out, nil
}
func (c *client) get_document(ctx context.Context, in *GetDocumentIn) (*GetDocumentOut, error) {
	path, e := documentPath(in.IndexUID, in.DocumentID)
	if e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "GET", path, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	s, e := documentJSON(b)
	if e != nil {
		return nil, e
	}
	return &GetDocumentOut{DocumentJSON: s}, nil
}
func (c *client) add_documents(ctx context.Context, in *AddDocumentsIn) (*AddDocumentsOut, error) {
	path, e := indexPath(in.IndexUID)
	if e != nil {
		return nil, e
	}
	if e = primaryKey(in.PrimaryKey); e != nil {
		return nil, e
	}
	if len(in.DocumentsJSON) > maxRequest || !utf8.ValidString(in.DocumentsJSON) {
		return nil, errors.New("Document JSON must be UTF-8 and at most 1 MiB.")
	}
	var rows []json.RawMessage
	if decodeJSON([]byte(in.DocumentsJSON), &rows) != nil || len(rows) < 1 || len(rows) > 1000 {
		return nil, errors.New("Provide a JSON array of 1..1000 document objects.")
	}
	for _, r := range rows {
		if _, e = documentJSON(r); e != nil {
			return nil, e
		}
	}
	q := url.Values{}
	if in.PrimaryKey != "" {
		q.Set("primaryKey", in.PrimaryKey)
	}
	b, e := c.call(ctx, "POST", path+"/documents", q, rows, 202)
	if e != nil {
		return nil, e
	}
	v, e := taskReceipt(b, in.IndexUID, "documentAdditionOrUpdate")
	if e != nil {
		return nil, e
	}
	return &AddDocumentsOut{Task: v}, nil
}
func (c *client) delete_document(ctx context.Context, in *DeleteDocumentIn) (*DeleteDocumentOut, error) {
	path, e := documentPath(in.IndexUID, in.DocumentID)
	if e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "DELETE", path, nil, nil, 202)
	if e != nil {
		return nil, e
	}
	v, e := taskReceipt(b, in.IndexUID, "documentDeletion")
	if e != nil {
		return nil, e
	}
	return &DeleteDocumentOut{Task: v}, nil
}
func (c *client) get_task(ctx context.Context, in *GetTaskIn) (*GetTaskOut, error) {
	if e := numericID(in.TaskUID); e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "GET", "/tasks/"+in.TaskUID, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		UID        json.Number `json:"uid"`
		IndexUID   *string     `json:"indexUid"`
		Status     string      `json:"status"`
		Type       string      `json:"type"`
		EnqueuedAt string      `json:"enqueuedAt"`
		StartedAt  *string     `json:"startedAt"`
		FinishedAt *string     `json:"finishedAt"`
		Error      *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if decodeJSON(b, &v) != nil || numericID(string(v.UID)) != nil || string(v.UID) != in.TaskUID || v.Type == "" || !timestamp(v.EnqueuedAt) || (v.StartedAt != nil && !timestamp(*v.StartedAt)) || (v.FinishedAt != nil && !timestamp(*v.FinishedAt)) {
		return nil, errors.New("Meilisearch returned incomplete or mismatched task metadata.")
	}
	finished := false
	switch v.Status {
	case "enqueued", "processing":
	case "succeeded", "failed", "canceled":
		finished = true
	default:
		return nil, errors.New("Meilisearch returned an unknown task status.")
	}
	if finished && v.FinishedAt == nil {
		return nil, errors.New("Meilisearch returned a terminal task without a finish timestamp.")
	}
	if !finished && v.FinishedAt != nil {
		return nil, errors.New("Meilisearch returned inconsistent task state.")
	}
	if v.Status == "succeeded" && v.Error != nil {
		return nil, errors.New("Meilisearch returned a succeeded task with an error.")
	}
	out := &GetTaskOut{Task: schema.Task{UID: in.TaskUID, IndexUID: v.IndexUID, Status: v.Status, Type: v.Type, EnqueuedAt: v.EnqueuedAt, StartedAt: v.StartedAt, FinishedAt: v.FinishedAt}, Finished: finished, Succeeded: v.Status == "succeeded"}
	if v.Error != nil {
		code := v.Error.Code
		if !errorCode.MatchString(code) {
			return nil, errors.New("Meilisearch returned an invalid task error code.")
		}
		if c.cred.APIKey != "" && strings.Contains(code, c.cred.APIKey) {
			code = "upstream_task_error"
		}
		out.Task.ErrorCode = &code
	}
	if v.Status == "failed" && out.Task.ErrorCode == nil {
		return nil, errors.New("Meilisearch returned a failed task without an error code.")
	}
	return out, nil
}
