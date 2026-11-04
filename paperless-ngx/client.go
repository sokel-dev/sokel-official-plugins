package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/paperless-ngx/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type documentWire struct {
	ID               int64   `json:"id"`
	Title            string  `json:"title"`
	Content          string  `json:"content"`
	Tags             []int64 `json:"tags"`
	Correspondent    *int64  `json:"correspondent"`
	DocumentType     *int64  `json:"document_type"`
	Created          string  `json:"created"`
	Modified         string  `json:"modified"`
	OriginalFileName string  `json:"original_file_name"`
	MIMEType         string  `json:"mime_type"`
}
type tagWire struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	IsInboxTag bool   `json:"is_inbox_tag"`
}
type pageWire struct {
	Count   *int64          `json:"count"`
	Next    json.RawMessage `json:"next"`
	Results json.RawMessage `json:"results"`
}
type taskWire struct {
	TaskID             string  `json:"task_id"`
	TaskType           string  `json:"task_type"`
	Status             string  `json:"status"`
	RelatedDocumentIDs []int64 `json:"related_document_ids"`
	DateDone           *string `json:"date_done"`
}

func numericIDs(ids []int64) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, n := range ids {
		s, e := idString(n)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}
func parseIDs(ids []string) ([]int64, error) {
	if len(ids) > 1000 {
		return nil, errors.New("At most 1000 tag IDs are supported.")
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, s := range ids {
		n, e := positiveID(s)
		if e != nil {
			return nil, e
		}
		if seen[n] {
			return nil, errors.New("Tag IDs must not contain duplicates.")
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}
func (w documentWire) output() (schema.Document, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Document{}, e
	}
	tags, e := numericIDs(w.Tags)
	if e != nil {
		return schema.Document{}, e
	}
	out := schema.Document{ID: id, Title: w.Title, Content: w.Content, Tags: tags, Created: w.Created, Modified: w.Modified, OriginalFileName: w.OriginalFileName, MIMEType: w.MIMEType}
	for _, pair := range []struct {
		in  *int64
		out **string
	}{{w.Correspondent, &out.CorrespondentID}, {w.DocumentType, &out.DocumentTypeID}} {
		if pair.in != nil {
			s, e := idString(*pair.in)
			if e != nil {
				return schema.Document{}, e
			}
			*pair.out = &s
		}
	}
	return out, nil
}
func (w tagWire) output() (schema.Tag, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Tag{}, e
	}
	if w.Name == "" {
		return schema.Tag{}, errors.New("Paperless-ngx returned a tag without a name.")
	}
	return schema.Tag{ID: id, Name: w.Name, Color: w.Color, IsInboxTag: w.IsInboxTag}, nil
}
func pageQuery(p schema.PageOptions) (url.Values, int, int, error) {
	page, size := 1, 50
	if p.Page != nil {
		page = *p.Page
	}
	if p.PageSize != nil {
		size = *p.PageSize
	}
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		return nil, 0, 0, errors.New("Page must be 1–100000 and page size must be 1–100.")
	}
	return url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(size)}}, page, size, nil
}
func (c *client) collection(ctx plugin.Ctx, route string, q url.Values, page int) (pageWire, int, error) {
	var w pageWire
	if _, e := c.json(ctx, "GET", route, q, nil, 200, &w); e != nil {
		return w, 0, e
	}
	if w.Count == nil || *w.Count < 0 || len(w.Results) == 0 || w.Results[0] != '[' || len(w.Next) == 0 {
		return w, 0, errors.New("Paperless-ngx returned an incomplete API 10 paginated response.")
	}
	var next *string
	if e := decodeJSON(w.Next, &next); e != nil {
		return w, 0, e
	}
	if next == nil {
		return w, 0, nil
	}
	if *next == "" {
		return w, 0, errors.New("Paperless-ngx returned an empty pagination URL.")
	}
	n, e := c.nextPage(*next, route, q, page)
	return w, n, e
}
func validatePage(count int64, size, limit int, next int) error {
	if size > limit || int64(size) > count {
		return errors.New("Paperless-ngx returned inconsistent pagination counts.")
	}
	if next > 0 && count == 0 {
		return errors.New("Paperless-ngx returned a continuation for an empty collection.")
	}
	return nil
}
func (c *client) healthCheck(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
	one := 1
	_, e := c.listDocuments(ctx, &ListDocumentsIn{Pagination: schema.PageOptions{PageSize: &one}})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Paperless-ngx accepted the token and returned an API 10 document list. Write permissions and ingestion workers are checked separately."}, nil
}
func (c *client) listDocuments(ctx plugin.Ctx, in *ListDocumentsIn) (*ListDocumentsOut, error) {
	q, page, size, e := pageQuery(in.Pagination)
	if e != nil {
		return nil, e
	}
	if !safeText(in.Query, 4096) {
		return nil, errors.New("Search query must be at most 4096 bytes without control characters.")
	}
	if in.Query != "" {
		q.Set("query", in.Query)
	}
	w, next, e := c.collection(ctx, "/api/documents/", q, page)
	if e != nil {
		return nil, e
	}
	var rows []documentWire
	if e = decodeJSON(w.Results, &rows); e != nil {
		return nil, e
	}
	if e = validatePage(*w.Count, len(rows), size, next); e != nil {
		return nil, e
	}
	out := &ListDocumentsOut{Documents: make([]schema.Document, 0, len(rows)), NextPage: next, TotalCount: strconv.FormatInt(*w.Count, 10)}
	for _, row := range rows {
		v, e := row.output()
		if e != nil {
			return nil, e
		}
		out.Documents = append(out.Documents, v)
	}
	return out, nil
}
func documentPath(id string) (string, error) {
	if _, e := positiveID(id); e != nil {
		return "", e
	}
	return "/api/documents/" + id + "/", nil
}
func (c *client) getDocument(ctx plugin.Ctx, in *GetDocumentIn) (*GetDocumentOut, error) {
	route, e := documentPath(in.DocumentID)
	if e != nil {
		return nil, e
	}
	var w documentWire
	if _, e = c.json(ctx, "GET", route, nil, nil, 200, &w); e != nil {
		return nil, e
	}
	v, e := w.output()
	if e != nil {
		return nil, e
	}
	if v.ID != in.DocumentID {
		return nil, errors.New("Paperless-ngx returned a different document ID.")
	}
	return &GetDocumentOut{Document: v}, nil
}
func validFilename(s string) bool {
	return s != "" && s != "." && s != ".." && safeText(s, 255) && !strings.ContainsAny(s, "/\\")
}
func quotedFilename(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
func readFile(ctx plugin.Ctx, f *plugin.File) ([]byte, error) {
	if f == nil || f.Size < 0 || f.Size > maxFile || !validFilename(f.Name) {
		return nil, errors.New("Provide a platform file with a plain filename and at most 32 MiB.")
	}
	if f.ID == "" && len(f.Data) == 0 {
		return nil, errors.New("Provide a platform file ID; local paths and external URLs are not supported.")
	}
	b, e := f.Blob(ctx)
	if e != nil {
		return nil, errors.New("Could not read the platform file.")
	}
	if len(b) == 0 || int64(len(b)) > maxFile {
		return nil, errors.New("The platform file must contain 1 byte to 32 MiB.")
	}
	if f.Size > 0 && int64(len(b)) != f.Size {
		return nil, errors.New("Platform file byte count does not match its declared size.")
	}
	return b, nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (c *client) uploadDocument(ctx plugin.Ctx, in *UploadDocumentIn) (*UploadDocumentOut, error) {
	fields := url.Values{}
	o := in.Options
	if o.Title != nil {
		if !safeText(*o.Title, 255) {
			return nil, errors.New("Title must be at most 255 bytes without control characters.")
		}
		fields.Set("title", *o.Title)
	}
	for _, pair := range []struct {
		name  string
		value *string
	}{{"correspondent", o.CorrespondentID}, {"document_type", o.DocumentTypeID}} {
		if pair.value != nil {
			if _, e := positiveID(*pair.value); e != nil {
				return nil, e
			}
			fields.Set(pair.name, *pair.value)
		}
	}
	if _, e := parseIDs(o.Tags); e != nil {
		return nil, e
	}
	for _, tag := range o.Tags {
		fields.Add("tags", tag)
	}
	b, e := readFile(ctx, in.File)
	if e != nil {
		return nil, e
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="document"; filename="`+quotedFilename(in.File.Name)+`"`)
	typ := in.File.Mime
	if typ == "" {
		typ = "application/octet-stream"
	}
	mt, _, e := mime.ParseMediaType(typ)
	if e != nil || !safeText(typ, 255) {
		return nil, errors.New("The platform file has an invalid media type.")
	}
	h.Set("Content-Type", mt)
	part, e := writer.CreatePart(h)
	if e != nil {
		return nil, errors.New("Could not encode the document upload.")
	}
	if _, e = part.Write(b); e != nil {
		return nil, e
	}
	for _, name := range []string{"title", "correspondent", "document_type", "tags"} {
		for _, value := range fields[name] {
			if e = writer.WriteField(name, value); e != nil {
				return nil, e
			}
		}
	}
	if e = writer.Close(); e != nil {
		return nil, e
	}
	raw, headers, e := c.request(ctx, "POST", "/api/documents/post_document/", nil, body.Bytes(), writer.FormDataContentType(), 200, maxJSON)
	if e != nil {
		return nil, e
	}
	media, _, e := mime.ParseMediaType(headers.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return nil, errors.New("Paperless-ngx returned an unexpected upload response media type.")
	}
	var id string
	if e = decodeJSON(raw, &id); e != nil {
		return nil, e
	}
	if !uuidPattern.MatchString(id) {
		return nil, errors.New("Paperless-ngx did not return a consumption task UUID.")
	}
	return &UploadDocumentOut{TaskID: id, Accepted: true}, nil
}
func (c *client) getIngestionTask(ctx plugin.Ctx, in *GetIngestionTaskIn) (*GetIngestionTaskOut, error) {
	if !uuidPattern.MatchString(in.TaskID) {
		return nil, errors.New("Task ID must be a UUID returned by upload_document.")
	}
	q := url.Values{"task_id": {in.TaskID}, "page": {"1"}, "page_size": {"2"}}
	w, next, e := c.collection(ctx, "/api/tasks/", q, 1)
	if e != nil {
		return nil, e
	}
	var rows []taskWire
	if e = decodeJSON(w.Results, &rows); e != nil {
		return nil, e
	}
	if *w.Count != int64(len(rows)) || len(rows) > 1 || next != 0 {
		return nil, errors.New("Paperless-ngx returned an ambiguous task lookup.")
	}
	if len(rows) == 0 {
		return &GetIngestionTaskOut{Found: false}, nil
	}
	row := rows[0]
	if !strings.EqualFold(row.TaskID, in.TaskID) || row.TaskType != "consume_file" {
		return nil, errors.New("Paperless-ngx returned a different task or a non-ingestion task.")
	}
	complete := false
	succeeded := false
	switch row.Status {
	case "pending", "started":
	case "success":
		complete = true
		succeeded = true
	case "failure", "revoked":
		complete = true
	default:
		return nil, errors.New("Paperless-ngx returned an unsupported API 10 task status.")
	}
	ids, e := numericIDs(row.RelatedDocumentIDs)
	if e != nil {
		return nil, e
	}
	return &GetIngestionTaskOut{Found: true, Task: schema.IngestionTask{TaskID: row.TaskID, TaskType: row.TaskType, Status: row.Status, Complete: complete, Succeeded: succeeded, RelatedDocumentIDs: ids, DateDone: row.DateDone}}, nil
}
func (c *client) downloadDocument(ctx plugin.Ctx, in *DownloadDocumentIn) (*DownloadDocumentOut, error) {
	route, e := documentPath(in.DocumentID)
	if e != nil {
		return nil, e
	}
	q := url.Values{}
	if in.Options.Original != nil {
		q.Set("original", strconv.FormatBool(*in.Options.Original))
	}
	if in.Options.VersionID != nil {
		if _, e := positiveID(*in.Options.VersionID); e != nil {
			return nil, e
		}
		q.Set("version", *in.Options.VersionID)
	}
	b, h, e := c.request(ctx, "GET", route+"download/", q, nil, "", 200, maxFile)
	if e != nil {
		return nil, e
	}
	if len(b) == 0 {
		return nil, errors.New("Paperless-ngx returned an empty document.")
	}
	typ, _, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil {
		return nil, errors.New("Paperless-ngx returned an invalid document media type.")
	}
	disposition, params, e := mime.ParseMediaType(h.Get("Content-Disposition"))
	if e != nil || disposition != "attachment" || !validFilename(params["filename"]) {
		return nil, errors.New("Paperless-ngx did not return a valid attachment filename.")
	}
	file, e := ctx.Upload(params["filename"], typ, b)
	if e != nil || file == nil || file.ID == "" {
		return nil, errors.New("Could not store the downloaded document as a platform file.")
	}
	return &DownloadDocumentOut{File: file}, nil
}
func (c *client) updateDocument(ctx plugin.Ctx, in *UpdateDocumentIn) (*UpdateDocumentOut, error) {
	route, e := documentPath(in.DocumentID)
	if e != nil {
		return nil, e
	}
	m := in.Metadata
	body := map[string]any{}
	if m.Title != nil {
		if !safeText(*m.Title, 255) {
			return nil, errors.New("Title must be at most 255 bytes without control characters.")
		}
		body["title"] = *m.Title
	}
	if m.Tags != nil {
		ids, e := parseIDs(*m.Tags)
		if e != nil {
			return nil, e
		}
		body["tags"] = ids
	}
	for _, pair := range []struct {
		name  string
		value *string
	}{{"correspondent", m.CorrespondentID}, {"document_type", m.DocumentTypeID}} {
		if pair.value != nil {
			n, e := positiveID(*pair.value)
			if e != nil {
				return nil, e
			}
			body[pair.name] = n
		}
	}
	if len(body) == 0 {
		return nil, errors.New("Provide at least one metadata change. An empty tag array removes all tags.")
	}
	var row documentWire
	if _, e = c.json(ctx, "PATCH", route, nil, body, 200, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	if v.ID != in.DocumentID {
		return nil, errors.New("Paperless-ngx returned a different document ID.")
	}
	return &UpdateDocumentOut{Document: v}, nil
}
func (c *client) listTags(ctx plugin.Ctx, in *ListTagsIn) (*ListTagsOut, error) {
	q, page, size, e := pageQuery(in.Pagination)
	if e != nil {
		return nil, e
	}
	w, next, e := c.collection(ctx, "/api/tags/", q, page)
	if e != nil {
		return nil, e
	}
	var rows []tagWire
	if e = decodeJSON(w.Results, &rows); e != nil {
		return nil, e
	}
	if e = validatePage(*w.Count, len(rows), size, next); e != nil {
		return nil, e
	}
	out := &ListTagsOut{Tags: make([]schema.Tag, 0, len(rows)), NextPage: next, TotalCount: strconv.FormatInt(*w.Count, 10)}
	for _, row := range rows {
		v, e := row.output()
		if e != nil {
			return nil, e
		}
		out.Tags = append(out.Tags, v)
	}
	return out, nil
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (c *client) createTag(ctx plugin.Ctx, in *CreateTagIn) (*CreateTagOut, error) {
	if strings.TrimSpace(in.Name) == "" || !safeText(in.Name, 128) {
		return nil, errors.New("Tag name must contain 1–128 bytes without control characters.")
	}
	body := map[string]any{"name": in.Name}
	if in.Options.Color != nil {
		if !colorPattern.MatchString(*in.Options.Color) {
			return nil, errors.New("Tag color must have the form #RRGGBB.")
		}
		body["color"] = *in.Options.Color
	}
	if in.Options.IsInboxTag != nil {
		body["is_inbox_tag"] = *in.Options.IsInboxTag
	}
	var row tagWire
	if _, e := c.json(ctx, "POST", "/api/tags/", nil, body, 201, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	return &CreateTagOut{Tag: v}, nil
}
