package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func sitePath(id string) (string, error) { s, e := segment(id); return "/v1.0/sites/" + s, e }
func listPath(site, id string) (string, error) {
	p, e := sitePath(site)
	if e != nil {
		return "", e
	}
	s, e := segment(id)
	return p + "/lists/" + s, e
}
func (c *client) collection(ctx context.Context, path, link string, q url.Values) ([]json.RawMessage, string, error) {
	target, e := pageURL(path, link, q)
	if e != nil {
		return nil, "", e
	}
	var p struct {
		Value []json.RawMessage `json:"value"`
		Next  string            `json:"@odata.nextLink"`
	}
	if e = c.json(ctx, "GET", target, nil, nil, &p, 200); e != nil {
		return nil, "", e
	}
	if p.Value == nil {
		return nil, "", errors.New("Microsoft Graph response is missing the collection.")
	}
	next, e := checkedNext(path, target, p.Next)
	return p.Value, next, e
}

type upstreamSite struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	WebURL      string `json:"webUrl"`
}

func (r upstreamSite) typed() (Site, error) {
	if _, e := segment(r.ID); e != nil || r.WebURL == "" {
		return Site{}, errors.New("Microsoft Graph returned incomplete site metadata.")
	}
	return Site{ID: r.ID, Name: r.Name, DisplayName: r.DisplayName, WebURL: r.WebURL}, nil
}
func (c *client) searchSites(ctx context.Context, in *SearchSitesIn) (*SearchSitesOut, error) {
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 1024 {
		return nil, errors.New("Provide a site search query of 1 to 1024 bytes.")
	}
	rows, next, e := c.collection(ctx, "/v1.0/sites", in.NextLink, url.Values{"search": {in.Query}})
	if e != nil {
		return nil, e
	}
	out := &SearchSitesOut{Sites: []Site{}, NextLink: next}
	for _, b := range rows {
		var r upstreamSite
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		s, e := r.typed()
		if e != nil {
			return nil, e
		}
		out.Sites = append(out.Sites, s)
	}
	return out, nil
}
func (c *client) getSite(ctx context.Context, in *GetSiteIn) (*GetSiteOut, error) {
	p, e := sitePath(in.SiteID)
	if e != nil {
		return nil, e
	}
	var r upstreamSite
	if e = c.json(ctx, "GET", graphOrigin+p, nil, nil, &r, 200); e != nil {
		return nil, e
	}
	s, e := r.typed()
	if e == nil && strings.Contains(in.SiteID, ",") && s.ID != in.SiteID {
		return nil, errors.New("Microsoft Graph returned a different site ID.")
	}
	return &GetSiteOut{Site: s}, e
}
func (c *client) listDrives(ctx context.Context, in *ListDrivesIn) (*ListDrivesOut, error) {
	p, e := sitePath(in.SiteID)
	if e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, p+"/drives", in.NextLink, nil)
	if e != nil {
		return nil, e
	}
	out := &ListDrivesOut{Drives: []Drive{}, NextLink: next}
	for _, b := range rows {
		var r struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Type   string `json:"driveType"`
			WebURL string `json:"webUrl"`
		}
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if _, e = segment(r.ID); e != nil || r.Type == "" {
			return nil, errors.New("Microsoft Graph returned incomplete drive metadata.")
		}
		out.Drives = append(out.Drives, Drive{ID: r.ID, Name: r.Name, DriveType: r.Type, WebURL: r.WebURL})
	}
	return out, nil
}
func (c *client) listLists(ctx context.Context, in *ListListsIn) (*ListListsOut, error) {
	p, e := sitePath(in.SiteID)
	if e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, p+"/lists", in.NextLink, nil)
	if e != nil {
		return nil, e
	}
	out := &ListListsOut{Lists: []SPList{}, NextLink: next}
	for _, b := range rows {
		var r upstreamSite
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if _, e = segment(r.ID); e != nil || r.Name == "" {
			return nil, errors.New("Microsoft Graph returned incomplete list metadata.")
		}
		out.Lists = append(out.Lists, SPList{ID: r.ID, Name: r.Name, DisplayName: r.DisplayName, WebURL: r.WebURL})
	}
	return out, nil
}
func (c *client) listColumns(ctx context.Context, in *ListColumnsIn) (*ListColumnsOut, error) {
	p, e := listPath(in.SiteID, in.ListID)
	if e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, p+"/columns", in.NextLink, nil)
	if e != nil {
		return nil, e
	}
	out := &ListColumnsOut{Columns: []Column{}, NextLink: next}
	for _, b := range rows {
		var r struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Required    bool   `json:"required"`
			ReadOnly    bool   `json:"readOnly"`
		}
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if _, e = segment(r.ID); e != nil || r.Name == "" {
			return nil, errors.New("Microsoft Graph returned incomplete column metadata.")
		}
		var facets map[string]json.RawMessage
		if e = decodeJSON(b, &facets); e != nil {
			return nil, e
		}
		kind := "unknown"
		for _, k := range []string{"text", "number", "boolean", "dateTime", "choice", "currency", "lookup", "personOrGroup", "calculated", "hyperlinkOrPicture", "term"} {
			if v := facets[k]; len(v) > 0 && string(v) != "null" {
				kind = k
				break
			}
		}
		out.Columns = append(out.Columns, Column{ID: r.ID, Name: r.Name, DisplayName: r.DisplayName, Kind: kind, Required: r.Required, ReadOnly: r.ReadOnly})
	}
	return out, nil
}

