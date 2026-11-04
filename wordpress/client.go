package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const wpPrefix = "/wp-json/wp/v2"
const maxMedia = 10 << 20

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

func validID(s string) error {
	if !numericID.MatchString(s) {
		return errors.New("Provide a positive numeric WordPress resource ID.")
	}
	return nil
}
func pageQuery(page, search string) (url.Values, int64, error) {
	if page == "" {
		page = "1"
	}
	if !numericID.MatchString(page) {
		return nil, 0, errors.New("page must be a positive integer string.")
	}
	n, e := strconv.ParseInt(page, 10, 64)
	if e != nil || n > 1000000 {
		return nil, 0, errors.New("page must not exceed 1000000.")
	}
	if len(search) > 4096 {
		return nil, 0, errors.New("Search text exceeds 4096 bytes.")
	}
	q := url.Values{"page": {page}, "per_page": {"20"}}
	if search != "" {
		q.Set("search", search)
	}
	return q, n, nil
}
func pageHeaders(h http.Header, page int64) (int64, int64, string, error) {
	total, e := strconv.ParseInt(h.Get("X-WP-Total"), 10, 64)
	pages, e2 := strconv.ParseInt(h.Get("X-WP-TotalPages"), 10, 64)
	if e != nil || e2 != nil || total < 0 || pages < 0 {
		return 0, 0, "", errors.New("WordPress pagination headers are missing or invalid. Check the reverse proxy.")
	}
	next := ""
	if page < pages {
		next = strconv.FormatInt(page+1, 10)
	}
	return total, pages, next, nil
}

type rendered struct {
	Rendered string `json:"rendered"`
}
type upstreamPost struct {
	ID         int64    `json:"id"`
	Status     string   `json:"status"`
	Slug       string   `json:"slug"`
	Link       string   `json:"link"`
	Title      rendered `json:"title"`
	Content    rendered `json:"content"`
	Excerpt    rendered `json:"excerpt"`
	Modified   string   `json:"modified_gmt"`
	Categories []int64  `json:"categories"`
}

