package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxResponse = 4 << 20

type client struct {
	base, token string
	http        *http.Client
}
type rateLimitError struct{ after time.Duration }

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("Airtable rate limit reached; retry after %d seconds. The request was not retried automatically.", int(e.after.Seconds()))
}
func newClient(token string) *client {
	base := strings.TrimRight(os.Getenv("AIRTABLE_API_BASE_URL"), "/")
	if base == "" {
		base = "https://api.airtable.com/v0"
	}
	return &client{base: base, token: strings.TrimSpace(token), http: &http.Client{Timeout: 40 * time.Second}}
}
func (c *client) httpClient() *http.Client {
	h := http.Client{Timeout: 40 * time.Second}
	if c.http != nil {
		h = *c.http
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &h
}
func retryDelay(s string) time.Duration {
	if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 86400 {
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(s); e == nil {
		d := time.Until(t)
		if d > 0 && d <= 24*time.Hour {
			return d.Round(time.Second) + time.Second
		}
	}
	return 30 * time.Second
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	if c.token == "" {
		return errors.New("Missing Airtable personal access token. Configure token in the plugin credential.")
	}
	u, err := url.Parse(c.base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Invalid AIRTABLE_API_BASE_URL deployment configuration.")
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("Field values cannot be encoded as JSON.")
		}
		if len(data) > 1<<20 {
			return errors.New("Record request exceeds 1 MiB. Reduce the field data.")
		}
	}
	endpoint := c.base + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("Invalid Airtable request configuration.")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Cannot reach Airtable. Check the plugin server network and deployment configuration.")
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return &rateLimitError{retryDelay(res.Header.Get("Retry-After"))}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return errors.New("Airtable access token is invalid or revoked.")
		case 403:
			return errors.New("Airtable permission denied. Check token scopes and access to the target base.")
		case 404:
			return errors.New("Airtable base, table or record does not exist, or the token cannot access it.")
		case 422:
			return errors.New("Invalid Airtable field or filter formula. Check the table schema.")
		}
		return fmt.Errorf("Airtable HTTP %d. Check service status. Writes are not retried automatically.", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return errors.New("Failed to read the Airtable response.")
	}
	if len(b) > maxResponse {
		return errors.New("Airtable response exceeds 4 MiB. Request fewer fields.")
	}
	if b = bytes.TrimSpace(b); len(b) == 0 || b[0] != '{' {
		return errors.New("Airtable returned an invalid JSON object.")
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(b, &envelope) != nil {
		return errors.New("Airtable returned invalid JSON.")
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return errors.New("Airtable returned an API error. Check parameters and permissions.")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(out) != nil {
		return errors.New("Airtable response does not match the expected structure.")
	}
	return nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)

func validID(id, prefix, label string) error {
	if !strings.HasPrefix(id, prefix) || len(id) <= len(prefix) || len(id) > 64 || !idPattern.MatchString(id) {
		return fmt.Errorf("Invalid %s. Enter an ID starting with %s.", label, prefix)
	}
	return nil
}
func tablePath(base, table string) (string, error) {
	if e := validID(base, "app", "base ID"); e != nil {
		return "", e
	}
	if e := validID(table, "tbl", "table ID"); e != nil {
		return "", e
	}
	return "/" + base + "/" + table, nil
}
func recordPath(base, table, id string) (string, error) {
	p, e := tablePath(base, table)
	if e != nil {
		return "", e
	}
	if e = validID(id, "rec", "record ID"); e != nil {
		return "", e
	}
	return p + "/" + id, nil
}
func validateRecord(r *Record) error {
	if validID(r.ID, "rec", "record ID") != nil || r.CreatedTime == "" {
		return errors.New("Airtable record response is missing its ID or creation time.")
	}
	if _, e := time.Parse(time.RFC3339Nano, r.CreatedTime); e != nil {
		return errors.New("Invalid Airtable record creation time.")
	}
	if r.Fields == nil {
		r.Fields = map[string]any{}
	}
	return nil
}
func (c *client) bases(ctx context.Context, in *AirtableBasesIn) (*AirtableBasesOut, error) {
	if len(in.Offset) > 4096 {
		return nil, errors.New("Pagination cursor is too long.")
	}
	q := url.Values{}
	if in.Offset != "" {
		q.Set("offset", in.Offset)
	}
	var out AirtableBasesOut
	if err := c.request(ctx, "GET", "/meta/bases", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Bases == nil {
		return nil, errors.New("Airtable response is missing the bases array.")
	}
	for _, b := range out.Bases {
		if validID(b.ID, "app", "base ID") != nil {
			return nil, errors.New("Airtable base response is missing a valid ID.")
		}
	}
	return &out, nil
}
func (c *client) health(ctx context.Context) (*HealthCheckOut, error) {
	_, err := c.bases(ctx, &AirtableBasesIn{})
	if err != nil {
		return nil, err
	}
	return &HealthCheckOut{OK: true}, nil
}
func (c *client) tables(ctx context.Context, in *AirtableTablesIn) (*AirtableTablesOut, error) {
	if err := validID(in.BaseID, "app", "base ID"); err != nil {
		return nil, err
	}
	var out AirtableTablesOut
	if err := c.request(ctx, "GET", "/meta/bases/"+in.BaseID+"/tables", nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Tables == nil {
		return nil, errors.New("Airtable response is missing the tables array.")
	}
	for _, t := range out.Tables {
		if validID(t.ID, "tbl", "table ID") != nil {
			return nil, errors.New("Airtable table response is missing a valid ID.")
		}
	}
	return &out, nil
}
func (c *client) records(ctx context.Context, in *AirtableRecordsIn) (*AirtableRecordsOut, error) {
	p, e := tablePath(in.BaseID, in.TableID)
	if e != nil {
		return nil, e
	}
	if len(in.Offset) > 4096 || len(in.FilterFormula) > 8000 || len(in.View) > 1024 || len(in.Fields) > 100 {
		return nil, errors.New("Pagination cursor, filter formula, view or field count exceeds the supported limits.")
	}
	q := url.Values{"pageSize": {"100"}}
	for k, v := range map[string]string{"offset": in.Offset, "view": in.View, "filterByFormula": in.FilterFormula} {
		if v != "" {
			q.Set(k, v)
		}
	}
	for _, f := range in.Fields {
		if strings.TrimSpace(f) == "" || len(f) > 1024 {
			return nil, errors.New("Returned field names must contain 1 to 1024 bytes.")
		}
		q.Add("fields[]", f)
	}
	var out AirtableRecordsOut
	if e = c.request(ctx, "GET", p, q, nil, &out); e != nil {
		return nil, e
	}
	if out.Records == nil {
		return nil, errors.New("Airtable response is missing the records array.")
	}
	for i := range out.Records {
		if e = validateRecord(&out.Records[i]); e != nil {
			return nil, e
		}
	}
	return &out, nil
}
func (c *client) get(ctx context.Context, in *AirtableGetIn) (*AirtableGetOut, error) {
	p, e := recordPath(in.BaseID, in.TableID, in.RecordID)
	if e != nil {
		return nil, e
	}
	var out AirtableGetOut
	if e = c.request(ctx, "GET", p, nil, nil, &out.Record); e != nil {
		return nil, e
	}
	if e = validateRecord(&out.Record); e != nil {
		return nil, e
	}
	return &out, nil
}
func (c *client) write(ctx context.Context, method, path string, fields map[string]any, typecast bool) (Record, error) {
	var r Record
	if len(fields) == 0 || len(fields) > 500 {
		return r, errors.New("Field values must contain 1 to 500 fields.")
	}
	for k := range fields {
		if strings.TrimSpace(k) == "" {
			return r, errors.New("Field names must not be empty.")
		}
	}
	body := map[string]any{"fields": fields}
	if typecast {
		body["typecast"] = true
	}
	if e := c.request(ctx, method, path, nil, body, &r); e != nil {
		return r, e
	}
	if e := validateRecord(&r); e != nil {
		return r, errors.New("Airtable responded without a valid record receipt. Check the data before retrying a write.")
	}
	return r, nil
}
func (c *client) create(ctx context.Context, in *AirtableCreateIn) (*AirtableCreateOut, error) {
	p, e := tablePath(in.BaseID, in.TableID)
	if e != nil {
		return nil, e
	}
	r, e := c.write(ctx, "POST", p, in.Fields, in.Typecast)
	if e != nil {
		return nil, e
	}
	return &AirtableCreateOut{Record: r}, nil
}
func (c *client) update(ctx context.Context, in *AirtableUpdateIn) (*AirtableUpdateOut, error) {
	p, e := recordPath(in.BaseID, in.TableID, in.RecordID)
	if e != nil {
		return nil, e
	}
	r, e := c.write(ctx, "PATCH", p, in.Fields, in.Typecast)
	if e != nil {
		return nil, e
	}
	return &AirtableUpdateOut{Record: r}, nil
}
func (c *client) remove(ctx context.Context, in *AirtableDeleteIn) (*AirtableDeleteOut, error) {
	p, e := recordPath(in.BaseID, in.TableID, in.RecordID)
	if e != nil {
		return nil, e
	}
	var out AirtableDeleteOut
	if e = c.request(ctx, "DELETE", p, nil, nil, &out); e != nil {
		return nil, e
	}
	if out.ID != in.RecordID || !out.Deleted {
		return nil, errors.New("Airtable deletion response is missing a valid receipt. Check the data.")
	}
	return &out, nil
}
