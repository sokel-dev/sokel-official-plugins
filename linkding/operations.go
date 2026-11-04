package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/linkding/schema"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func id(s string) (int64, error) {
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return 0, errors.New("IDs must be canonical positive signed 64-bit decimal strings")
	}
	return n, nil
}
func bookmarkPath(s string) (string, error) {
	if _, e := id(s); e != nil {
		return "", e
	}
	return "/api/bookmarks/" + s + "/", nil
}
func bookmarkURL(s string) error {
	u, e := url.Parse(s)
	if e != nil || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.IndexFunc(s, unicode.IsControl) >= 0 || len(s) > 8192 {
		return errors.New("bookmark URL must be HTTP(S), without embedded credentials, and at most 8192 bytes")
	}
	return nil
}
func bookmarkValue(b json.RawMessage, want string) (schema.Bookmark, error) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(b, &keys) != nil || keys == nil {
		return schema.Bookmark{}, errors.New("linkding returned an invalid bookmark")
	}
	for _, k := range []string{"id", "url", "is_archived", "unread", "shared", "tag_names", "date_added", "date_modified"} {
		v, ok := keys[k]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return schema.Bookmark{}, errors.New("linkding returned incomplete bookmark metadata")
		}
	}
	var w struct {
		ID           json.Number `json:"id"`
		URL          string      `json:"url"`
		Title        string      `json:"title"`
		Description  string      `json:"description"`
		Notes        string      `json:"notes"`
		IsArchived   bool        `json:"is_archived"`
		Unread       bool        `json:"unread"`
		Shared       bool        `json:"shared"`
		TagNames     []string    `json:"tag_names"`
		DateAdded    string      `json:"date_added"`
		DateModified string      `json:"date_modified"`
	}
	if json.Unmarshal(b, &w) != nil {
		return schema.Bookmark{}, errors.New("linkding returned invalid bookmark fields")
	}
	if _, e := id(w.ID.String()); e != nil {
		return schema.Bookmark{}, errors.New("linkding returned an invalid bookmark ID")
	}
	if want != "" && w.ID.String() != want {
		return schema.Bookmark{}, errors.New("linkding returned an unexpected bookmark ID")
	}
	if w.URL == "" || w.DateAdded == "" || w.DateModified == "" {
		return schema.Bookmark{}, errors.New("linkding returned empty bookmark metadata")
	}
	return schema.Bookmark{ID: w.ID.String(), URL: w.URL, Title: w.Title, Description: w.Description, Notes: w.Notes, IsArchived: w.IsArchived, Unread: w.Unread, Shared: w.Shared, TagNames: w.TagNames, DateAdded: w.DateAdded, DateModified: w.DateModified}, nil
}
func pageParams(limit, offset *int) (url.Values, int, int, error) {
	l, o := 100, 0
	if limit != nil {
		l = *limit
	}
	if offset != nil {
		o = *offset
	}
	if l < 1 || l > 100 || o < 0 || o > 1000000 {
		return nil, 0, 0, errors.New("page size must be 1–100 and offset 0–1000000")
	}
	return url.Values{"limit": {strconv.Itoa(l)}, "offset": {strconv.Itoa(o)}}, l, o, nil
}

type pageEnvelope struct {
	Count   json.Number       `json:"count"`
	Next    json.RawMessage   `json:"next"`
	Results []json.RawMessage `json:"results"`
}

