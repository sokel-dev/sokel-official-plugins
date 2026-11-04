package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/prometheus/schema"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type envelope struct {
	Status   string          `json:"status"`
	Data     json.RawMessage `json:"data"`
	Warnings []string        `json:"warnings"`
	Infos    []string        `json:"infos"`
}

func (c *client) api(ctx context.Context, method, path string, q url.Values) (envelope, error) {
	var body []byte
	h := http.Header{}
	if method == "POST" {
		body = []byte(q.Encode())
		q = nil
		h.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	b, rh, e := c.request(ctx, method, path, q, body, h, 200)
	if e != nil {
		return envelope{}, e
	}
	var v envelope
	if e = decodeJSON(b, rh, &v); e != nil {
		return v, e
	}
	if v.Status != "success" {
		return v, errors.New("Prometheus did not report a successful API result")
	}
	if len(v.Data) == 0 || bytes.Equal(bytes.TrimSpace(v.Data), []byte("null")) {
		return v, errors.New("Prometheus returned missing or null data")
	}
	if v.Warnings == nil {
		v.Warnings = []string{}
	}
	if v.Infos == nil {
		v.Infos = []string{}
	}
	return v, nil
}
func (c *client) getBuildInfo(ctx context.Context, _ *GetBuildInfoIn) (*GetBuildInfoOut, error) {
	v, e := c.api(ctx, "GET", "/api/v1/status/buildinfo", nil)
	if e != nil {
		return nil, e
	}
	var b struct {
		Version   string `json:"version"`
		Revision  string `json:"revision"`
		BuildDate string `json:"buildDate"`
	}
	if json.Unmarshal(v.Data, &b) != nil || b.Version == "" {
		return nil, errors.New("Prometheus returned invalid build information")
	}
	return &GetBuildInfoOut{Version: b.Version, Revision: b.Revision, BuildDate: b.BuildDate}, nil
}
func timestamp(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || len(s) > 64 {
		return time.Time{}, errors.New("timestamps must be RFC3339 with a timezone")
	}
	return t, nil
}
func limitValue(p *int) (int, error) {
	n := 1000
	if p != nil {
		n = *p
	}
	if n < 1 || n > 10000 {
		return 0, errors.New("result limit must be between 1 and 10000")
	}
	return n, nil
}
func expression(s string) error {
	if strings.TrimSpace(s) == "" || len(s) > 65536 || strings.ContainsRune(s, 0) {
		return errors.New("PromQL expression must contain 1 to 65536 bytes without NUL")
	}
	return nil
}
func metricObject(raw json.RawMessage) error {
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return errors.New("Prometheus returned an invalid metric label set")
	}
	return nil
}
func sampleTuple(raw json.RawMessage, histogram bool, stringValue bool) error {
	var tuple []json.RawMessage
	if json.Unmarshal(raw, &tuple) != nil || len(tuple) != 2 {
		return errors.New("Prometheus returned an invalid sample tuple")
	}
	if raw := bytes.TrimSpace(tuple[0]); len(raw) == 0 || raw[0] == '"' || bytes.Equal(raw, []byte("null")) {
		return errors.New("Prometheus sample timestamp must be numeric")
	}
	var stamp json.Number
	if json.Unmarshal(tuple[0], &stamp) != nil {
		return errors.New("Prometheus sample timestamp must be numeric")
	}
	f, e := stamp.Float64()
	if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return errors.New("Prometheus returned an invalid sample timestamp")
	}
	if histogram {
		var h map[string]json.RawMessage
		if json.Unmarshal(tuple[1], &h) != nil || h == nil {
			return errors.New("Prometheus returned an invalid histogram")
		}
		return nil
	}
	var s string
	if json.Unmarshal(tuple[1], &s) != nil {
		return errors.New("Prometheus sample values must be strings")
	}
	if !stringValue {
		if _, e := strconv.ParseFloat(s, 64); e != nil {
			return errors.New("Prometheus returned an invalid sample value")
		}
	}
	return nil
}
func queryResult(data json.RawMessage, wantMatrix bool) (string, string, error) {
	var v struct {
		Type   string          `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &v) != nil || len(v.Result) == 0 || bytes.Equal(bytes.TrimSpace(v.Result), []byte("null")) {
		return "", "", errors.New("Prometheus returned an invalid query result")
	}
	if wantMatrix && v.Type != "matrix" {
		return "", "", errors.New("range query must return a matrix")
	}
	switch v.Type {
	case "scalar", "string":
		if e := sampleTuple(v.Result, false, v.Type == "string"); e != nil {
			return "", "", e
		}
	case "matrix", "vector":
		var series []map[string]json.RawMessage
		if json.Unmarshal(v.Result, &series) != nil || series == nil {
			return "", "", errors.New("query result must be a series array")
		}
		for _, s := range series {
			if e := metricObject(s["metric"]); e != nil {
				return "", "", e
			}
			hasSamples := false
			for _, key := range []string{"value", "values", "histogram", "histograms"} {
				raw, ok := s[key]
				if !ok {
					continue
				}
				if (v.Type == "vector" && (key == "values" || key == "histograms")) || (v.Type == "matrix" && (key == "value" || key == "histogram")) {
					return "", "", errors.New("sample shape does not match the query result type")
				}
				hasSamples = true
				hist := strings.HasPrefix(key, "histogram")
				if key == "values" || key == "histograms" {
					var tuples []json.RawMessage
					if json.Unmarshal(raw, &tuples) != nil || tuples == nil {
						return "", "", errors.New("invalid sample array")
					}
					for _, tuple := range tuples {
						if e := sampleTuple(tuple, hist, false); e != nil {
							return "", "", e
						}
					}
				} else if e := sampleTuple(raw, hist, false); e != nil {
					return "", "", e
				}
			}
			if !hasSamples {
				return "", "", errors.New("series has no sample field")
			}
		}
	default:
		return "", "", errors.New("Prometheus returned an unknown query result type")
	}
	return v.Type, string(v.Result), nil
}
func (c *client) query(ctx context.Context, in *QueryIn) (*QueryOut, error) {
	if e := expression(in.Query); e != nil {
		return nil, e
	}
	limit, e := limitValue(in.Options.Limit)
	if e != nil {
		return nil, e
	}
	q := url.Values{"query": {in.Query}, "timeout": {"30s"}, "limit": {strconv.Itoa(limit)}}
	if in.Options.Time != nil {
		if _, e = timestamp(*in.Options.Time); e != nil {
			return nil, e
		}
		q.Set("time", *in.Options.Time)
	}
	v, e := c.api(ctx, "POST", "/api/v1/query", q)
	if e != nil {
		return nil, e
	}
	typ, result, e := queryResult(v.Data, false)
	if e != nil {
		return nil, e
	}
	return &QueryOut{ResultType: typ, ResultJSON: result, Warnings: v.Warnings, Infos: v.Infos}, nil
}

var decimal = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

func (c *client) queryRange(ctx context.Context, in *QueryRangeIn) (*QueryRangeOut, error) {
	if e := expression(in.Query); e != nil {
		return nil, e
	}
	start, e := timestamp(in.Start)
	if e != nil {
		return nil, e
	}
	end, e := timestamp(in.End)
	if e != nil {
		return nil, e
	}
	step, e := strconv.ParseFloat(in.StepSeconds, 64)
	if e != nil || !decimal.MatchString(in.StepSeconds) || step <= 0 || math.IsInf(step, 0) || len(in.StepSeconds) > 32 {
		return nil, errors.New("step_seconds must be a positive decimal number")
	}
	span := end.Sub(start).Seconds()
	if end.Before(start) || span > 31*86400 || math.Floor(span/step)+1 > 11000 {
		return nil, errors.New("range must be ordered, at most 31 days, and at most 11000 evaluation points per series")
	}
	limit, e := limitValue(in.Options.Limit)
	if e != nil {
		return nil, e
	}
	q := url.Values{"query": {in.Query}, "start": {in.Start}, "end": {in.End}, "step": {in.StepSeconds}, "timeout": {"30s"}, "limit": {strconv.Itoa(limit)}}
	v, e := c.api(ctx, "POST", "/api/v1/query_range", q)
	if e != nil {
		return nil, e
	}
	typ, result, e := queryResult(v.Data, true)
	if e != nil {
		return nil, e
	}
	return &QueryRangeOut{ResultType: typ, ResultJSON: result, Warnings: v.Warnings, Infos: v.Infos}, nil
}
func metadataQuery(o schema.MetadataOptions, required bool) (url.Values, error) {
	limit, e := limitValue(o.Limit)
	if e != nil {
		return nil, e
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	var start, end time.Time
	if o.Start != nil {
		start, e = timestamp(*o.Start)
		if e != nil {
			return nil, e
		}
		q.Set("start", *o.Start)
	}
	if o.End != nil {
		end, e = timestamp(*o.End)
		if e != nil {
			return nil, e
		}
		q.Set("end", *o.End)
	}
	if o.Start != nil && o.End != nil && end.Before(start) {
		return nil, errors.New("end time must not precede start time")
	}
	if len(o.Matches) > 20 || (required && len(o.Matches) == 0) {
		return nil, errors.New("provide 1 to 20 series selectors for series discovery; other metadata operations allow none")
	}
	for _, s := range o.Matches {
		if e = expression(s); e != nil {
			return nil, e
		}
		if len(s) > 4096 {
			return nil, errors.New("series selector exceeds 4096 bytes")
		}
		q.Add("match[]", s)
	}
	return q, nil
}
func stringsData(data json.RawMessage) ([]string, error) {
	var a []string
	if json.Unmarshal(data, &a) != nil || a == nil {
		return nil, errors.New("Prometheus returned an invalid string list")
	}
	return a, nil
}
func (c *client) listLabelNames(ctx context.Context, in *ListLabelNamesIn) (*ListLabelNamesOut, error) {
	q, e := metadataQuery(in.Options, false)
	if e != nil {
		return nil, e
	}
	v, e := c.api(ctx, "GET", "/api/v1/labels", q)
	if e != nil {
		return nil, e
	}
	names, e := stringsData(v.Data)
	if e != nil {
		return nil, e
	}
	return &ListLabelNamesOut{Names: names, Warnings: v.Warnings, Infos: v.Infos}, nil
}

var labelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func (c *client) listLabelValues(ctx context.Context, in *ListLabelValuesIn) (*ListLabelValuesOut, error) {
	if !labelName.MatchString(in.Label) || len(in.Label) > 256 {
		return nil, errors.New("label name must use the traditional ASCII label syntax and contain at most 256 bytes")
	}
	q, e := metadataQuery(in.Options, false)
	if e != nil {
		return nil, e
	}
	v, e := c.api(ctx, "GET", "/api/v1/label/"+in.Label+"/values", q)
	if e != nil {
		return nil, e
	}
	values, e := stringsData(v.Data)
	if e != nil {
		return nil, e
	}
	return &ListLabelValuesOut{Values: values, Warnings: v.Warnings, Infos: v.Infos}, nil
}
func (c *client) findSeries(ctx context.Context, in *FindSeriesIn) (*FindSeriesOut, error) {
	q, e := metadataQuery(in.Options, true)
	if e != nil {
		return nil, e
	}
	v, e := c.api(ctx, "GET", "/api/v1/series", q)
	if e != nil {
		return nil, e
	}
	var rows []json.RawMessage
	if json.Unmarshal(v.Data, &rows) != nil || rows == nil {
		return nil, errors.New("Prometheus returned an invalid series list")
	}
	out := &FindSeriesOut{SeriesJSON: []string{}, Warnings: v.Warnings, Infos: v.Infos}
	for _, r := range rows {
		if e = metricObject(r); e != nil {
			return nil, e
		}
		out.SeriesJSON = append(out.SeriesJSON, string(r))
	}
	return out, nil
}
func (c *client) listAlerts(ctx context.Context, _ *ListAlertsIn) (*ListAlertsOut, error) {
	v, e := c.api(ctx, "GET", "/api/v1/alerts", nil)
	if e != nil {
		return nil, e
	}
	var data struct {
		Alerts *[]struct {
			Labels      json.RawMessage `json:"labels"`
			Annotations json.RawMessage `json:"annotations"`
			State       string          `json:"state"`
			ActiveAt    *string         `json:"activeAt"`
			Value       string          `json:"value"`
		} `json:"alerts"`
	}
	if json.Unmarshal(v.Data, &data) != nil || data.Alerts == nil {
		return nil, errors.New("Prometheus returned an invalid alerts result")
	}
	out := &ListAlertsOut{Alerts: []schema.Alert{}, Warnings: v.Warnings, Infos: v.Infos}
	for _, a := range *data.Alerts {
		if metricObject(a.Labels) != nil || metricObject(a.Annotations) != nil || (a.State != "pending" && a.State != "firing") || a.Value == "" {
			return nil, fmt.Errorf("Prometheus returned an invalid alert")
		}
		out.Alerts = append(out.Alerts, schema.Alert{LabelsJSON: string(a.Labels), AnnotationsJSON: string(a.Annotations), State: a.State, ActiveAt: a.ActiveAt, Value: a.Value})
	}
	return out, nil
}
