package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/confluence/schema"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

const v2 = "/wiki/api/v2"

func (a *api) health(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		AccountID string `json:"accountId"`
		Type      string
	}
	_, e := a.request(ctx, "GET", "/wiki/rest/api/user/current", nil, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.AccountID == "" || r.Type == "anonymous" {
		return &HealthCheckOut{Message: "Confluence returned no authenticated account"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Confluence identity access verified; space and page permissions are checked by each operation"}, nil
}
func representation(s string) (string, error) {
	if s == "" {
		return "storage", nil
	}
	if s != "storage" && s != "atlas_doc_format" {
		return "", fmt.Errorf("representation must be storage or atlas_doc_format")
	}
	return s, nil
}

var nextLink = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

func (a *api) cursor(path string, h http.Header, bodyNext string) (string, error) {
	next := bodyNext
	if next == "" {
		if match := nextLink.FindStringSubmatch(h.Get("Link")); len(match) == 2 {
			next = match[1]
		}
	}
	if next == "" {
		return "", nil
	}
	base, e := a.base()
	if e != nil {
		return "", e
	}
	u, e := url.Parse(next)
	if e != nil || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("Confluence returned an invalid pagination link")
	}
	origin, _ := url.Parse(base)
	if u.IsAbs() && (u.Scheme != origin.Scheme || u.Host != origin.Host) {
		return "", fmt.Errorf("Confluence pagination link points outside the configured service")
	}
	if u.Host != "" && u.Host != origin.Host {
		return "", fmt.Errorf("Confluence pagination link points outside the configured service")
	}
	if u.Path != path && u.Path != origin.Path+path {
		return "", fmt.Errorf("Confluence pagination link changed the resource path")
	}
	values, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(values["cursor"]) != 1 || values.Get("cursor") == "" {
		return "", fmt.Errorf("Confluence returned an incomplete pagination cursor")
	}
	return values.Get("cursor"), nil
}
func listQuery(n int, cursor string) (url.Values, error) {
	limit, e := pageSize(n)
	if e != nil {
		return nil, e
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return q, nil
}
func (a *api) spaces(ctx context.Context, in *ListSpacesIn) (*ListSpacesOut, error) {
	q, e := listQuery(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	if in.Key != "" {
		q.Set("keys", in.Key)
	}
	var r struct {
		Results []schema.Space
		Links   struct{ Next string } `json:"_links"`
	}
	h, e := a.request(ctx, "GET", v2+"/spaces", q, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Results == nil {
		return nil, fmt.Errorf("Confluence returned an incomplete space list")
	}
	next, e := a.cursor(v2+"/spaces", h, r.Links.Next)
	return &ListSpacesOut{Spaces: r.Results, NextCursor: next}, e
}
func (a *api) getSpace(ctx context.Context, in *GetSpaceIn) (*GetSpaceOut, error) {
	id, e := numericID(in.SpaceID, "space_id")
	if e != nil {
		return nil, e
	}
	var r schema.Space
	_, e = a.request(ctx, "GET", v2+"/spaces/"+id, nil, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Confluence returned an incomplete space")
	}
	return &GetSpaceOut{Space: r}, nil
}

type rawPage struct {
	ID            string
	SpaceID       string `json:"spaceId"`
	ParentID      string `json:"parentId"`
	Title, Status string
	CreatedAt     string `json:"createdAt"`
	Version       struct{ Number int }
	Body          map[string]struct{ Value, Representation string }
	Links         struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

func (r rawPage) typed(rep string) schema.Page {
	return schema.Page{ID: r.ID, SpaceID: r.SpaceID, ParentID: r.ParentID, Title: r.Title, Status: r.Status, CreatedAt: r.CreatedAt, Version: r.Version.Number, Body: r.Body[rep].Value, Representation: rep, WebPath: r.Links.WebUI}
}
func (a *api) pages(ctx context.Context, in *ListPagesIn) (*ListPagesOut, error) {
	q, e := listQuery(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	rep, e := representation(in.Representation)
	if e != nil {
		return nil, e
	}
	q.Set("body-format", rep)
	q.Set("status", "current")
	if in.SpaceID != "" {
		if _, e := numericID(in.SpaceID, "space_id"); e != nil {
			return nil, e
		}
		q.Set("space-id", in.SpaceID)
	}
	if in.Title != "" {
		q.Set("title", in.Title)
	}
	var r struct {
		Results []rawPage
		Links   struct{ Next string } `json:"_links"`
	}
	h, e := a.request(ctx, "GET", v2+"/pages", q, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Results == nil {
		return nil, fmt.Errorf("Confluence returned an incomplete page list")
	}
	next, e := a.cursor(v2+"/pages", h, r.Links.Next)
	if e != nil {
		return nil, e
	}
	out := &ListPagesOut{Pages: []schema.Page{}, NextCursor: next}
	for _, v := range r.Results {
		out.Pages = append(out.Pages, v.typed(rep))
	}
	return out, nil
}
func (a *api) getPage(ctx context.Context, in *GetPageIn) (*GetPageOut, error) {
	id, e := numericID(in.PageID, "page_id")
	if e != nil {
		return nil, e
	}
	rep, e := representation(in.Representation)
	if e != nil {
		return nil, e
	}
	var r rawPage
	_, e = a.request(ctx, "GET", v2+"/pages/"+id, url.Values{"body-format": {rep}}, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.ID == "" || r.Version.Number < 1 {
		return nil, fmt.Errorf("Confluence returned an incomplete page or version")
	}
	return &GetPageOut{Page: r.typed(rep)}, nil
}
func pageBody(title, content, rep string) (map[string]any, error) {
	if content == "" {
		return nil, fmt.Errorf("body is required; use an explicit empty storage paragraph to clear content")
	}
	if e := required(title, "title"); e != nil {
		return nil, e
	}
	representation, e := representation(rep)
	if e != nil {
		return nil, e
	}
	if representation == "atlas_doc_format" {
		var adf struct {
			Type    string
			Version int
		}
		if json.Unmarshal([]byte(content), &adf) != nil || adf.Type != "doc" || adf.Version != 1 {
			return nil, fmt.Errorf("atlas_doc_format body must be a JSON document with type doc and version 1")
		}
	}
	return map[string]any{"title": title, "status": "current", "body": map[string]string{"representation": representation, "value": content}}, nil
}
func (a *api) createPage(ctx context.Context, in *CreatePageIn) (*CreatePageOut, error) {
	id, e := numericID(in.SpaceID, "space_id")
	if e != nil {
		return nil, e
	}
	payload, e := pageBody(in.Title, in.Body, in.Representation)
	if e != nil {
		return nil, e
	}
	payload["spaceId"] = id
	if in.ParentID != "" {
		if _, e := numericID(in.ParentID, "parent_id"); e != nil {
			return nil, e
		}
		payload["parentId"] = in.ParentID
	}
	var r rawPage
	_, e = a.request(ctx, "POST", v2+"/pages", nil, payload, &r)
	if e != nil {
		return nil, e
	}
	if r.ID == "" || r.Version.Number < 1 {
		return nil, fmt.Errorf("Confluence did not confirm page creation; inspect Confluence before retrying")
	}
	rep, _ := representation(in.Representation)
	return &CreatePageOut{Page: r.typed(rep)}, nil
}
func (a *api) updatePage(ctx context.Context, in *UpdatePageIn) (*UpdatePageOut, error) {
	if in.ExpectedVersion < 1 || in.ExpectedVersion >= 2147483647 {
		return nil, fmt.Errorf("expected_version must be a positive current version below 2147483647")
	}
	payload, e := pageBody(in.Title, in.Body, in.Representation)
	if e != nil {
		return nil, e
	}
	current, e := a.getPage(ctx, &GetPageIn{PageID: in.PageID, Representation: in.Representation})
	if e != nil {
		return nil, e
	}
	if current.Page.Status != "current" {
		return nil, fmt.Errorf("only current published pages can be updated")
	}
	if current.Page.Version != in.ExpectedVersion {
		return nil, fmt.Errorf("Confluence version conflict: expected %d, current %d; fetch and reconcile before retrying", in.ExpectedVersion, current.Page.Version)
	}
	payload["id"] = in.PageID
	payload["version"] = map[string]any{"number": in.ExpectedVersion + 1, "message": in.VersionMessage}
	var r rawPage
	_, e = a.request(ctx, "PUT", v2+"/pages/"+in.PageID, nil, payload, &r)
	if e != nil {
		return nil, e
	}
	if r.ID == "" || r.Version.Number != in.ExpectedVersion+1 {
		return nil, fmt.Errorf("Confluence returned an unexpected updated version; inspect the page before retrying")
	}
	rep, _ := representation(in.Representation)
	return &UpdatePageOut{Page: r.typed(rep)}, nil
}