type upstreamListItem struct {
	ID     string         `json:"id"`
	ETag   string         `json:"eTag"`
	WebURL string         `json:"webUrl"`
	Fields map[string]any `json:"fields"`
}

var itemID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func (r upstreamListItem) typed() (ListItem, error) {
	if !itemID.MatchString(r.ID) || r.Fields == nil {
		return ListItem{}, errors.New("Microsoft Graph returned a list item without its ID or expanded fields.")
	}
	return ListItem{ID: r.ID, Etag: r.ETag, WebURL: r.WebURL, Fields: r.Fields}, nil
}
func (c *client) listItems(ctx context.Context, in *ListItemsIn) (*ListItemsOut, error) {
	p, e := listPath(in.SiteID, in.ListID)
	if e != nil {
		return nil, e
	}
	rows, next, e := c.collection(ctx, p+"/items", in.NextLink, url.Values{"$expand": {"fields"}, "$top": {"100"}})
	if e != nil {
		return nil, e
	}
	out := &ListItemsOut{Items: []ListItem{}, NextLink: next}
	for _, b := range rows {
		var r upstreamListItem
		if e = decodeJSON(b, &r); e != nil {
			return nil, e
		}
		item, e := r.typed()
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,254}$`)

func validateFields(fields map[string]any) error {
	if len(fields) == 0 || len(fields) > 100 {
		return errors.New("Provide 1 to 100 scalar column values using internal column names.")
	}
	for k, v := range fields {
		if !fieldName.MatchString(k) {
			return errors.New("Invalid internal column name.")
		}
		switch v := v.(type) {
		case nil, bool, int, int32, int64, uint, uint32, uint64:
		case string:
			if len(v) > 65536 {
				return errors.New("A column value exceeds 64 KiB.")
			}
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("Column numbers must be finite.")
			}
		case json.Number:
			if n, e := v.Float64(); e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return errors.New("Invalid column number.")
			}
		default:
			return errors.New("This version writes scalar string, number, boolean or null column values only.")
		}
	}
	return nil
}
func (c *client) createItem(ctx context.Context, in *CreateItemIn) (*CreateItemOut, error) {
	p, e := listPath(in.SiteID, in.ListID)
	if e != nil {
		return nil, e
	}
	if e = validateFields(in.Fields); e != nil {
		return nil, e
	}
	var r upstreamListItem
	if e = c.json(ctx, "POST", graphOrigin+p+"/items", map[string]any{"fields": in.Fields}, nil, &r, 201); e != nil {
		return nil, e
	}
	item, e := r.typed()
	if e != nil {
		return nil, errors.New("Microsoft Graph returned no valid creation receipt. Inspect the list before retrying.")
	}
	return &CreateItemOut{Item: item}, nil
}
func (c *client) updateFields(ctx context.Context, in *UpdateFieldsIn) (*UpdateFieldsOut, error) {
	p, e := listPath(in.SiteID, in.ListID)
	if e != nil {
		return nil, e
	}
	if !itemID.MatchString(in.ItemID) {
		return nil, errors.New("Provide a positive numeric SharePoint list item ID.")
	}
	if e = validateFields(in.Fields); e != nil {
		return nil, e
	}
	if !safeHeader(in.IfMatch) {
		return nil, errors.New("Invalid ETag.")
	}
	h := http.Header{}
	if in.IfMatch != "" {
		h.Set("If-Match", in.IfMatch)
	}
	var fields map[string]any
	if e = c.json(ctx, "PATCH", graphOrigin+p+"/items/"+in.ItemID+"/fields", in.Fields, h, &fields, 200); e != nil {
		return nil, e
	}
	if len(fields) == 0 {
		return nil, errors.New("Microsoft Graph returned no field update receipt. Inspect the item before retrying.")
	}
	return &UpdateFieldsOut{ItemID: in.ItemID, Fields: fields}, nil
}