func (c *client) page(ctx context.Context, path string, q url.Values, limit, offset int) ([]json.RawMessage, schema.PageInfo, error) {
	var w pageEnvelope
	if e := c.json(ctx, "GET", path, q, nil, &w, 200); e != nil {
		return nil, schema.PageInfo{}, e
	}
	count, e := strconv.ParseInt(w.Count.String(), 10, 64)
	if e != nil || count < 0 || strconv.FormatInt(count, 10) != w.Count.String() || w.Results == nil || len(w.Next) == 0 || len(w.Results) > limit {
		return nil, schema.PageInfo{}, errors.New("linkding returned invalid pagination metadata")
	}
	if len(w.Results) > 0 && int64(offset+len(w.Results)) > count {
		return nil, schema.PageInfo{}, errors.New("linkding returned an inconsistent total count")
	}
	info := schema.PageInfo{Total: w.Count.String()}
	nextRaw := bytes.TrimSpace(w.Next)
	if !bytes.Equal(nextRaw, []byte("null")) {
		var next string
		if json.Unmarshal(nextRaw, &next) != nil || next == "" || len(w.Results) == 0 || offset+limit > 1000000 {
			return nil, info, errors.New("linkding returned an invalid continuation")
		}
		u, e := url.Parse(next)
		endpoint, _ := c.endpoint(path, nil)
		expected, _ := url.Parse(endpoint)
		if e != nil || u == nil || u.User != nil || u.Fragment != "" || u.Scheme != expected.Scheme || u.Host != expected.Host || u.EscapedPath() != expected.EscapedPath() || u.ForceQuery {
			return nil, info, errors.New("linkding continuation is outside the configured endpoint")
		}
		expectedQuery := url.Values{}
		for k, vs := range q {
			expectedQuery[k] = append([]string{}, vs...)
		}
		n := offset + limit
		expectedQuery.Set("offset", strconv.Itoa(n))
		if u.Query().Encode() != expectedQuery.Encode() || count <= int64(n) {
			return nil, info, errors.New("linkding returned a non-advancing or inconsistent continuation")
		}
		info.HasMore = true
		info.NextOffset = &n
	} else if count > int64(offset+limit) {
		return nil, info, errors.New("linkding omitted a required continuation")
	}
	return w.Results, info, nil
}
func (c *client) healthCheck(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	n := 1
	_, e := c.listTags(ctx, &ListTagsIn{Options: schema.PageOptions{Limit: &n}})
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "linkding accepted the API token and returned its tag list."}, nil
}
func (c *client) listBookmarks(ctx context.Context, in *ListBookmarksIn) (*ListBookmarksOut, error) {
	o := in.Options
	q, l, offset, e := pageParams(o.Limit, o.Offset)
	if e != nil {
		return nil, e
	}
	path := "/api/bookmarks/"
	if o.Archived != nil && *o.Archived {
		path += "archived/"
	}
	if o.Search != nil {
		if len(*o.Search) > 4096 || !utf8.ValidString(*o.Search) || strings.ContainsRune(*o.Search, 0) {
			return nil, errors.New("search text must be valid UTF-8 within 4096 bytes")
		}
		q.Set("q", *o.Search)
	}
	for key, v := range map[string]*string{"modified_since": o.ModifiedSince, "added_since": o.AddedSince} {
		if v != nil {
			if len(*v) > 64 {
				return nil, errors.New("date filters must be RFC3339 timestamps with a timezone")
			}
			if _, e = time.Parse(time.RFC3339Nano, *v); e != nil {
				return nil, errors.New("date filters must be RFC3339 timestamps with a timezone")
			}
			q.Set(key, *v)
		}
	}
	rows, page, e := c.page(ctx, path, q, l, offset)
	if e != nil {
		return nil, e
	}
	out := &ListBookmarksOut{Bookmarks: []schema.Bookmark{}, Page: page}
	seen := map[string]bool{}
	for _, r := range rows {
		v, e := bookmarkValue(r, "")
		if e != nil {
			return nil, e
		}
		if seen[v.ID] {
			return nil, errors.New("linkding returned a repeated bookmark ID")
		}
		seen[v.ID] = true
		out.Bookmarks = append(out.Bookmarks, v)
	}
	return out, nil
}
func (c *client) getBookmark(ctx context.Context, in *GetBookmarkIn) (*GetBookmarkOut, error) {
	p, e := bookmarkPath(in.BookmarkID)
	if e != nil {
		return nil, e
	}
	var raw json.RawMessage
	if e = c.json(ctx, "GET", p, nil, nil, &raw, 200); e != nil {
		return nil, e
	}
	v, e := bookmarkValue(raw, in.BookmarkID)
	if e != nil {
		return nil, e
	}
	return &GetBookmarkOut{Bookmark: v}, nil
}
func validateFields(f schema.BookmarkFields) error {
	for label, v := range map[string]*string{"title": f.Title, "description": f.Description, "notes": f.Notes} {
		max := 256 << 10
		if label == "title" {
			max = 4096
		}
		if v != nil && (len(*v) > max || !utf8.ValidString(*v) || strings.ContainsRune(*v, 0)) {
			return errors.New("title must be valid UTF-8 within 4096 bytes; description and notes within 256 KiB")
		}
	}
	if f.TagNames != nil {
		if len(*f.TagNames) > 100 {
			return errors.New("provide at most 100 tag names")
		}
		seen := map[string]bool{}
		for _, tag := range *f.TagNames {
			if strings.TrimSpace(tag) == "" || len(tag) > 200 || !utf8.ValidString(tag) || strings.IndexFunc(tag, unicode.IsControl) >= 0 || seen[tag] {
				return errors.New("tag names must be nonblank, distinct, and at most 200 UTF-8 bytes")
			}
			seen[tag] = true
		}
	}
	return nil
}
func (c *client) saveBookmark(ctx context.Context, in *SaveBookmarkIn) (*SaveBookmarkOut, error) {
	if e := bookmarkURL(in.URL); e != nil {
		return nil, e
	}
	if e := validateFields(in.Fields); e != nil {
		return nil, e
	}
	body := struct {
		URL string `json:"url"`
		schema.BookmarkFields
	}{in.URL, in.Fields}
	var raw json.RawMessage
	q := url.Values{"disable_scraping": {"true"}, "disable_html_snapshot": {"true"}}
	if e := c.json(ctx, "POST", "/api/bookmarks/", q, body, &raw, 201); e != nil {
		return nil, e
	}
	v, e := bookmarkValue(raw, "")
	if e != nil {
		return nil, e
	}
	return &SaveBookmarkOut{Bookmark: v}, nil
}
func (c *client) updateBookmark(ctx context.Context, in *UpdateBookmarkIn) (*UpdateBookmarkOut, error) {
	p, e := bookmarkPath(in.BookmarkID)
	if e != nil {
		return nil, e
	}
	ch := in.Changes
	f := schema.BookmarkFields{Title: ch.Title, Description: ch.Description, Notes: ch.Notes, IsArchived: ch.IsArchived, Unread: ch.Unread, Shared: ch.Shared, TagNames: ch.TagNames}
	if e = validateFields(f); e != nil {
		return nil, e
	}
	if ch.URL != nil {
		if e = bookmarkURL(*ch.URL); e != nil {
			return nil, e
		}
	}
	b, _ := json.Marshal(ch)
	if string(b) == "{}" {
		return nil, errors.New("provide at least one bookmark change")
	}
	var raw json.RawMessage
	if e = c.json(ctx, "PATCH", p, nil, ch, &raw, 200); e != nil {
		return nil, e
	}
	v, e := bookmarkValue(raw, in.BookmarkID)
	if e != nil {
		return nil, e
	}
	return &UpdateBookmarkOut{Bookmark: v}, nil
}
func (c *client) archiveBookmark(ctx context.Context, in *ArchiveBookmarkIn) (*ArchiveBookmarkOut, error) {
	p, e := bookmarkPath(in.BookmarkID)
	if e != nil {
		return nil, e
	}
	if e = c.json(ctx, "POST", p+"archive/", nil, nil, nil, 204); e != nil {
		return nil, e
	}
	return &ArchiveBookmarkOut{Accepted: true}, nil
}
func (c *client) unarchiveBookmark(ctx context.Context, in *UnarchiveBookmarkIn) (*UnarchiveBookmarkOut, error) {
	p, e := bookmarkPath(in.BookmarkID)
	if e != nil {
		return nil, e
	}
	if e = c.json(ctx, "POST", p+"unarchive/", nil, nil, nil, 204); e != nil {
		return nil, e
	}
	return &UnarchiveBookmarkOut{Accepted: true}, nil
}
func (c *client) deleteBookmark(ctx context.Context, in *DeleteBookmarkIn) (*DeleteBookmarkOut, error) {
	p, e := bookmarkPath(in.BookmarkID)
	if e != nil {
		return nil, e
	}
	if e = c.json(ctx, "DELETE", p, nil, nil, nil, 204); e != nil {
		return nil, e
	}
	return &DeleteBookmarkOut{Deleted: true}, nil
}
func (c *client) listTags(ctx context.Context, in *ListTagsIn) (*ListTagsOut, error) {
	q, l, offset, e := pageParams(in.Options.Limit, in.Options.Offset)
	if e != nil {
		return nil, e
	}
	rows, page, e := c.page(ctx, "/api/tags/", q, l, offset)
	if e != nil {
		return nil, e
	}
	out := &ListTagsOut{Tags: []schema.Tag{}, Page: page}
	seen := map[string]bool{}
	for _, r := range rows {
		var w struct {
			ID        json.Number `json:"id"`
			Name      string      `json:"name"`
			DateAdded string      `json:"date_added"`
		}
		if json.Unmarshal(r, &w) != nil || w.Name == "" || w.DateAdded == "" {
			return nil, errors.New("linkding returned invalid tag metadata")
		}
		if _, e = id(w.ID.String()); e != nil || seen[w.ID.String()] {
			return nil, errors.New("linkding returned invalid or repeated tag IDs")
		}
		seen[w.ID.String()] = true
		out.Tags = append(out.Tags, schema.Tag{ID: w.ID.String(), Name: w.Name, DateAdded: w.DateAdded})
	}
	return out, nil
}
