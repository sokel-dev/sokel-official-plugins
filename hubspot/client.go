package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var recordID = regexp.MustCompile(`^[0-9]{1,30}$`)
var propertyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,255}$`)

func objectPath(kind string) (string, error) {
	switch kind {
	case "contacts", "companies", "deals":
		return "/crm/v3/objects/" + kind, nil
	}
	return "", errors.New("object_type must be contacts, companies or deals.")
}
func propertiesQuery(names []string) (string, error) {
	if len(names) > 100 {
		return "", errors.New("At most 100 returned properties are supported.")
	}
	for _, s := range names {
		if !propertyName.MatchString(s) {
			return "", errors.New("Invalid property name.")
		}
	}
	return strings.Join(names, ","), nil
}

type upstreamRecord struct {
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties"`
	CreatedAt  string         `json:"createdAt"`
	UpdatedAt  string         `json:"updatedAt"`
	Archived   bool           `json:"archived"`
}

func (r upstreamRecord) typed() (CRMRecord, error) {
	if !recordID.MatchString(r.ID) || r.Properties == nil {
		return CRMRecord{}, errors.New("HubSpot returned a record without its ID or properties.")
	}
	for _, v := range r.Properties {
		if v != nil {
			if _, ok := v.(string); !ok {
				return CRMRecord{}, errors.New("HubSpot returned a property value that is not a string or null.")
			}
		}
	}
	return CRMRecord{ID: r.ID, Properties: r.Properties, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Archived: r.Archived}, nil
}

type recordsPage struct {
	Results []upstreamRecord `json:"results"`
	Total   *int64           `json:"total"`
	Paging  struct {
		Next struct {
			After json.RawMessage `json:"after"`
		} `json:"next"`
	} `json:"paging"`
}

