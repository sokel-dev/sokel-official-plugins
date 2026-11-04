package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/miniflux/schema"
	"net/url"
	"strconv"
	"strings"
)

func id(s string) (int64, error) {
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return 0, errors.New("IDs must be canonical positive decimal strings within signed 64-bit range")
	}
	return n, nil
}
func sid(n int64) (string, error) {
	if n <= 0 {
		return "", errors.New("service returned an invalid ID")
	}
	return strconv.FormatInt(n, 10), nil
}

type wireCategory struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

func (w wireCategory) project() (schema.Category, error) {
	s, e := sid(w.ID)
	return schema.Category{ID: s, Title: w.Title}, e
}

type wireFeed struct {
	ID                int64        `json:"id"`
	Title             string       `json:"title"`
	FeedURL           string       `json:"feed_url"`
	SiteURL           string       `json:"site_url"`
	Category          wireCategory `json:"category"`
	CheckedAt         *string      `json:"checked_at"`
	Disabled          bool         `json:"disabled"`
	ParsingErrorCount int64        `json:"parsing_error_count"`
}
type wireEntry struct {
	ID          int64  `json:"id"`
	FeedID      int64  `json:"feed_id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Author      string `json:"author"`
	Content     string `json:"content"`
	Status      string `json:"status"`
	Starred     bool   `json:"starred"`
	PublishedAt string `json:"published_at"`
}

func (w wireEntry) project() (schema.Entry, error) {
	s, e := sid(w.ID)
	if e != nil {
		return schema.Entry{}, e
	}
	f, e := sid(w.FeedID)
	return schema.Entry{ID: s, FeedID: f, Title: w.Title, URL: w.URL, Author: w.Author, Content: w.Content, Status: w.Status, Starred: w.Starred, PublishedAt: w.PublishedAt}, e
}
func (c *client) getCurrentUser(ctx context.Context, _ *GetCurrentUserIn) (*GetCurrentUserOut, error) {
	var w struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if e := c.json(ctx, "GET", "/v1/me", nil, nil, &w, 200); e != nil {
		return nil, e
	}
	s, e := sid(w.ID)
	if e != nil {
		return nil, e
	}
	return &GetCurrentUserOut{UserID: s, Username: w.Username}, nil
}
func (c *client) listCategories(ctx context.Context, _ *ListCategoriesIn) (*ListCategoriesOut, error) {
	var ws []wireCategory
	if e := c.json(ctx, "GET", "/v1/categories", nil, nil, &ws, 200); e != nil {
		return nil, e
	}
	out := &ListCategoriesOut{Categories: []schema.Category{}}
	for _, w := range ws {
		v, e := w.project()
		if e != nil {
			return nil, e
		}
		out.Categories = append(out.Categories, v)
	}
	return out, nil
}
func (c *client) createCategory(ctx context.Context, in *CreateCategoryIn) (*CreateCategoryOut, error) {
	if strings.TrimSpace(in.Title) == "" || len(in.Title) > 1024 {
		return nil, errors.New("category title must contain 1 to 1024 bytes")
	}
	var w wireCategory
	if e := c.json(ctx, "POST", "/v1/categories", nil, map[string]string{"title": in.Title}, &w, 201); e != nil {
		return nil, e
	}
	v, e := w.project()
	if e != nil {
		return nil, e
	}
	return &CreateCategoryOut{Category: v}, nil
}
func (c *client) listFeeds(ctx context.Context, _ *ListFeedsIn) (*ListFeedsOut, error) {
	var ws []wireFeed
	if e := c.json(ctx, "GET", "/v1/feeds", nil, nil, &ws, 200); e != nil {
		return nil, e
	}
	out := &ListFeedsOut{Feeds: []schema.Feed{}}
	for _, w := range ws {
		s, e := sid(w.ID)
		if e != nil {
			return nil, e
		}
		cat, e := w.Category.project()
		if e != nil {
			return nil, e
		}
		out.Feeds = append(out.Feeds, schema.Feed{ID: s, Title: w.Title, FeedURL: w.FeedURL, SiteURL: w.SiteURL, Category: cat, CheckedAt: w.CheckedAt, Disabled: w.Disabled, ParsingErrorCount: w.ParsingErrorCount})
	}
	return out, nil
}
func (c *client) createFeed(ctx context.Context, in *CreateFeedIn) (*CreateFeedOut, error) {
	u, e := url.Parse(in.FeedURL)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || len(in.FeedURL) > 8192 {
		return nil, errors.New("feed URL must be HTTP(S) without credentials or fragment")
	}
	cat, e := id(in.CategoryID)
	if e != nil {
		return nil, e
	}
	var w struct {
		FeedID int64 `json:"feed_id"`
	}
	if e = c.json(ctx, "POST", "/v1/feeds", nil, map[string]any{"feed_url": in.FeedURL, "category_id": cat}, &w, 201); e != nil {
		return nil, e
	}
	s, e := sid(w.FeedID)
	if e != nil {
		return nil, e
	}
	return &CreateFeedOut{FeedID: s}, nil
}
func (c *client) refreshFeed(ctx context.Context, in *RefreshFeedIn) (*RefreshFeedOut, error) {
	if _, e := id(in.FeedID); e != nil {
		return nil, e
	}
	if e := c.json(ctx, "PUT", "/v1/feeds/"+in.FeedID+"/refresh", nil, nil, nil, 204); e != nil {
		return nil, e
	}
	return &RefreshFeedOut{Accepted: true}, nil
}
func validStatus(s string) bool { return s == "read" || s == "unread" || s == "removed" }
func (c *client) listEntries(ctx context.Context, in *ListEntriesIn) (*ListEntriesOut, error) {
	o := in.Options
	n := 100
	if o.Limit != nil {
		n = *o.Limit
	}
	if n < 1 || n > 1000 {
		return nil, errors.New("page size must be between 1 and 1000")
	}
	q := url.Values{"limit": {strconv.Itoa(n)}, "order": {"id"}, "direction": {"asc"}}
	var after int64
	if o.AfterID != nil {
		v, e := id(*o.AfterID)
		if e != nil {
			return nil, e
		}
		after = v
		q.Set("after_entry_id", *o.AfterID)
	}
	if o.CategoryID != nil {
		if _, e := id(*o.CategoryID); e != nil {
			return nil, e
		}
		q.Set("category_id", *o.CategoryID)
	}
	if o.Status != nil {
		if !validStatus(*o.Status) {
			return nil, errors.New("status must be read, unread or removed")
		}
		q.Set("status", *o.Status)
	}
	if o.Starred != nil {
		q.Set("starred", strconv.FormatBool(*o.Starred))
	}
	if o.Search != nil {
		if len(*o.Search) > 4096 {
			return nil, errors.New("search text exceeds 4096 bytes")
		}
		q.Set("search", *o.Search)
	}
	var w struct {
		Entries *[]wireEntry `json:"entries"`
	}
	if e := c.json(ctx, "GET", "/v1/entries", q, nil, &w, 200); e != nil {
		return nil, e
	}
	if w.Entries == nil || len(*w.Entries) > n {
		return nil, errors.New("service returned an invalid entry page")
	}
	out := &ListEntriesOut{Entries: []schema.Entry{}, MayHaveMore: len(*w.Entries) == n}
	prev := after
	for _, item := range *w.Entries {
		if item.ID <= prev {
			return nil, errors.New("service did not return entries in ascending cursor order")
		}
		v, e := item.project()
		if e != nil {
			return nil, e
		}
		out.Entries = append(out.Entries, v)
		prev = item.ID
		out.NextAfterID = v.ID
	}
	return out, nil
}
func (c *client) getEntry(ctx context.Context, in *GetEntryIn) (*GetEntryOut, error) {
	n, e := id(in.EntryID)
	if e != nil {
		return nil, e
	}
	var w wireEntry
	if e = c.json(ctx, "GET", "/v1/entries/"+in.EntryID, nil, nil, &w, 200); e != nil {
		return nil, e
	}
	if w.ID != n {
		return nil, errors.New("service returned an unexpected entry ID")
	}
	v, e := w.project()
	if e != nil {
		return nil, e
	}
	return &GetEntryOut{Entry: v}, nil
}
func (c *client) updateEntries(ctx context.Context, in *UpdateEntriesIn) (*UpdateEntriesOut, error) {
	if len(in.EntryIDs) < 1 || len(in.EntryIDs) > 1000 {
		return nil, errors.New("provide between 1 and 1000 entry IDs")
	}
	if in.Changes.Status == nil && in.Changes.Starred == nil {
		return nil, errors.New("provide status, starred, or both")
	}
	if in.Changes.Status != nil && !validStatus(*in.Changes.Status) {
		return nil, errors.New("status must be read, unread or removed")
	}
	ids := make([]int64, 0, len(in.EntryIDs))
	seen := map[int64]bool{}
	for _, s := range in.EntryIDs {
		n, e := id(s)
		if e != nil {
			return nil, e
		}
		if seen[n] {
			return nil, errors.New("entry IDs must not repeat")
		}
		seen[n] = true
		ids = append(ids, n)
	}
	body := struct {
		EntryIDs []int64 `json:"entry_ids"`
		Status   *string `json:"status,omitempty"`
		Starred  *bool   `json:"starred,omitempty"`
	}{ids, in.Changes.Status, in.Changes.Starred}
	if e := c.json(ctx, "PUT", "/v1/entries", nil, body, nil, 204); e != nil {
		return nil, fmt.Errorf("update entries: %w", e)
	}
	return &UpdateEntriesOut{Accepted: true}, nil
}
