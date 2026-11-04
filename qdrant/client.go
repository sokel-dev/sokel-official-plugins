package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/qdrant/schema"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var collectionName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
var decimalID = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var uuidID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func collectionPath(name string) (string, error) {
	if !collectionName.MatchString(name) {
		return "", errors.New("Collection name must contain 1..255 ASCII letters, digits, hyphens or underscores.")
	}
	return "/collections/" + name, nil
}
func pointID(s string) (any, string, error) {
	if decimalID.MatchString(s) {
		if _, e := strconv.ParseUint(s, 10, 64); e == nil {
			return json.Number(s), s, nil
		}
	}
	if uuidID.MatchString(s) {
		s = strings.ToLower(s)
		return s, s, nil
	}
	return nil, "", errors.New("Point ID must be a canonical uint64 decimal string or a hyphenated UUID.")
}
func readID(b []byte) (string, error) {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if decodeJSON(b, &s) != nil {
			return "", errors.New("Qdrant returned an invalid point ID.")
		}
	} else {
		s = string(b)
	}
	_, s, e := pointID(s)
	return s, e
}
func vectorOK(v []float64) error {
	if len(v) < 1 || len(v) > 65536 {
		return errors.New("Dense vector must contain 1..65536 numbers.")
	}
	for _, n := range v {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return errors.New("Vector components must be finite numbers.")
		}
	}
	return nil
}
func textOK(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}
func namedVector(s string) error {
	if s != "" && !textOK(s, 128) {
		return errors.New("Named vector must be valid UTF-8 and at most 128 bytes.")
	}
	return nil
}
func objectJSON(s string) (json.RawMessage, error) {
	if len(s) > 512<<10 || !utf8.ValidString(s) {
		return nil, errors.New("JSON object exceeds 512 KiB or is not valid UTF-8.")
	}
	var v map[string]json.RawMessage
	if decodeJSON([]byte(s), &v) != nil || v == nil {
		return nil, errors.New("Provide a JSON object string.")
	}
	return json.RawMessage(s), nil
}
func (c *client) call(ctx context.Context, method, path string, q url.Values, body any) (json.RawMessage, error) {
	b, e := c.request(ctx, method, path, q, body, 200)
	if e != nil {
		return nil, e
	}
	var env struct {
		Status json.RawMessage `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	if e = decodeJSON(b, &env); e != nil {
		return nil, e
	}
	var status string
	if json.Unmarshal(env.Status, &status) != nil || status != "ok" {
		return nil, errors.New("Qdrant rejected the operation. Check collection, vector dimensions and permissions; inspect writes before retrying.")
	}
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return nil, errors.New("Qdrant returned no result.")
	}
	return env.Result, nil
}
func timeoutQuery() url.Values { return url.Values{"timeout": {"30"}} }
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := c.list_collections(ctx, &ListCollectionsIn{})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Qdrant collection-list access verified; writes may require additional permissions."}, nil
}
func (c *client) list_collections(ctx context.Context, _ *ListCollectionsIn) (*ListCollectionsOut, error) {
	b, e := c.call(ctx, "GET", "/collections", nil, nil)
	if e != nil {
		return nil, e
	}
	var v struct {
		Collections []schema.Collection `json:"collections"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	if v.Collections == nil {
		return nil, errors.New("Qdrant returned no collection list.")
	}
	seen := map[string]bool{}
	for _, v := range v.Collections {
		if v.Name == "" || seen[v.Name] {
			return nil, errors.New("Qdrant returned invalid or duplicate collections.")
		}
		seen[v.Name] = true
	}
	return &ListCollectionsOut{Collections: v.Collections}, nil
}
func (c *client) get_collection(ctx context.Context, in *GetCollectionIn) (*GetCollectionOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "GET", path, nil, nil)
	if e != nil {
		return nil, e
	}
	var v struct {
		Status              string          `json:"status"`
		PointsCount         *int64          `json:"points_count"`
		IndexedVectorsCount *int64          `json:"indexed_vectors_count"`
		Config              json.RawMessage `json:"config"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	if v.Status == "" || (v.PointsCount != nil && *v.PointsCount < 0) || (v.IndexedVectorsCount != nil && *v.IndexedVectorsCount < 0) {
		return nil, errors.New("Qdrant returned invalid collection metadata.")
	}
	cfg, e := objectJSON(string(v.Config))
	if e != nil {
		return nil, e
	}
	return &GetCollectionOut{Collection: schema.CollectionInfo{Name: in.Collection, Status: v.Status, PointsCount: v.PointsCount, IndexedVectorsCount: v.IndexedVectorsCount, ConfigJSON: string(cfg)}}, nil
}
func (c *client) create_collection(ctx context.Context, in *CreateCollectionIn) (*CreateCollectionOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	if in.Size < 1 || in.Size > 65536 {
		return nil, errors.New("Vector dimensions must be 1..65536.")
	}
	switch in.Distance {
	case "Cosine", "Euclid", "Dot", "Manhattan":
	default:
		return nil, errors.New("Distance must be Cosine, Euclid, Dot or Manhattan.")
	}
	if e = namedVector(in.VectorName); e != nil {
		return nil, e
	}
	var vectors any = map[string]any{"size": in.Size, "distance": in.Distance}
	if in.VectorName != "" {
		vectors = map[string]any{in.VectorName: vectors}
	}
	b, e := c.call(ctx, "PUT", path, timeoutQuery(), map[string]any{"vectors": vectors})
	if e != nil {
		return nil, e
	}
	var ok bool
	if decodeJSON(b, &ok) != nil || !ok {
		return nil, errors.New("Qdrant did not confirm collection creation. Inspect the collection before retrying.")
	}
	return &CreateCollectionOut{Created: true}, nil
}
func parsePoints(b []byte, limit int, scored bool) ([]schema.Point, error) {
	var rows []struct {
		ID      json.RawMessage `json:"id"`
		Payload json.RawMessage `json:"payload"`
		Score   *float64        `json:"score"`
	}
	if decodeJSON(b, &rows) != nil || rows == nil || len(rows) > limit {
		return nil, errors.New("Qdrant returned an invalid point collection.")
	}
	out := []schema.Point{}
	seen := map[string]bool{}
	for _, r := range rows {
		id, e := readID(r.ID)
		if e != nil || seen[id] {
			return nil, errors.New("Qdrant returned invalid or duplicate point IDs.")
		}
		seen[id] = true
		v := schema.Point{ID: id, Score: r.Score}
		if scored && (r.Score == nil || math.IsNaN(*r.Score) || math.IsInf(*r.Score, 0)) {
			return nil, errors.New("Qdrant returned no finite similarity score.")
		}
		if len(r.Payload) > 0 && string(r.Payload) != "null" {
			p, e := objectJSON(string(r.Payload))
			if e != nil {
				return nil, e
			}
			s := string(p)
			v.PayloadJSON = &s
		}
		out = append(out, v)
	}
	return out, nil
}
func (c *client) query_points(ctx context.Context, in *QueryPointsIn) (*QueryPointsOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	if e = vectorOK(in.Vector); e != nil {
		return nil, e
	}
	if e = namedVector(in.VectorName); e != nil {
		return nil, e
	}
	limit, offset := 10, 0
	if in.Options.Limit != nil {
		limit = *in.Options.Limit
	}
	if in.Options.Offset != nil {
		offset = *in.Options.Offset
	}
	if limit < 1 || limit > 1000 || offset < 0 || offset > 1000000 {
		return nil, errors.New("Query limit must be 1..1000 and offset 0..1000000.")
	}
	body := map[string]any{"query": in.Vector, "limit": limit, "offset": offset, "with_payload": true, "with_vector": false}
	if in.VectorName != "" {
		body["using"] = in.VectorName
	}
	if in.Options.ScoreThreshold != nil {
		n := *in.Options.ScoreThreshold
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, errors.New("Score threshold must be finite.")
		}
		body["score_threshold"] = n
	}
	if in.Options.FilterJSON != nil {
		v, e := objectJSON(*in.Options.FilterJSON)
		if e != nil {
			return nil, e
		}
		body["filter"] = v
	}
	b, e := c.call(ctx, "POST", path+"/points/query", timeoutQuery(), body)
	if e != nil {
		return nil, e
	}
	var v struct {
		Points json.RawMessage `json:"points"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	rows, e := parsePoints(v.Points, limit, true)
	if e != nil {
		return nil, e
	}
	return &QueryPointsOut{Points: rows}, nil
}
func writeReceipt(b []byte) (schema.WriteReceipt, error) {
	var v struct {
		Status string          `json:"status"`
		ID     json.RawMessage `json:"operation_id"`
	}
	if decodeJSON(b, &v) != nil || v.Status != "completed" {
		return schema.WriteReceipt{}, errors.New("Qdrant did not confirm completion despite wait=true. Inspect the write before retrying.")
	}
	o := schema.WriteReceipt{Status: v.Status}
	if len(v.ID) > 0 && string(v.ID) != "null" {
		s := string(v.ID)
		if !decimalID.MatchString(s) {
			return o, errors.New("Qdrant returned an invalid operation ID.")
		}
		if _, e := strconv.ParseUint(s, 10, 64); e != nil {
			return o, errors.New("Qdrant operation ID exceeds uint64.")
		}
		o.OperationID = &s
	}
	return o, nil
}
func (c *client) upsert_points(ctx context.Context, in *UpsertPointsIn) (*UpsertPointsOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	if e = namedVector(in.VectorName); e != nil {
		return nil, e
	}
	if len(in.Points) < 1 || len(in.Points) > 256 {
		return nil, errors.New("Upsert requires 1..256 points.")
	}
	rows := []map[string]any{}
	seen := map[string]bool{}
	size := 0
	for _, p := range in.Points {
		id, canonical, e := pointID(p.ID)
		if e != nil {
			return nil, e
		}
		if seen[canonical] {
			return nil, errors.New("Point IDs must be unique within a batch.")
		}
		seen[canonical] = true
		if e = vectorOK(p.Vector); e != nil {
			return nil, e
		}
		if size != 0 && len(p.Vector) != size {
			return nil, errors.New("All vectors in a batch must have the same dimensions.")
		}
		size = len(p.Vector)
		var vec any = p.Vector
		if in.VectorName != "" {
			vec = map[string]any{in.VectorName: p.Vector}
		}
		r := map[string]any{"id": id, "vector": vec}
		if p.PayloadJSON != nil {
			v, e := objectJSON(*p.PayloadJSON)
			if e != nil {
				return nil, e
			}
			r["payload"] = v
		}
		rows = append(rows, r)
	}
	q := timeoutQuery()
	q.Set("wait", "true")
	b, e := c.call(ctx, "PUT", path+"/points", q, map[string]any{"points": rows})
	if e != nil {
		return nil, e
	}
	v, e := writeReceipt(b)
	if e != nil {
		return nil, e
	}
	return &UpsertPointsOut{Receipt: v}, nil
}
func idsInput(ids []string) ([]any, []string, error) {
	if len(ids) < 1 || len(ids) > 256 {
		return nil, nil, errors.New("Provide 1..256 point IDs.")
	}
	wire := []any{}
	canonical := []string{}
	seen := map[string]bool{}
	for _, s := range ids {
		id, key, e := pointID(s)
		if e != nil {
			return nil, nil, e
		}
		if seen[key] {
			return nil, nil, errors.New("Point IDs must be unique.")
		}
		seen[key] = true
		wire = append(wire, id)
		canonical = append(canonical, key)
	}
	return wire, canonical, nil
}
func (c *client) retrieve_points(ctx context.Context, in *RetrievePointsIn) (*RetrievePointsOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	ids, keys, e := idsInput(in.IDs)
	if e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "POST", path+"/points", timeoutQuery(), map[string]any{"ids": ids, "with_payload": true, "with_vector": false})
	if e != nil {
		return nil, e
	}
	rows, e := parsePoints(b, len(ids), false)
	if e != nil {
		return nil, e
	}
	wanted := map[string]bool{}
	for _, id := range keys {
		wanted[id] = true
	}
	for _, r := range rows {
		if !wanted[r.ID] {
			return nil, errors.New("Qdrant returned an unrequested point.")
		}
		delete(wanted, r.ID)
	}
	missing := []string{}
	for _, id := range keys {
		if wanted[id] {
			missing = append(missing, id)
		}
	}
	return &RetrievePointsOut{Points: rows, MissingIDs: missing}, nil
}
func (c *client) scroll_points(ctx context.Context, in *ScrollPointsIn) (*ScrollPointsOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	limit := 100
	if in.Options.Limit != nil {
		limit = *in.Options.Limit
	}
	if limit < 1 || limit > 1000 {
		return nil, errors.New("Scroll page size must be 1..1000.")
	}
	body := map[string]any{"limit": limit, "with_payload": true, "with_vector": false}
	prior := ""
	if in.Options.Offset != nil {
		var id any
		id, prior, e = pointID(*in.Options.Offset)
		if e != nil {
			return nil, e
		}
		body["offset"] = id
	}
	if in.Options.FilterJSON != nil {
		v, e := objectJSON(*in.Options.FilterJSON)
		if e != nil {
			return nil, e
		}
		body["filter"] = v
	}
	b, e := c.call(ctx, "POST", path+"/points/scroll", timeoutQuery(), body)
	if e != nil {
		return nil, e
	}
	var v struct {
		Points json.RawMessage `json:"points"`
		Next   json.RawMessage `json:"next_page_offset"`
	}
	if e = decodeJSON(b, &v); e != nil {
		return nil, e
	}
	rows, e := parsePoints(v.Points, limit, false)
	if e != nil {
		return nil, e
	}
	if len(v.Next) == 0 {
		return nil, errors.New("Qdrant returned no scroll continuation metadata.")
	}
	next := ""
	if string(v.Next) != "null" {
		next, e = readID(v.Next)
		if e != nil || next == prior || len(rows) == 0 {
			return nil, errors.New("Qdrant returned an invalid or non-advancing scroll offset.")
		}
	}
	return &ScrollPointsOut{Points: rows, NextOffset: next, HasMore: next != ""}, nil
}
func (c *client) delete_points(ctx context.Context, in *DeletePointsIn) (*DeletePointsOut, error) {
	path, e := collectionPath(in.Collection)
	if e != nil {
		return nil, e
	}
	ids, _, e := idsInput(in.IDs)
	if e != nil {
		return nil, e
	}
	q := timeoutQuery()
	q.Set("wait", "true")
	b, e := c.call(ctx, "POST", path+"/points/delete", q, map[string]any{"points": ids})
	if e != nil {
		return nil, e
	}
	v, e := writeReceipt(b)
	if e != nil {
		return nil, e
	}
	return &DeletePointsOut{Receipt: v}, nil
}