func (r upstreamPost) typed() (Post, error) {
	if r.ID <= 0 || r.Status == "" {
		return Post{}, errors.New("WordPress response is missing a valid post ID or status.")
	}
	cats := []string{}
	for _, id := range r.Categories {
		if id <= 0 {
			return Post{}, errors.New("WordPress returned an invalid category ID.")
		}
		cats = append(cats, strconv.FormatInt(id, 10))
	}
	return Post{ID: strconv.FormatInt(r.ID, 10), Status: r.Status, Slug: r.Slug, Link: r.Link, TitleHTML: r.Title.Rendered, ContentHTML: r.Content.Rendered, ExcerptHTML: r.Excerpt.Rendered, ModifiedGmt: r.Modified, Categories: cats}, nil
}
func (c *client) listPosts(ctx context.Context, in *ListPostsIn) (*ListPostsOut, error) {
	q, page, e := pageQuery(in.Page, in.Search)
	if e != nil {
		return nil, e
	}
	status := in.Status
	if status == "" {
		status = "publish"
	}
	switch status {
	case "publish", "draft", "pending", "private", "future":
	default:
		return nil, errors.New("Unsupported post status.")
	}
	q.Set("status", status)
	var rows []upstreamPost
	h, e := c.json(ctx, "GET", wpPrefix+"/posts", q, nil, &rows)
	if e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("WordPress response is missing the posts array.")
	}
	total, pages, next, e := pageHeaders(h, page)
	if e != nil {
		return nil, e
	}
	out := &ListPostsOut{Posts: []Post{}, Total: total, TotalPages: pages, NextPage: next}
	for _, r := range rows {
		p, e := r.typed()
		if e != nil {
			return nil, e
		}
		out.Posts = append(out.Posts, p)
	}
	return out, nil
}
func (c *client) getPost(ctx context.Context, in *GetPostIn) (*GetPostOut, error) {
	if e := validID(in.PostID); e != nil {
		return nil, e
	}
	var r upstreamPost
	if _, e := c.json(ctx, "GET", wpPrefix+"/posts/"+in.PostID, nil, nil, &r); e != nil {
		return nil, e
	}
	p, e := r.typed()
	if e == nil && p.ID != in.PostID {
		return nil, errors.New("WordPress returned a different post ID.")
	}
	return &GetPostOut{Post: p}, e
}
func postBody(title, content, excerpt string, categories []string) (map[string]any, error) {
	if strings.TrimSpace(title) == "" || len(title) > 4096 {
		return nil, errors.New("Provide a title of 1 to 4096 bytes.")
	}
	if len(content) > maxRequest || len(excerpt) > 65536 {
		return nil, errors.New("Content exceeds 1 MiB or excerpt exceeds 64 KiB.")
	}
	body := map[string]any{"title": title, "content": content, "excerpt": excerpt}
	if categories != nil {
		if len(categories) > 100 {
			return nil, errors.New("At most 100 categories are supported.")
		}
		ids := []int64{}
		for _, s := range categories {
			if e := validID(s); e != nil {
				return nil, e
			}
			id, _ := strconv.ParseInt(s, 10, 64)
			ids = append(ids, id)
		}
		body["categories"] = ids
	}
	return body, nil
}
func (c *client) save(ctx context.Context, id, title, content, excerpt string, categories []string) (Post, error) {
	body, e := postBody(title, content, excerpt, categories)
	if e != nil {
		return Post{}, e
	}
	path := wpPrefix + "/posts"
	if id == "" {
		body["status"] = "draft"
	} else {
		if e = validID(id); e != nil {
			return Post{}, e
		}
		path += "/" + id
	}
	var r upstreamPost
	if _, e = c.json(ctx, "POST", path, nil, body, &r); e != nil {
		return Post{}, e
	}
	p, e := r.typed()
	if e != nil {
		return p, errors.New("WordPress returned no valid post receipt. Inspect the site before retrying.")
	}
	if id != "" && p.ID != id {
		return p, errors.New("WordPress returned a different post receipt ID. Inspect the site before retrying.")
	}
	if id == "" && p.Status != "draft" {
		return p, errors.New("WordPress did not retain draft status. Inspect site hooks and the created post before retrying.")
	}
	return p, nil
}
func (c *client) createDraft(ctx context.Context, in *CreateDraftIn) (*CreateDraftOut, error) {
	p, e := c.save(ctx, "", in.Title, in.Content, in.Excerpt, in.Categories)
	return &CreateDraftOut{Post: p}, e
}
func (c *client) updatePost(ctx context.Context, in *UpdatePostIn) (*UpdatePostOut, error) {
	if e := validID(in.PostID); e != nil {
		return nil, e
	}
	p, e := c.save(ctx, in.PostID, in.Title, in.Content, in.Excerpt, in.Categories)
	return &UpdatePostOut{Post: p}, e
}
func (c *client) listCategories(ctx context.Context, in *ListCategoriesIn) (*ListCategoriesOut, error) {
	q, page, e := pageQuery(in.Page, in.Search)
	if e != nil {
		return nil, e
	}
	var rows []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Parent int64  `json:"parent"`
		Count  int64  `json:"count"`
	}
	h, e := c.json(ctx, "GET", wpPrefix+"/categories", q, nil, &rows)
	if e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("WordPress response is missing the categories array.")
	}
	total, pages, next, e := pageHeaders(h, page)
	if e != nil {
		return nil, e
	}
	out := &ListCategoriesOut{Categories: []Category{}, Total: total, TotalPages: pages, NextPage: next}
	for _, r := range rows {
		if r.ID <= 0 || r.Parent < 0 || r.Count < 0 {
			return nil, errors.New("WordPress returned invalid category metadata.")
		}
		out.Categories = append(out.Categories, Category{ID: strconv.FormatInt(r.ID, 10), Name: r.Name, Slug: r.Slug, ParentID: strconv.FormatInt(r.Parent, 10), Count: r.Count})
	}
	return out, nil
}

type upstreamMedia struct {
	ID        int64    `json:"id"`
	SourceURL string   `json:"source_url"`
	Mime      string   `json:"mime_type"`
	Alt       string   `json:"alt_text"`
	Title     rendered `json:"title"`
}

