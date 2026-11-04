package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/box/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var numericID = regexp.MustCompile(`^[0-9]{1,30}$`)

const itemFields = "id,type,name,size,etag,modified_at"

func validID(id string, root bool) bool { return numericID.MatchString(id) && (root || id != "0") }
func validItem(v schema.Item) error {
	if !validID(v.ID, v.Type == "folder") || !validName(v.Name) {
		return errors.New("Box returned an invalid item identity.")
	}
	if v.Type != "file" && v.Type != "folder" && v.Type != "web_link" {
		return errors.New("Box returned an unsupported item type.")
	}
	if v.Size != nil && *v.Size < 0 {
		return errors.New("Box returned an invalid item size.")
	}
	if v.SHA1 != nil && *v.SHA1 != "" {
		b, e := hex.DecodeString(*v.SHA1)
		if e != nil || len(b) != 20 {
			return errors.New("Box returned an invalid file checksum.")
		}
	}
	return nil
}
func fields() url.Values     { return url.Values{"fields": {itemFields}} }
func fileFields() url.Values { return url.Values{"fields": {itemFields + ",sha1"}} }
func (c *client) health_check(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct{ ID, Type string }
	e := c.json(ctx, "GET", "/2.0/users/me", url.Values{"fields": {"id,type"}}, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if !validID(r.ID, false) || r.Type != "user" {
		return &HealthCheckOut{Message: "Box did not identify an authenticated user."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Box user identity verified. File and folder permissions are checked by each operation."}, nil
}
func (c *client) list_folder(ctx plugin.Ctx, in *ListFolderIn) (*ListFolderOut, error) {
	if !validID(in.FolderID, true) {
		return nil, errors.New("Provide a numeric folder ID; use 0 for the root.")
	}
	if len(in.Marker) > 16384 || !utf8.ValidString(in.Marker) || strings.IndexFunc(in.Marker, unicode.IsControl) >= 0 {
		return nil, errors.New("Invalid pagination marker.")
	}
	q := fields()
	q.Set("usemarker", "true")
	q.Set("limit", "100")
	if in.Marker != "" {
		q.Set("marker", in.Marker)
	}
	var r struct {
		Entries    []schema.Item `json:"entries"`
		NextMarker string        `json:"next_marker"`
		Limit      *int          `json:"limit"`
	}
	if e := c.json(ctx, "GET", "/2.0/folders/"+in.FolderID+"/items", q, nil, &r); e != nil {
		return nil, e
	}
	if r.Entries == nil || r.Limit == nil || *r.Limit < 1 || *r.Limit > 100 || len(r.NextMarker) > 16384 || (r.NextMarker != "" && r.NextMarker == in.Marker) {
		return nil, errors.New("Box returned invalid marker pagination.")
	}
	for _, v := range r.Entries {
		if e := validItem(v); e != nil {
			return nil, e
		}
	}
	return &ListFolderOut{Items: r.Entries, NextMarker: r.NextMarker, HasMore: r.NextMarker != ""}, nil
}
func (c *client) search(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 4096 || !utf8.ValidString(in.Query) {
		return nil, errors.New("Provide a search query of 1 to 4096 bytes.")
	}
	offset, limit := 0, 100
	if in.Page.Offset != nil {
		offset = *in.Page.Offset
	}
	if in.Page.Limit != nil {
		limit = *in.Page.Limit
	}
	if offset < 0 || offset > 10000 || limit < 1 || limit > 200 {
		return nil, errors.New("Search offset must be 0 to 10000 and page size 1 to 200. Partition larger searches.")
	}
	q := fields()
	q.Set("query", in.Query)
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("include_recent_shared_links", "false")
	q.Set("trash_content", "non_trashed_only")
	if in.AncestorFolderID != "" {
		if !validID(in.AncestorFolderID, true) {
			return nil, errors.New("Provide a numeric ancestor folder ID.")
		}
		q.Set("ancestor_folder_ids", in.AncestorFolderID)
	}
	var r struct {
		Type          string        `json:"type"`
		Entries       []schema.Item `json:"entries"`
		Offset, Limit *int
		Total         *int `json:"total_count"`
	}
	if e := c.json(ctx, "GET", "/2.0/search", q, nil, &r); e != nil {
		return nil, e
	}
	if r.Type != "search_results_items" || r.Entries == nil || r.Offset == nil || *r.Offset != offset || r.Limit == nil || *r.Limit < 1 || *r.Limit > limit || r.Total == nil || *r.Total < 0 {
		return nil, errors.New("Box returned invalid search pagination.")
	}
	for _, v := range r.Entries {
		if e := validItem(v); e != nil {
			return nil, e
		}
	}
	next := offset + *r.Limit
	more := next < *r.Total
	if !more {
		next = 0
	}
	return &SearchOut{Items: r.Entries, TotalCount: *r.Total, NextOffset: next, HasMore: more}, nil
}
func (c *client) get(ctx context.Context, id, kind string) (schema.Item, error) {
	var r schema.Item
	if !validID(id, kind == "folder") {
		return r, errors.New("Provide a numeric item ID of the requested type.")
	}
	q := fields()
	if kind == "file" {
		q = fileFields()
	}
	if e := c.json(ctx, "GET", "/2.0/"+kind+"s/"+id, q, nil, &r); e != nil {
		return r, e
	}
	if e := validItem(r); e != nil {
		return r, e
	}
	if r.ID != id || r.Type != kind {
		return r, errors.New("Box returned a different item identity.")
	}
	return r, nil
}
func (c *client) get_file(ctx plugin.Ctx, in *GetFileIn) (*GetFileOut, error) {
	v, e := c.get(ctx, in.FileID, "file")
	return &GetFileOut{Item: v}, e
}
func (c *client) get_folder(ctx plugin.Ctx, in *GetFolderIn) (*GetFolderOut, error) {
	v, e := c.get(ctx, in.FolderID, "folder")
	return &GetFolderOut{Item: v}, e
}
func (c *client) create_folder(ctx plugin.Ctx, in *CreateFolderIn) (*CreateFolderOut, error) {
	if !validID(in.ParentID, true) || !validName(in.Name) || strings.TrimSpace(in.Name) != in.Name {
		return nil, errors.New("Provide a numeric parent ID and a folder name without paths, control characters or outer spaces.")
	}
	var v schema.Item
	if e := c.json(ctx, "POST", "/2.0/folders", fields(), map[string]any{"name": in.Name, "parent": map[string]string{"id": in.ParentID}}, &v); e != nil {
		return nil, e
	}
	if validItem(v) != nil || v.Type != "folder" || v.ID == "0" {
		return nil, errors.New("Box returned an invalid folder receipt. Inspect the parent folder before retrying.")
	}
	return &CreateFolderOut{Item: v}, nil
}
func (c *client) upload_file(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
	if !validID(in.ParentID, true) || in.File == nil || in.File.Size < 0 || in.File.Size > maxFile {
		return nil, errors.New("Provide a numeric parent folder ID and a platform file of at most 32 MiB.")
	}
	if !validName(in.File.Name) || strings.TrimSpace(in.File.Name) != in.File.Name {
		return nil, errors.New("Use a filename without paths, control characters or outer spaces.")
	}
	data, e := in.File.Blob(ctx)
	if e != nil {
		return nil, errors.New("Could not read the platform file.")
	}
	if len(data) > maxFile {
		return nil, errors.New("Platform file exceeds 32 MiB.")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	attrs, _ := json.Marshal(map[string]any{"name": in.File.Name, "parent": map[string]string{"id": in.ParentID}})
	if e = w.WriteField("attributes", string(attrs)); e != nil {
		return nil, errors.New("Could not prepare file attributes.")
	}
	filename := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(in.File.Name)
	part, e := w.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="` + filename + `"`}, "Content-Type": {fileMIME(in.File.Mime)}})
	if e != nil {
		return nil, errors.New("Could not prepare the file part.")
	}
	if _, e = part.Write(data); e != nil {
		return nil, errors.New("Could not prepare file bytes.")
	}
	if e = w.Close(); e != nil {
		return nil, errors.New("Could not finish the multipart request.")
	}
	endpoint, e := c.endpoint(c.contentBase, "/api/2.0/files/content", fileFields())
	if e != nil {
		return nil, e
	}
	res, e := c.do(ctx, "POST", endpoint, body.Bytes(), http.Header{"Content-Type": {w.FormDataContentType()}, "Accept": {"application/json"}}, true, 120*time.Second)
	if e != nil {
		return nil, e
	}
	var r struct {
		Entries []schema.Item `json:"entries"`
		Total   *int          `json:"total_count"`
	}
	if e = jsonResponse(res, &r); e != nil {
		return nil, e
	}
	if r.Total == nil || *r.Total != 1 || len(r.Entries) != 1 || validItem(r.Entries[0]) != nil || r.Entries[0].Type != "file" || r.Entries[0].Size == nil || *r.Entries[0].Size != int64(len(data)) {
		return nil, errors.New("Box returned an invalid upload receipt. Inspect the folder before retrying.")
	}
	return &UploadFileOut{Item: r.Entries[0]}, nil
}
func allowedDownload(raw, token string) bool {
	if len(raw) > 16384 || raw == "" || strings.Contains(raw, "\r") || strings.Contains(raw, "\n") {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "dl.boxcloud.com" && !strings.HasSuffix(host, ".boxcloud.com") {
		return false
	}
	if u.Query().Get("access_token") != "" || u.Query().Get("token") == token && token != "" {
		return false
	}
	if token != "" && (strings.Contains(raw, token) || strings.Contains(u.Path, token) || strings.Contains(u.RawQuery, url.QueryEscape(token))) {
		return false
	}
	for _, values := range u.Query() {
		for _, value := range values {
			if token != "" && strings.Contains(value, token) {
				return false
			}
		}
	}
	return true
}
func (c *client) download_file(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
	deadline, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	v, e := c.get(deadline, in.FileID, "file")
	if e != nil {
		return nil, e
	}
	if v.Size == nil || *v.Size > maxFile {
		return nil, errors.New("Box file size is missing or exceeds the 32 MiB download limit.")
	}
	endpoint, e := c.endpoint(c.base, "/2.0/files/"+in.FileID+"/content", nil)
	if e != nil {
		return nil, e
	}
	res, e := c.do(deadline, "GET", endpoint, nil, nil, true, 120*time.Second)
	if e != nil {
		return nil, e
	}
	for hops := 0; res.StatusCode == http.StatusFound || res.StatusCode == http.StatusTemporaryRedirect || res.StatusCode == http.StatusSeeOther; hops++ {
		location := res.Header.Get("Location")
		res.Body.Close()
		if hops >= 3 || !allowedDownload(location, strings.TrimSpace(c.cred.AccessToken)) {
			return nil, errors.New("Box download redirect was rejected. Only HTTPS Box content domains are supported.")
		}
		res, e = c.do(deadline, "GET", location, nil, nil, false, 120*time.Second)
		if e != nil {
			return nil, e
		}
	}
	defer res.Body.Close()
	if res.Header.Get("Retry-After") != "" {
		return nil, errors.New("Box file is not ready to download. Retry later; no automatic retry was made.")
	}
	if res.StatusCode != 200 {
		return nil, apiError(res.StatusCode, res.Header)
	}
	data, e := bounded(res.Body, maxFile)
	if e != nil {
		return nil, e
	}
	if int64(len(data)) != *v.Size {
		return nil, errors.New("Downloaded bytes do not match Box metadata. The file may have changed; retry the operation.")
	}
	if v.SHA1 != nil && *v.SHA1 != "" {
		sum := sha1.Sum(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), *v.SHA1) {
			return nil, errors.New("Downloaded checksum does not match Box metadata. The file may have changed; retry the operation.")
		}
	}
	f, e := ctx.Upload(v.Name, fileMIME(res.Header.Get("Content-Type")), data)
	if e != nil || f == nil {
		return nil, errors.New("Could not store the downloaded file in the platform.")
	}
	return &DownloadFileOut{File: f, Item: v}, nil
}
