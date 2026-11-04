package main

import (
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/grafana/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var uidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validUID(s string) error {
	if !uidPattern.MatchString(s) {
		return errors.New("UID must contain 1–128 ASCII letters, digits, underscores or hyphens.")
	}
	return nil
}

type sourceWire struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	IsDefault bool   `json:"isDefault"`
	ReadOnly  bool   `json:"readOnly"`
}

func (w sourceWire) output() (schema.DataSource, error) {
	if w.UID == "" || w.Name == "" || w.Type == "" {
		return schema.DataSource{}, errors.New("Grafana returned an incomplete data source summary.")
	}
	return schema.DataSource{UID: w.UID, Name: w.Name, Type: w.Type, IsDefault: w.IsDefault, ReadOnly: w.ReadOnly}, nil
}

type hitWire struct {
	UID       string   `json:"uid"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Type      string   `json:"type"`
	Tags      []string `json:"tags"`
	FolderUID *string  `json:"folderUid"`
	IsStarred bool     `json:"isStarred"`
}

func (w hitWire) output() (schema.DashboardHit, error) {
	if w.UID == "" || w.Title == "" || w.Type != "dash-db" {
		return schema.DashboardHit{}, errors.New("Grafana returned an invalid dashboard search result.")
	}
	if w.Tags == nil {
		w.Tags = []string{}
	}
	return schema.DashboardHit{UID: w.UID, Title: w.Title, URL: w.URL, Tags: w.Tags, FolderUID: w.FolderUID, IsStarred: w.IsStarred}, nil
}

type annotationWire struct {
	ID           int64    `json:"id"`
	DashboardUID *string  `json:"dashboardUID"`
	PanelID      int64    `json:"panelId"`
	Time         *int64   `json:"time"`
	TimeEnd      *int64   `json:"timeEnd"`
	Text         string   `json:"text"`
	Tags         []string `json:"tags"`
}

func (w annotationWire) output() (schema.Annotation, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Annotation{}, e
	}
	if w.Time == nil || *w.Time < 0 || w.PanelID < 0 {
		return schema.Annotation{}, errors.New("Grafana returned invalid annotation timestamps or panel ID.")
	}
	v := schema.Annotation{ID: id, DashboardUID: w.DashboardUID, PanelID: strconv.FormatInt(w.PanelID, 10), Time: strconv.FormatInt(*w.Time, 10), Text: w.Text, Tags: w.Tags}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if w.TimeEnd != nil {
		if *w.TimeEnd < 0 {
			return schema.Annotation{}, errors.New("Grafana returned an invalid region end.")
		}
		s := strconv.FormatInt(*w.TimeEnd, 10)
		v.TimeEnd = &s
	}
	return v, nil
}
func checkedTags(tags []string) error {
	if len(tags) > 100 {
		return errors.New("At most 100 tags are supported.")
	}
	for _, v := range tags {
		if v == "" || !safeText(v, 255) {
			return errors.New("Tags must contain 1–255 bytes without control characters.")
		}
	}
	return nil
}
func nonnegative(s string) (int64, error) {
	if s == "0" {
		return 0, nil
	}
	return positiveID(s)
}
func epoch(s string, allowZero bool) (int64, error) {
	n, e := nonnegative(s)
	if e != nil {
		return 0, e
	}
	if n > 253402300799999 || (!allowZero && n == 0) {
		return 0, errors.New("Epoch milliseconds must be within years 1970–9999; creation time must be greater than zero.")
	}
	return n, nil
}
func (c *client) healthCheck(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
	one := 1
	_, e := c.searchDashboards(ctx, &SearchDashboardsIn{Options: schema.SearchOptions{Limit: &one}})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Grafana accepted the token and returned a dashboard search response. Data source and annotation permissions are checked separately."}, nil
}
func (c *client) listDataSources(ctx plugin.Ctx, in *ListDataSourcesIn) (*ListDataSourcesOut, error) {
	var rows []sourceWire
	if _, e := c.json(ctx, "GET", "/api/datasources", nil, nil, 200, &rows); e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("Grafana returned a null data source list.")
	}
	out := &ListDataSourcesOut{DataSources: make([]schema.DataSource, 0, len(rows))}
	for _, row := range rows {
		v, e := row.output()
		if e != nil {
			return nil, e
		}
		out.DataSources = append(out.DataSources, v)
	}
	return out, nil
}
func (c *client) getDataSource(ctx plugin.Ctx, in *GetDataSourceIn) (*GetDataSourceOut, error) {
	if e := validUID(in.UID); e != nil {
		return nil, e
	}
	var row sourceWire
	if _, e := c.json(ctx, "GET", "/api/datasources/uid/"+in.UID, nil, nil, 200, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	if v.UID != in.UID {
		return nil, errors.New("Grafana returned a different data source UID.")
	}
	return &GetDataSourceOut{DataSource: v}, nil
}
func (c *client) searchDashboards(ctx plugin.Ctx, in *SearchDashboardsIn) (*SearchDashboardsOut, error) {
	o := in.Options
	page, limit := 1, 100
	if o.Page != nil {
		page = *o.Page
	}
	if o.Limit != nil {
		limit = *o.Limit
	}
	if page < 1 || page > 100000 || limit < 1 || limit > 100 {
		return nil, errors.New("Page must be 1–100000 and page size must be 1–100.")
	}
	if !safeText(in.Query, 4096) {
		return nil, errors.New("Search query exceeds 4096 bytes or contains control characters.")
	}
	if e := checkedTags(o.Tags); e != nil {
		return nil, e
	}
	if len(o.FolderUIDs) > 100 {
		return nil, errors.New("At most 100 folder UIDs are supported.")
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(limit)}, "type": {"dash-db"}}
	if in.Query != "" {
		q.Set("query", in.Query)
	}
	for _, tag := range o.Tags {
		q.Add("tag", tag)
	}
	for _, uid := range o.FolderUIDs {
		if e := validUID(uid); e != nil {
			return nil, e
		}
		q.Add("folderUIDs", uid)
	}
	if o.Starred != nil {
		q.Set("starred", strconv.FormatBool(*o.Starred))
	}
	var rows []hitWire
	if _, e := c.json(ctx, "GET", "/api/search", q, nil, 200, &rows); e != nil {
		return nil, e
	}
	if rows == nil || len(rows) > limit {
		return nil, errors.New("Grafana returned an invalid dashboard search page.")
	}
	out := &SearchDashboardsOut{Dashboards: make([]schema.DashboardHit, 0, len(rows))}
	for _, row := range rows {
		v, e := row.output()
		if e != nil {
			return nil, e
		}
		out.Dashboards = append(out.Dashboards, v)
	}
	if len(rows) == limit {
		if page == 100000 {
			return nil, errors.New("Grafana search exceeds the supported page limit.")
		}
		out.NextPage = page + 1
	}
	return out, nil
}
func (c *client) getDashboard(ctx plugin.Ctx, in *GetDashboardIn) (*GetDashboardOut, error) {
	if e := validUID(in.UID); e != nil {
		return nil, e
	}
	var w struct {
		Dashboard json.RawMessage `json:"dashboard"`
	}
	if _, e := c.json(ctx, "GET", "/api/dashboards/uid/"+in.UID, nil, nil, 200, &w); e != nil {
		return nil, e
	}
	if len(w.Dashboard) == 0 || w.Dashboard[0] != '{' {
		return nil, errors.New("Grafana did not return a dashboard model.")
	}
	var meta struct {
		UID     string `json:"uid"`
		Title   string `json:"title"`
		Version *int64 `json:"version"`
	}
	if e := decodeJSON(w.Dashboard, &meta); e != nil {
		return nil, e
	}
	if meta.UID != in.UID || meta.Title == "" || meta.Version == nil || *meta.Version < 0 {
		return nil, errors.New("Grafana returned a different or incomplete dashboard model.")
	}
	return &GetDashboardOut{UID: meta.UID, Title: meta.Title, Version: strconv.FormatInt(*meta.Version, 10), ModelJSON: string(w.Dashboard)}, nil
}
func (c *client) listAnnotations(ctx plugin.Ctx, in *ListAnnotationsIn) (*ListAnnotationsOut, error) {
	f := in.Filter
	limit := 100
	if f.Limit != nil {
		limit = *f.Limit
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("Annotation result limit must be 1–100.")
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}, "type": {"annotation"}}
	var start, end int64
	var e error
	if f.From != nil {
		start, e = epoch(*f.From, true)
		if e != nil {
			return nil, e
		}
		q.Set("from", *f.From)
	}
	if f.To != nil {
		end, e = epoch(*f.To, true)
		if e != nil {
			return nil, e
		}
		q.Set("to", *f.To)
	}
	if f.From != nil && f.To != nil && end < start {
		return nil, errors.New("Annotation end time must not precede start time.")
	}
	if f.DashboardUID != nil {
		if e := validUID(*f.DashboardUID); e != nil {
			return nil, e
		}
		q.Set("dashboardUID", *f.DashboardUID)
	}
	if f.PanelID != nil {
		if f.DashboardUID == nil {
			return nil, errors.New("A panel filter requires a dashboard UID.")
		}
		if _, e := nonnegative(*f.PanelID); e != nil {
			return nil, e
		}
		q.Set("panelId", *f.PanelID)
	}
	if e := checkedTags(f.Tags); e != nil {
		return nil, e
	}
	for _, tag := range f.Tags {
		q.Add("tags", tag)
	}
	var rows []annotationWire
	if _, e = c.json(ctx, "GET", "/api/annotations", q, nil, 200, &rows); e != nil {
		return nil, e
	}
	if rows == nil || len(rows) > limit {
		return nil, errors.New("Grafana returned an invalid annotation list.")
	}
	out := &ListAnnotationsOut{Annotations: make([]schema.Annotation, 0, len(rows)), LimitReached: len(rows) == limit}
	for _, row := range rows {
		v, e := row.output()
		if e != nil {
			return nil, e
		}
		out.Annotations = append(out.Annotations, v)
	}
	return out, nil
}
func (c *client) createAnnotation(ctx plugin.Ctx, in *CreateAnnotationIn) (*CreateAnnotationOut, error) {
	if strings.TrimSpace(in.Text) == "" || !validBody(in.Text, 1<<20) {
		return nil, errors.New("Annotation text must contain 1 byte to 1 MiB of valid UTF-8 text.")
	}
	time, e := epoch(in.Time, false)
	if e != nil {
		return nil, e
	}
	body := map[string]any{"text": in.Text, "time": time}
	o := in.Options
	if o.TimeEnd != nil {
		end, e := epoch(*o.TimeEnd, false)
		if e != nil {
			return nil, e
		}
		if end < time {
			return nil, errors.New("Region end must not precede annotation time.")
		}
		body["timeEnd"] = end
	}
	if o.DashboardUID != nil {
		if e := validUID(*o.DashboardUID); e != nil {
			return nil, e
		}
		body["dashboardUID"] = *o.DashboardUID
	}
	if o.PanelID != nil {
		if o.DashboardUID == nil {
			return nil, errors.New("A panel annotation requires a dashboard UID.")
		}
		id, e := nonnegative(*o.PanelID)
		if e != nil {
			return nil, e
		}
		body["panelId"] = id
	}
	if e := checkedTags(o.Tags); e != nil {
		return nil, e
	}
	if o.Tags != nil {
		body["tags"] = o.Tags
	}
	var w struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if _, e = c.json(ctx, "POST", "/api/annotations", nil, body, 200, &w); e != nil {
		return nil, e
	}
	id, e := idString(w.ID)
	if e != nil {
		return nil, e
	}
	if w.Message != "Annotation added" {
		return nil, errors.New("Grafana returned an unexpected annotation receipt.")
	}
	return &CreateAnnotationOut{ID: id}, nil
}