func (r upstreamMedia) typed() (Media, error) {
	if r.ID <= 0 || r.SourceURL == "" || r.Mime == "" {
		return Media{}, errors.New("WordPress returned incomplete media metadata.")
	}
	return Media{ID: strconv.FormatInt(r.ID, 10), SourceURL: r.SourceURL, MimeType: r.Mime, AltText: r.Alt, TitleHTML: r.Title.Rendered}, nil
}
func (c *client) listMedia(ctx context.Context, in *ListMediaIn) (*ListMediaOut, error) {
	q, page, e := pageQuery(in.Page, in.Search)
	if e != nil {
		return nil, e
	}
	var rows []upstreamMedia
	h, e := c.json(ctx, "GET", wpPrefix+"/media", q, nil, &rows)
	if e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("WordPress response is missing the media array.")
	}
	total, pages, next, e := pageHeaders(h, page)
	if e != nil {
		return nil, e
	}
	out := &ListMediaOut{Media: []Media{}, Total: total, TotalPages: pages, NextPage: next}
	for _, r := range rows {
		m, e := r.typed()
		if e != nil {
			return nil, e
		}
		out.Media = append(out.Media, m)
	}
	return out, nil
}
func validateUpload(in *UploadMediaIn) error {
	if in.File == nil || in.File.Size < 0 || in.File.Size > maxMedia {
		return errors.New("Provide a platform file of at most 10 MiB.")
	}
	if strings.TrimSpace(in.File.Name) == "" || len(in.File.Name) > 255 || strings.ContainsAny(in.File.Name, "/\\\r\n\x00") {
		return errors.New("Provide a filename without paths or control characters.")
	}
	switch in.File.Mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf", "text/plain":
	default:
		return errors.New("Supported media types are JPEG, PNG, GIF, WebP, PDF and plain text.")
	}
	if len(in.AltText) > 4096 {
		return errors.New("Alternative text exceeds 4096 bytes.")
	}
	return nil
}
func (c *client) uploadMedia(ctx plugin.Ctx, in *UploadMediaIn) (*UploadMediaOut, error) {
	if e := validateUpload(in); e != nil {
		return nil, e
	}
	data, e := in.File.Blob(ctx)
	if e != nil {
		return nil, errors.New("Failed to read the platform file. Check file access.")
	}
	if len(data) == 0 || len(data) > maxMedia {
		return nil, errors.New("Media must contain 1 byte to 10 MiB.")
	}
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	// PHP's multipart parser requires a regular filename parameter. Do not use
	// mime.FormatMediaType: it emits filename* for UTF-8 names, which RFC 7578
	// disallows and PHP does not treat as an uploaded file.
	filename := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(in.File.Name)
	disposition := `form-data; name="file"; filename="` + filename + `"`
	part, e := mw.CreatePart(textproto.MIMEHeader{"Content-Disposition": {disposition}, "Content-Type": {in.File.Mime}})
	if e != nil {
		return nil, errors.New("Failed to prepare the media upload.")
	}
	if _, e = io.Copy(part, bytes.NewReader(data)); e != nil {
		return nil, errors.New("Failed to prepare the media upload.")
	}
	if in.AltText != "" {
		if e = mw.WriteField("alt_text", in.AltText); e != nil {
			return nil, e
		}
	}
	if e = mw.Close(); e != nil {
		return nil, e
	}
	raw, _, e := c.request(ctx, "POST", wpPrefix+"/media", nil, b.Bytes(), mw.FormDataContentType())
	if e != nil {
		return nil, e
	}
	var r upstreamMedia
	if e = decodeJSON(raw, &r); e != nil {
		return nil, e
	}
	m, e := r.typed()
	if e != nil {
		return nil, errors.New("WordPress returned no valid media receipt. Inspect the media library before retrying.")
	}
	return &UploadMediaOut{Media: m}, nil
}
func (c *client) health(ctx context.Context) (*HealthCheckOut, error) {
	var r struct {
		ID int64 `json:"id"`
	}
	_, e := c.json(ctx, "GET", wpPrefix+"/users/me", nil, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.ID <= 0 {
		return &HealthCheckOut{Message: "WordPress did not identify an authenticated user."}, nil
	}
	return &HealthCheckOut{OK: true, Message: fmt.Sprintf("Authenticated WordPress user %d. Edit and upload capabilities are checked by each operation.", r.ID)}, nil
}