func (p recordsPage) typed() ([]CRMRecord, string, error) {
	if p.Results == nil {
		return nil, "", errors.New("HubSpot response is missing the results array.")
	}
	rows := make([]CRMRecord, 0, len(p.Results))
	for _, r := range p.Results {
		v, e := r.typed()
		if e != nil {
			return nil, "", e
		}
		rows = append(rows, v)
	}
	after := ""
	raw := p.Paging.Next.After
	if len(raw) > 0 && string(raw) != "null" {
		if raw[0] == '"' {
			if json.Unmarshal(raw, &after) != nil {
				return nil, "", errors.New("HubSpot returned an invalid cursor.")
			}
		} else {
			var n json.Number
			if json.Unmarshal(raw, &n) != nil || !recordID.MatchString(string(n)) {
				return nil, "", errors.New("HubSpot returned an invalid cursor.")
			}
			after = string(n)
		}
		if after == "" || len(after) > 4096 {
			return nil, "", errors.New("HubSpot returned an invalid cursor.")
		}
	}
	return rows, after, nil
}
func (c *client) listRecords(ctx context.Context, in *ListRecordsIn) (*ListRecordsOut, error) {
	path, e := objectPath(in.ObjectType)
	if e != nil {
		return nil, e
	}
	props, e := propertiesQuery(in.Properties)
	if e != nil {
		return nil, e
	}
	if len(in.After) > 4096 {
		return nil, errors.New("Pagination cursor exceeds 4096 characters.")
	}
	q := url.Values{"limit": {"100"}, "archived": {"false"}}
	if props != "" {
		q.Set("properties", props)
	}
	if in.After != "" {
		q.Set("after", in.After)
	}
	var page recordsPage
	if _, e = c.json(ctx, "GET", path, q, nil, &page); e != nil {
		return nil, e
	}
	rows, after, e := page.typed()
	if after != "" && after == in.After {
		return nil, errors.New("HubSpot repeated the current pagination cursor.")
	}
	return &ListRecordsOut{Records: rows, NextAfter: after}, e
}
func (c *client) getRecord(ctx context.Context, in *GetRecordIn) (*GetRecordOut, error) {
	path, e := objectPath(in.ObjectType)
	if e != nil {
		return nil, e
	}
	if !recordID.MatchString(in.RecordID) {
		return nil, errors.New("record_id must be a numeric HubSpot record ID.")
	}
	props, e := propertiesQuery(in.Properties)
	if e != nil {
		return nil, e
	}
	q := url.Values{"archived": {"false"}}
	if props != "" {
		q.Set("properties", props)
	}
	var r upstreamRecord
	if _, e = c.json(ctx, "GET", path+"/"+in.RecordID, q, nil, &r); e != nil {
		return nil, e
	}
	record, e := r.typed()
	if e == nil && record.ID != in.RecordID {
		return nil, errors.New("HubSpot returned a different record ID.")
	}
	return &GetRecordOut{Record: record}, e
}
func validateProperties(p map[string]any) error {
	if len(p) == 0 || len(p) > 100 {
		return errors.New("Provide 1 to 100 property values.")
	}
	for k, v := range p {
		if !propertyName.MatchString(k) {
			return errors.New("Invalid property name.")
		}
		s, ok := v.(string)
		if !ok {
			return errors.New("Write property values must be strings; use an empty string to clear a value.")
		}
		if len(s) > 65536 {
			return errors.New("A property value exceeds 64 KiB.")
		}
	}
	return nil
}
func (c *client) save(ctx context.Context, kind, id string, properties map[string]any) (CRMRecord, error) {
	path, e := objectPath(kind)
	if e != nil {
		return CRMRecord{}, e
	}
	if e = validateProperties(properties); e != nil {
		return CRMRecord{}, e
	}
	method := "POST"
	if id != "" {
		if !recordID.MatchString(id) {
			return CRMRecord{}, errors.New("record_id must be a numeric HubSpot record ID.")
		}
		method = "PATCH"
		path += "/" + id
	}
	var r upstreamRecord
	if _, e = c.json(ctx, method, path, nil, map[string]any{"properties": properties}, &r); e != nil {
		return CRMRecord{}, e
	}
	out, e := r.typed()
	if e != nil {
		return out, errors.New("HubSpot returned no valid write receipt. Inspect the record before retrying.")
	}
	if id != "" && out.ID != id {
		return out, errors.New("HubSpot returned a different write receipt ID. Inspect the record before retrying.")
	}
	return out, nil
}
func (c *client) createRecord(ctx context.Context, in *CreateRecordIn) (*CreateRecordOut, error) {
	r, e := c.save(ctx, in.ObjectType, "", in.Properties)
	return &CreateRecordOut{Record: r}, e
}
func (c *client) updateRecord(ctx context.Context, in *UpdateRecordIn) (*UpdateRecordOut, error) {
	if in.RecordID == "" {
		return nil, errors.New("record_id is required.")
	}
	r, e := c.save(ctx, in.ObjectType, in.RecordID, in.Properties)
	return &UpdateRecordOut{Record: r}, e
}
func (c *client) searchRecords(ctx context.Context, in *SearchRecordsIn) (*SearchRecordsOut, error) {
	path, e := objectPath(in.ObjectType)
	if e != nil {
		return nil, e
	}
	if _, e = propertiesQuery(in.Properties); e != nil {
		return nil, e
	}
	if len(in.Query) > 3000 || len(in.Filters) > 6 {
		return nil, errors.New("Search supports up to 3000 query bytes and six AND conditions.")
	}
	body := map[string]any{"limit": 100}
	if in.After != "" {
		n, e := strconv.ParseUint(in.After, 10, 64)
		if e != nil || n >= 10000 {
			return nil, errors.New("Search after must be a numeric offset below 10000; partition larger searches.")
		}
		body["after"] = in.After
	}
	if in.Query != "" {
		body["query"] = in.Query
	}
	if len(in.Properties) > 0 {
		body["properties"] = in.Properties
	}
	filters := []map[string]any{}
	for _, f := range in.Filters {
		if !propertyName.MatchString(f.PropertyName) {
			return nil, errors.New("Invalid filter property name.")
		}
		v := map[string]any{"propertyName": f.PropertyName, "operator": f.Operator}
		switch f.Operator {
		case "HAS_PROPERTY", "NOT_HAS_PROPERTY":
			if f.Value != "" || len(f.Values) > 0 {
				return nil, errors.New("Presence filters must not contain values.")
			}
		case "IN", "NOT_IN":
			if len(f.Values) < 1 || len(f.Values) > 100 || f.Value != "" {
				return nil, errors.New("IN and NOT_IN require 1 to 100 values and no scalar value.")
			}
			v["values"] = f.Values
		case "EQ", "NEQ", "LT", "LTE", "GT", "GTE", "CONTAINS_TOKEN", "NOT_CONTAINS_TOKEN":
			if len(f.Values) > 0 {
				return nil, errors.New("This operator accepts only a scalar value.")
			}
			v["value"] = f.Value
		default:
			return nil, errors.New("Unsupported filter operator.")
		}
		filters = append(filters, v)
	}
	if len(filters) > 0 {
		body["filterGroups"] = []any{map[string]any{"filters": filters}}
	}
	encoded, _ := json.Marshal(body)
	if len(encoded) > 3000 {
		return nil, errors.New("HubSpot search request exceeds 3000 bytes. Reduce the query or filters.")
	}
	var page recordsPage
	if _, e = c.json(ctx, "POST", path+"/search", nil, body, &page); e != nil {
		return nil, e
	}
	if page.Total == nil || *page.Total < 0 {
		return nil, errors.New("HubSpot search response is missing a valid total.")
	}
	rows, after, e := page.typed()
	if after != "" && after == in.After {
		return nil, errors.New("HubSpot repeated the current search cursor.")
	}
	return &SearchRecordsOut{Records: rows, NextAfter: after, Total: *page.Total}, e
}
func (c *client) listProperties(ctx context.Context, in *ListPropertiesIn) (*ListPropertiesOut, error) {
	if _, e := objectPath(in.ObjectType); e != nil {
		return nil, e
	}
	var r struct {
		Results []struct {
			Name        string           `json:"name"`
			Label       string           `json:"label"`
			Type        string           `json:"type"`
			FieldType   string           `json:"fieldType"`
			GroupName   string           `json:"groupName"`
			Description string           `json:"description"`
			Options     []PropertyOption `json:"options"`
		} `json:"results"`
	}
	if _, e := c.json(ctx, "GET", "/crm/v3/properties/"+in.ObjectType, nil, nil, &r); e != nil {
		return nil, e
	}
	if r.Results == nil {
		return nil, errors.New("HubSpot response is missing property definitions.")
	}
	out := &ListPropertiesOut{Properties: []PropertyDefinition{}}
	for _, p := range r.Results {
		if p.Name == "" || p.Type == "" {
			return nil, errors.New("HubSpot returned invalid property metadata.")
		}
		if p.Options == nil {
			p.Options = []PropertyOption{}
		}
		out.Properties = append(out.Properties, PropertyDefinition{Name: p.Name, Label: p.Label, Type: p.Type, FieldType: p.FieldType, GroupName: p.GroupName, Description: p.Description, Options: p.Options})
	}
	return out, nil
}
func (c *client) health(ctx context.Context) (*HealthCheckOut, error) {
	var p recordsPage
	_, e := c.json(ctx, "GET", "/crm/v3/objects/contacts", url.Values{"limit": {"1"}}, nil, &p)
	if e == nil {
		_, _, e = p.typed()
	}
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Contact read access verified. Other object and write scopes are checked by each operation."}, nil
}
