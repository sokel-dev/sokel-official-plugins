package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/salesforce/schema"
	"math"
	"net/url"
	"regexp"
	"strings"
)

const apiPath = "/services/data/v65.0"

var apiName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,254}$`)
var recordID = regexp.MustCompile(`^(?:[A-Za-z0-9]{15}|[A-Za-z0-9]{18})$`)
var queryLocator = regexp.MustCompile(`^/services/data/v65\.0/query/[A-Za-z0-9]+-[0-9]+$`)
var selectQuery = regexp.MustCompile(`(?i)^SELECT\s+`)
var mutableQuery = regexp.MustCompile(`(?i)\bFOR\s+(UPDATE|VIEW|REFERENCE)\b`)

func objectPath(name string) (string, error) {
	if !apiName.MatchString(name) {
		return "", fmt.Errorf("invalid Salesforce object API name")
	}
	return apiPath + "/sobjects/" + name, nil
}
func rowPath(object, id string) (string, error) {
	p, e := objectPath(object)
	if e != nil {
		return "", e
	}
	if !recordID.MatchString(id) {
		return "", fmt.Errorf("record ID must contain 15 or 18 alphanumeric characters")
	}
	return p + "/" + id, nil
}
func (a *api) health_check(c context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		Daily *struct {
			Max       *int `json:"Max"`
			Remaining *int `json:"Remaining"`
		} `json:"DailyApiRequests"`
	}
	status, e := a.request(c, "GET", apiPath+"/limits", nil, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if status != 200 || r.Daily == nil || r.Daily.Max == nil || r.Daily.Remaining == nil {
		return &HealthCheckOut{Message: "Salesforce did not confirm authenticated API access"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Salesforce API access is valid; object and field permissions are checked per operation"}, nil
}
func (a *api) describe_object(c context.Context, in *DescribeObjectIn) (*DescribeObjectOut, error) {
	p, e := objectPath(in.ObjectName)
	if e != nil {
		return nil, e
	}
	var r struct {
		Name       string               `json:"name"`
		Label      string               `json:"label"`
		Createable *bool                `json:"createable"`
		Updateable *bool                `json:"updateable"`
		Fields     []schema.ObjectField `json:"fields"`
	}
	status, e := a.request(c, "GET", p+"/describe", nil, nil, &r)
	if e != nil {
		return nil, e
	}
	if status != 200 || r.Name == "" || r.Fields == nil || r.Createable == nil || r.Updateable == nil {
		return nil, fmt.Errorf("Salesforce response is missing object metadata")
	}
	for _, f := range r.Fields {
		if f.Name == "" || f.Type == "" {
			return nil, fmt.Errorf("Salesforce returned incomplete field metadata")
		}
	}
	return &DescribeObjectOut{Name: r.Name, Label: r.Label, Createable: *r.Createable, Updateable: *r.Updateable, Fields: r.Fields}, nil
}

type queryResult struct {
	Records        []any  `json:"records"`
	TotalSize      *int   `json:"totalSize"`
	Done           *bool  `json:"done"`
	NextRecordsURL string `json:"nextRecordsUrl"`
}

func (a *api) queryPage(c context.Context, path string, q url.Values) (queryResult, error) {
	var r queryResult
	status, e := a.request(c, "GET", path, q, nil, &r)
	if e != nil {
		return r, e
	}
	if status != 200 || r.Records == nil || r.TotalSize == nil || *r.TotalSize < 0 || r.Done == nil {
		return r, fmt.Errorf("Salesforce response is missing query records or pagination metadata")
	}
	for _, v := range r.Records {
		if v == nil {
			return r, fmt.Errorf("Salesforce query returned a null record")
		}
		if _, ok := v.(map[string]any); !ok {
			return r, fmt.Errorf("Salesforce query returned a non-object record")
		}
	}
	if !*r.Done {
		if !queryLocator.MatchString(r.NextRecordsURL) || r.NextRecordsURL == path {
			return r, fmt.Errorf("Salesforce returned an invalid or repeated query continuation path")
		}
	} else if r.NextRecordsURL != "" {
		return r, fmt.Errorf("Salesforce query completion conflicts with its continuation path")
	}
	return r, nil
}
func (a *api) query(c context.Context, in *QueryIn) (*QueryOut, error) {
	soql := strings.TrimSpace(in.Soql)
	if len(soql) > 16000 || !selectQuery.MatchString(soql) || strings.Contains(soql, ";") || strings.Contains(soql, "/*") || strings.Contains(soql, "--") || mutableQuery.MatchString(soql) {
		return nil, fmt.Errorf("supply a SELECT SOQL query up to 16000 characters without comments, semicolons or FOR UPDATE/VIEW/REFERENCE")
	}
	r, e := a.queryPage(c, apiPath+"/query", url.Values{"q": {soql}})
	if e != nil {
		return nil, e
	}
	return &QueryOut{Records: r.Records, TotalSize: *r.TotalSize, Done: *r.Done, NextRecordsURL: r.NextRecordsURL}, nil
}
func (a *api) query_more(c context.Context, in *QueryMoreIn) (*QueryMoreOut, error) {
	if !queryLocator.MatchString(in.NextRecordsURL) {
		return nil, fmt.Errorf("next_records_url must be the relative v65.0 query continuation path returned by Salesforce")
	}
	r, e := a.queryPage(c, in.NextRecordsURL, nil)
	if e != nil {
		return nil, e
	}
	return &QueryMoreOut{Records: r.Records, TotalSize: *r.TotalSize, Done: *r.Done, NextRecordsURL: r.NextRecordsURL}, nil
}
func (a *api) get_record(c context.Context, in *GetRecordIn) (*GetRecordOut, error) {
	p, e := rowPath(in.ObjectName, in.RecordID)
	if e != nil {
		return nil, e
	}
	q := url.Values{}
	if len(in.FieldNames) > 0 {
		for _, f := range in.FieldNames {
			if !apiName.MatchString(f) {
				return nil, fmt.Errorf("requested fields must be Salesforce field API names")
			}
		}
		names := append([]string{"Id"}, in.FieldNames...)
		seen := map[string]bool{}
		unique := []string{}
		for _, v := range names {
			if !seen[v] {
				seen[v] = true
				unique = append(unique, v)
			}
		}
		q.Set("fields", strings.Join(unique, ","))
	}
	var v map[string]any
	status, e := a.request(c, "GET", p, q, nil, &v)
	if e != nil {
		return nil, e
	}
	id, ok := v["Id"].(string)
	if status != 200 || !ok || !recordID.MatchString(id) {
		return nil, fmt.Errorf("Salesforce response is missing a record ID")
	}
	if id[:15] != in.RecordID[:15] {
		return nil, fmt.Errorf("Salesforce returned an unexpected record ID")
	}
	return &GetRecordOut{Record: v}, nil
}
func safeNumbers(v any) error {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 9007199254740991 {
			return fmt.Errorf("large numeric field values must be supplied as strings to preserve precision")
		}
	case map[string]any:
		for _, x := range n {
			if e := safeNumbers(x); e != nil {
				return e
			}
		}
	case []any:
		for _, x := range n {
			if e := safeNumbers(x); e != nil {
				return e
			}
		}
	}
	return nil
}
func validateFields(v map[string]any) error {
	if len(v) == 0 {
		return fmt.Errorf("at least one field value is required")
	}
	for k, x := range v {
		if !apiName.MatchString(k) || strings.EqualFold(k, "Id") || strings.EqualFold(k, "attributes") {
			return fmt.Errorf("field keys must be writable API names; Id and attributes are excluded")
		}
		if e := safeNumbers(x); e != nil {
			return e
		}
	}
	return nil
}

type writeReceipt struct {
	ID      string            `json:"id"`
	Success *bool             `json:"success"`
	Errors  []json.RawMessage `json:"errors"`
	Created *bool             `json:"created"`
}

func validReceipt(r writeReceipt) error {
	if !recordID.MatchString(r.ID) || r.Success == nil || !*r.Success || r.Errors == nil || len(r.Errors) != 0 {
		return fmt.Errorf("Salesforce did not confirm a successful record write")
	}
	return nil
}
func (a *api) create_record(c context.Context, in *CreateRecordIn) (*CreateRecordOut, error) {
	p, e := objectPath(in.ObjectName)
	if e != nil {
		return nil, e
	}
	if e = validateFields(in.Fields); e != nil {
		return nil, e
	}
	var r writeReceipt
	status, e := a.request(c, "POST", p, nil, in.Fields, &r)
	if e != nil {
		return nil, e
	}
	if status != 201 {
		return nil, fmt.Errorf("Salesforce returned an unexpected create status")
	}
	if e = validReceipt(r); e != nil {
		return nil, e
	}
	return &CreateRecordOut{RecordID: r.ID, Success: true}, nil
}
func (a *api) update_record(c context.Context, in *UpdateRecordIn) (*UpdateRecordOut, error) {
	p, e := rowPath(in.ObjectName, in.RecordID)
	if e != nil {
		return nil, e
	}
	if e = validateFields(in.Fields); e != nil {
		return nil, e
	}
	status, e := a.request(c, "PATCH", p, nil, in.Fields, nil)
	if e != nil {
		return nil, e
	}
	if status != 204 {
		return nil, fmt.Errorf("Salesforce did not confirm the record update")
	}
	return &UpdateRecordOut{RecordID: in.RecordID, Success: true}, nil
}
func (a *api) upsert_record(c context.Context, in *UpsertRecordIn) (*UpsertRecordOut, error) {
	p, e := objectPath(in.ObjectName)
	if e != nil {
		return nil, e
	}
	if !apiName.MatchString(in.ExternalIDField) || strings.EqualFold(in.ExternalIDField, "Id") {
		return nil, fmt.Errorf("provide an external ID field API name; Id is not supported for upsert")
	}
	if in.ExternalID == "" || len(in.ExternalID) > 255 || in.ExternalID == "." || in.ExternalID == ".." || strings.ContainsAny(in.ExternalID, "/\\\\?#%\r\n\x00") {
		return nil, fmt.Errorf("external ID must be a nonempty path-safe value up to 255 characters")
	}
	if e = validateFields(in.Fields); e != nil {
		return nil, e
	}
	if _, ok := in.Fields[in.ExternalIDField]; ok {
		return nil, fmt.Errorf("supply the external ID only in external_id, not in field values")
	}
	p += "/" + in.ExternalIDField + "/" + url.PathEscape(in.ExternalID)
	var r writeReceipt
	status, e := a.request(c, "PATCH", p, nil, in.Fields, &r)
	if e != nil {
		return nil, e
	}
	if status != 200 && status != 201 {
		return nil, fmt.Errorf("Salesforce returned an unexpected upsert status")
	}
	if e = validReceipt(r); e != nil {
		return nil, e
	}
	if r.Created == nil || *r.Created != (status == 201) {
		return nil, fmt.Errorf("Salesforce returned inconsistent upsert creation metadata")
	}
	return &UpsertRecordOut{RecordID: r.ID, Success: true, Created: *r.Created}, nil
}
