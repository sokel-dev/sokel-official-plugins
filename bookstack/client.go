package main

import (
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/bookstack/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/url"
	"strconv"
	"strings"
)

type entryWire struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	Description string      `json:"description"`
	Type        string      `json:"type"`
	BookID      *int64      `json:"book_id"`
	ChapterID   *int64      `json:"chapter_id"`
	URL         *string     `json:"url"`
	Pages       []entryWire `json:"pages"`
}
type pageWire struct {
	ID            int64        `json:"id"`
	Name          string       `json:"name"`
	BookID        int64        `json:"book_id"`
	ChapterID     *int64       `json:"chapter_id"`
	HTML          *string      `json:"html"`
	RawHTML       *string      `json:"raw_html"`
	Markdown      *string      `json:"markdown"`
	RevisionCount *int64       `json:"revision_count"`
	Tags          []schema.Tag `json:"tags"`
}
type listWire struct {
	Data  []entryWire `json:"data"`
	Total *int64      `json:"total"`
}

func (w entryWire) output(kind string) (schema.Entry, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Entry{}, e
	}
	if w.Name == "" {
		return schema.Entry{}, errors.New("BookStack returned an entity without a name.")
	}
	if kind == "" {
		kind = w.Type
	}
	switch kind {
	case "bookshelf", "book", "chapter", "page":
	default:
		return schema.Entry{}, errors.New("BookStack returned an unsupported entity type.")
	}
	v := schema.Entry{ID: id, Kind: kind, Name: w.Name, Slug: w.Slug, Description: w.Description, WebURL: w.URL}
	for _, pair := range []struct {
		n   *int64
		out **string
	}{{w.BookID, &v.BookID}, {w.ChapterID, &v.ChapterID}} {
		if pair.n != nil {
			s, e := idString(*pair.n)
			if e != nil {
				return schema.Entry{}, e
			}
			*pair.out = &s
		}
	}
	return v, nil
}
func (w pageWire) output() (schema.Page, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Page{}, e
	}
	book, e := idString(w.BookID)
	if e != nil {
		return schema.Page{}, e
	}
	if w.Name == "" || w.HTML == nil || w.RevisionCount == nil || *w.RevisionCount < 0 {
		return schema.Page{}, errors.New("BookStack returned an incomplete page.")
	}
	v := schema.Page{ID: id, Name: w.Name, BookID: book, HTML: w.HTML, RawHTML: w.RawHTML, Markdown: w.Markdown, RevisionCount: strconv.FormatInt(*w.RevisionCount, 10), Tags: w.Tags}
	if v.Tags == nil {
		v.Tags = []schema.Tag{}
	}
	if w.ChapterID != nil {
		s, e := idString(*w.ChapterID)
		if e != nil {
			return schema.Page{}, e
		}
		v.ChapterID = &s
	}
	return v, nil
}
func entityKind(kind string) (string, error) {
	switch kind {
	case "shelves":
		return "bookshelf", nil
	case "books":
		return "book", nil
	case "chapters":
		return "chapter", nil
	case "pages":
		return "page", nil
	}
	return "", errors.New("Entity type must be shelves, books, chapters or pages.")
}
func listOptions(p schema.Pagination) (url.Values, int, int, error) {
	offset, count := 0, 100
	if p.Offset != nil {
		offset = *p.Offset
	}
	if p.Count != nil {
		count = *p.Count
	}
	if offset < 0 || offset > 10000000 || count < 1 || count > 100 {
		return nil, 0, 0, errors.New("Offset must be 0–10000000 and page size must be 1–100.")
	}
	return url.Values{"offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}, "sort": {"+id"}}, offset, count, nil
}
func validateList(w listWire, offset, count int) (bool, error) {
	if w.Total == nil || *w.Total < 0 || w.Data == nil || len(w.Data) > count || int64(len(w.Data)) > *w.Total {
		return false, errors.New("BookStack returned an invalid list response.")
	}
	more := int64(offset+len(w.Data)) < *w.Total
	if more && len(w.Data) == 0 {
		return false, errors.New("BookStack returned an empty page before the end of the list.")
	}
	return more, nil
}
func (c *client) healthCheck(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
	one := 1
	_, e := c.listEntities(ctx, &ListEntitiesIn{Kind: "books", Pagination: schema.Pagination{Count: &one}})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "BookStack accepted the token and returned a book list. Each content operation checks its own permissions."}, nil
}
func (c *client) listEntities(ctx plugin.Ctx, in *ListEntitiesIn) (*ListEntitiesOut, error) {
	kind, e := entityKind(in.Kind)
	if e != nil {
		return nil, e
	}
	q, offset, count, e := listOptions(in.Pagination)
	if e != nil {
		return nil, e
	}
	var w listWire
	if _, e = c.json(ctx, "GET", "/api/"+in.Kind, q, nil, 200, &w); e != nil {
		return nil, e
	}
	more, e := validateList(w, offset, count)
	if e != nil {
		return nil, e
	}
	out := &ListEntitiesOut{Entities: make([]schema.Entry, 0, len(w.Data)), TotalCount: strconv.FormatInt(*w.Total, 10)}
	if more {
		out.NextOffset = offset + len(w.Data)
		if out.NextOffset > 10000000 {
			return nil, errors.New("BookStack continuation exceeds the supported offset limit.")
		}
	}
	for _, row := range w.Data {
		v, e := row.output(kind)
		if e != nil {
			return nil, e
		}
		out.Entities = append(out.Entities, v)
	}
	return out, nil
}
func (c *client) search(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	if strings.TrimSpace(in.Query) == "" || !safeText(in.Query, 4096) {
		return nil, errors.New("Search query must contain 1–4096 bytes without control characters.")
	}
	page, count := 1, 20
	if in.Pagination.Page != nil {
		page = *in.Pagination.Page
	}
	if in.Pagination.Count != nil {
		count = *in.Pagination.Count
	}
	if page < 1 || page > 100000 || count < 1 || count > 100 {
		return nil, errors.New("Search page must be 1–100000 and page size must be 1–100.")
	}
	q := url.Values{"query": {in.Query}, "page": {strconv.Itoa(page)}, "count": {strconv.Itoa(count)}}
	var w listWire
	if _, e := c.json(ctx, "GET", "/api/search", q, nil, 200, &w); e != nil {
		return nil, e
	}
	more, e := validateList(w, (page-1)*count, count)
	if e != nil {
		return nil, e
	}
	out := &SearchOut{Results: make([]schema.Entry, 0, len(w.Data)), TotalCount: strconv.FormatInt(*w.Total, 10)}
	if more {
		if page == 100000 {
			return nil, errors.New("BookStack continuation exceeds the supported page limit.")
		}
		out.NextPage = page + 1
	}
	for _, row := range w.Data {
		v, e := row.output("")
		if e != nil {
			return nil, e
		}
		out.Results = append(out.Results, v)
	}
	return out, nil
}
func checkedPath(kind, id string) (string, error) {
	if _, e := positiveID(id); e != nil {
		return "", e
	}
	return "/api/" + kind + "/" + id, nil
}
func entries(rows []entryWire, kind string) ([]schema.Entry, error) {
	out := make([]schema.Entry, 0, len(rows))
	for _, row := range rows {
		v, e := row.output(kind)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (c *client) getShelf(ctx plugin.Ctx, in *GetShelfIn) (*GetShelfOut, error) {
	route, e := checkedPath("shelves", in.ShelfID)
	if e != nil {
		return nil, e
	}
	var w struct {
		entryWire
		Books []entryWire `json:"books"`
	}
	if _, e = c.json(ctx, "GET", route, nil, nil, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.entryWire.output("bookshelf")
	if e != nil {
		return nil, e
	}
	if v.ID != in.ShelfID || w.Books == nil {
		return nil, errors.New("BookStack returned a different or incomplete shelf.")
	}
	books, e := entries(w.Books, "book")
	if e != nil {
		return nil, e
	}
	return &GetShelfOut{Shelf: v, Books: books}, nil
}
func (c *client) getBook(ctx plugin.Ctx, in *GetBookIn) (*GetBookOut, error) {
	route, e := checkedPath("books", in.BookID)
	if e != nil {
		return nil, e
	}
	var w struct {
		entryWire
		Contents []entryWire `json:"contents"`
	}
	if _, e = c.json(ctx, "GET", route, nil, nil, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.entryWire.output("book")
	if e != nil {
		return nil, e
	}
	if v.ID != in.BookID || w.Contents == nil {
		return nil, errors.New("BookStack returned a different or incomplete book.")
	}
	flat := []schema.Entry{}
	for _, row := range w.Contents {
		if row.Type != "chapter" && row.Type != "page" {
			return nil, errors.New("BookStack returned an invalid book content type.")
		}
		item, e := row.output("")
		if e != nil {
			return nil, e
		}
		flat = append(flat, item)
		if row.Type == "chapter" {
			children, e := entries(row.Pages, "page")
			if e != nil {
				return nil, e
			}
			flat = append(flat, children...)
		}
	}
	return &GetBookOut{Book: v, Contents: flat}, nil
}
func (c *client) getChapter(ctx plugin.Ctx, in *GetChapterIn) (*GetChapterOut, error) {
	route, e := checkedPath("chapters", in.ChapterID)
	if e != nil {
		return nil, e
	}
	var w entryWire
	if _, e = c.json(ctx, "GET", route, nil, nil, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.output("chapter")
	if e != nil {
		return nil, e
	}
	if v.ID != in.ChapterID || w.Pages == nil {
		return nil, errors.New("BookStack returned a different or incomplete chapter.")
	}
	pages, e := entries(w.Pages, "page")
	if e != nil {
		return nil, e
	}
	return &GetChapterOut{Chapter: v, Pages: pages}, nil
}
func (c *client) getPage(ctx plugin.Ctx, in *GetPageIn) (*GetPageOut, error) {
	route, e := checkedPath("pages", in.PageID)
	if e != nil {
		return nil, e
	}
	var w pageWire
	if _, e = c.json(ctx, "GET", route, nil, nil, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.output()
	if e != nil {
		return nil, e
	}
	if v.ID != in.PageID {
		return nil, errors.New("BookStack returned a different page ID.")
	}
	return &GetPageOut{Page: v}, nil
}
func addPageOptions(body map[string]any, priority *int, tags *[]schema.Tag, changelog *string) error {
	if priority != nil {
		if *priority < -1000000 || *priority > 1000000 {
			return errors.New("Ordering priority must be between -1000000 and 1000000.")
		}
		body["priority"] = *priority
	}
	if tags != nil {
		if len(*tags) > 100 {
			return errors.New("At most 100 tags are supported.")
		}
		values := make([]schema.Tag, 0, len(*tags))
		for _, tag := range *tags {
			if strings.TrimSpace(tag.Name) == "" || !safeText(tag.Name, 255) || !safeText(tag.Value, 255) {
				return errors.New("Tags require a nonempty name and name/value lengths of at most 255 bytes.")
			}
			values = append(values, tag)
		}
		body["tags"] = values
	}
	if changelog != nil {
		if strings.TrimSpace(*changelog) == "" || !safeText(*changelog, 180) {
			return errors.New("Revision note must contain 1–180 bytes without control characters.")
		}
		body["changelog"] = *changelog
	}
	return nil
}
func pageName(s string) bool { return strings.TrimSpace(s) != "" && safeText(s, 255) }
func (c *client) createPage(ctx plugin.Ctx, in *CreatePageIn) (*CreatePageOut, error) {
	if in.ParentType != "book" && in.ParentType != "chapter" {
		return nil, errors.New("Parent type must be book or chapter.")
	}
	id, e := positiveID(in.ParentID)
	if e != nil {
		return nil, e
	}
	if !pageName(in.Name) {
		return nil, errors.New("Page name must contain 1–255 bytes without control characters.")
	}
	if in.Format != "html" && in.Format != "markdown" {
		return nil, errors.New("Content format must be html or markdown.")
	}
	if strings.TrimSpace(in.Content) == "" || !validBody(in.Content, 1<<20) {
		return nil, errors.New("Page content must contain 1 byte to 1 MiB of valid UTF-8 text.")
	}
	body := map[string]any{in.ParentType + "_id": id, "name": in.Name, in.Format: in.Content}
	if e = addPageOptions(body, in.Options.Priority, in.Options.Tags, in.Options.Changelog); e != nil {
		return nil, e
	}
	var w pageWire
	if _, e = c.json(ctx, "POST", "/api/pages", nil, body, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.output()
	if e != nil {
		return nil, e
	}
	return &CreatePageOut{Page: v}, nil
}
func (c *client) updatePage(ctx plugin.Ctx, in *UpdatePageIn) (*UpdatePageOut, error) {
	route, e := checkedPath("pages", in.PageID)
	if e != nil {
		return nil, e
	}
	changes := in.Changes
	if changes.HTML != nil && changes.Markdown != nil {
		return nil, errors.New("Supply HTML or Markdown content, not both.")
	}
	body := map[string]any{}
	if changes.Name != nil {
		if !pageName(*changes.Name) {
			return nil, errors.New("Page name must contain 1–255 bytes without control characters.")
		}
		body["name"] = *changes.Name
	}
	for _, pair := range []struct {
		k string
		v *string
	}{{"html", changes.HTML}, {"markdown", changes.Markdown}} {
		if pair.v != nil {
			if !validBody(*pair.v, 1<<20) {
				return nil, errors.New("Page content exceeds 1 MiB or contains invalid UTF-8 text.")
			}
			body[pair.k] = *pair.v
		}
	}
	if e = addPageOptions(body, changes.Priority, changes.Tags, changes.Changelog); e != nil {
		return nil, e
	}
	if len(body) == 0 {
		return nil, errors.New("Provide at least one page change.")
	}
	var w pageWire
	if _, e = c.json(ctx, "PUT", route, nil, body, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.output()
	if e != nil {
		return nil, e
	}
	if v.ID != in.PageID {
		return nil, errors.New("BookStack returned a different page ID.")
	}
	return &UpdatePageOut{Page: v}, nil
}
