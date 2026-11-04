package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/dropbox/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

func validPath(s string, root, ids bool) bool {
	if !utf8.ValidString(s) || len(s) > 4096 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	if s == "" {
		return root
	}
	if ids && strings.HasPrefix(s, "id:") {
		return len(s) > 3 && !strings.ContainsAny(s, "/\\")
	}
	if !strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.Contains(s, "\\") {
		return false
	}
	for _, v := range strings.Split(s[1:], "/") {
		if v == "" || v == "." || v == ".." {
			return false
		}
	}
	return true
}
func validCursor(s string) bool {
	return s != "" && len(s) <= 16384 && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
func validEntry(v *schema.Entry, concreteFile bool) error {
	if concreteFile && v.Kind == "" {
		v.Kind = "file"
	}
	if !validName(v.Name) {
		return errors.New("Dropbox returned an invalid item name.")
	}
	switch v.Kind {
	case "file":
		if v.ID == nil || !strings.HasPrefix(*v.ID, "id:") || len(*v.ID) <= 3 || v.Size == nil || *v.Size < 0 || v.Rev == nil || *v.Rev == "" {
			return errors.New("Dropbox returned incomplete file metadata.")
		}
	case "folder":
		if v.ID == nil || !strings.HasPrefix(*v.ID, "id:") || len(*v.ID) <= 3 {
			return errors.New("Dropbox returned incomplete folder metadata.")
		}
		v.Size = nil
		v.Rev = nil
		v.IsDownloadable = nil
		v.ServerModified = nil
	case "deleted":
		v.ID = nil
		v.Size = nil
		v.Rev = nil
		v.IsDownloadable = nil
		v.ServerModified = nil
	default:
		return errors.New("Dropbox returned an unknown metadata tag.")
	}
	if concreteFile && v.Kind != "file" {
		return errors.New("Dropbox did not return a file.")
	}
	return nil
}
func (c *client) rpc(ctx context.Context, path string, body any, out any) error {
	if body == nil {
		body = json.RawMessage("null")
	}
	return c.json(ctx, "POST", "/2/"+path, nil, body, out)
}
func (c *client) health_check(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		AccountID string `json:"account_id"`
	}
	e := c.rpc(ctx, "users/get_current_account", nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if !strings.HasPrefix(r.AccountID, "dbid:") || len(r.AccountID) <= 5 {
		return &HealthCheckOut{Message: "Dropbox did not identify an authenticated account."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Dropbox account identity verified. File permissions are checked by each operation."}, nil
}

type listResult struct {
	Entries []schema.Entry `json:"entries"`
	Cursor  string         `json:"cursor"`
	HasMore *bool          `json:"has_more"`
}

func (c *client) list(ctx context.Context, path string, body any, previous string) (*listResult, error) {
	var r listResult
	if e := c.rpc(ctx, path, body, &r); e != nil {
		return nil, e
	}
	if r.Entries == nil || r.HasMore == nil || !validCursor(r.Cursor) || (*r.HasMore && r.Cursor == previous) {
		return nil, errors.New("Dropbox returned invalid list pagination.")
	}
	for i := range r.Entries {
		if e := validEntry(&r.Entries[i], false); e != nil {
			return nil, e
		}
	}
	return &r, nil
}
func (c *client) list_folder(ctx plugin.Ctx, in *ListFolderIn) (*ListFolderOut, error) {
	if !validPath(in.Path, true, true) {
		return nil, errors.New("Use an absolute folder path, item ID, or an empty string for the root.")
	}
	if in.Options.Limit != nil && (*in.Options.Limit < 1 || *in.Options.Limit > 2000) {
		return nil, errors.New("Page size must be between 1 and 2000.")
	}
	body := map[string]any{"path": in.Path}
	if in.Options.Limit != nil {
		body["limit"] = *in.Options.Limit
	} else {
		body["limit"] = 100
	}
	if in.Options.Recursive != nil {
		body["recursive"] = *in.Options.Recursive
	}
	if in.Options.IncludeDeleted != nil {
		body["include_deleted"] = *in.Options.IncludeDeleted
	}
	r, e := c.list(ctx, "files/list_folder", body, "")
	if e != nil {
		return nil, e
	}
	return &ListFolderOut{Entries: r.Entries, Cursor: r.Cursor, HasMore: *r.HasMore}, nil
}
func (c *client) continue_list(ctx plugin.Ctx, in *ContinueListIn) (*ContinueListOut, error) {
	if !validCursor(in.Cursor) {
		return nil, errors.New("Provide a nonempty list cursor of at most 16384 bytes.")
	}
	r, e := c.list(ctx, "files/list_folder/continue", map[string]string{"cursor": in.Cursor}, in.Cursor)
	if e != nil {
		return nil, e
	}
	return &ContinueListOut{Entries: r.Entries, Cursor: r.Cursor, HasMore: *r.HasMore}, nil
}
func (c *client) get_metadata(ctx plugin.Ctx, in *GetMetadataIn) (*GetMetadataOut, error) {
	if !validPath(in.Path, false, true) {
		return nil, errors.New("Provide an absolute item path or item ID; the root has no metadata endpoint.")
	}
	var v schema.Entry
	if e := c.rpc(ctx, "files/get_metadata", map[string]any{"path": in.Path, "include_deleted": true}, &v); e != nil {
		return nil, e
	}
	if e := validEntry(&v, false); e != nil {
		return nil, e
	}
	if strings.HasPrefix(in.Path, "id:") && (v.ID == nil || *v.ID != in.Path) {
		return nil, errors.New("Dropbox returned a different item ID.")
	}
	return &GetMetadataOut{Entry: v}, nil
}

type searchResult struct {
	Matches []struct {
		Metadata struct {
			Tag      string       `json:".tag"`
			Metadata schema.Entry `json:"metadata"`
		} `json:"metadata"`
	} `json:"matches"`
	Cursor  string `json:"cursor"`
	HasMore *bool  `json:"has_more"`
}

func (c *client) find(ctx context.Context, path string, body any, previous string) ([]schema.Entry, string, bool, error) {
	var r searchResult
	if e := c.rpc(ctx, path, body, &r); e != nil {
		return nil, "", false, e
	}
	if r.Matches == nil || r.HasMore == nil || (*r.HasMore && (!validCursor(r.Cursor) || r.Cursor == previous)) {
		return nil, "", false, errors.New("Dropbox returned invalid search pagination.")
	}
	entries := []schema.Entry{}
	for _, m := range r.Matches {
		if m.Metadata.Tag != "metadata" {
			return nil, "", false, errors.New("Dropbox returned an unsupported search metadata tag.")
		}
		v := m.Metadata.Metadata
		if e := validEntry(&v, false); e != nil {
			return nil, "", false, e
		}
		entries = append(entries, v)
	}
	if !*r.HasMore {
		r.Cursor = ""
	}
	return entries, r.Cursor, *r.HasMore, nil
}
func (c *client) search(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	if strings.TrimSpace(in.Query) == "" || !utf8.ValidString(in.Query) || utf8.RuneCountInString(in.Query) > 1000 {
		return nil, errors.New("Search query must contain 1 to 1000 characters.")
	}
	if in.Options.Path != nil && !validPath(*in.Options.Path, true, true) {
		return nil, errors.New("Invalid search folder path.")
	}
	if in.Options.MaxResults != nil && (*in.Options.MaxResults < 1 || *in.Options.MaxResults > 1000) {
		return nil, errors.New("Search page size must be between 1 and 1000.")
	}
	entries, cursor, more, e := c.find(ctx, "files/search_v2", map[string]any{"query": in.Query, "options": in.Options}, "")
	return &SearchOut{Entries: entries, Cursor: cursor, HasMore: more}, e
}
func (c *client) continue_search(ctx plugin.Ctx, in *ContinueSearchIn) (*ContinueSearchOut, error) {
	if !validCursor(in.Cursor) {
		return nil, errors.New("Provide a nonempty search cursor of at most 16384 bytes.")
	}
	entries, cursor, more, e := c.find(ctx, "files/search/continue_v2", map[string]string{"cursor": in.Cursor}, in.Cursor)
	return &ContinueSearchOut{Entries: entries, Cursor: cursor, HasMore: more}, e
}

// Dropbox requires ASCII-safe JSON in HTTP headers, including surrogate pairs.
func asciiJSON(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", errors.New("Invalid file operation arguments.")
	}
	var out strings.Builder
	for _, r := range string(b) {
		if r < 127 {
			out.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&out, "\\u%04x", r)
		} else {
			a, z := utf16.EncodeRune(r)
			fmt.Fprintf(&out, "\\u%04x\\u%04x", a, z)
		}
	}
	return out.String(), nil
}
func (c *client) content(ctx context.Context, path string, arg any, data []byte) (*http.Response, error) {
	endpoint, e := c.endpoint(c.contentBase, "/2/files/"+path, nil)
	if e != nil {
		return nil, e
	}
	header, e := asciiJSON(arg)
	if e != nil {
		return nil, e
	}
	headers := http.Header{"Content-Type": {"application/octet-stream"}}
	headers.Set("Dropbox-API-Arg", header)
	return c.do(ctx, "POST", endpoint, data, headers, true, 120*time.Second)
}
func (c *client) upload_file(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
	if !validPath(in.Path, false, false) {
		return nil, errors.New("Provide an absolute destination file path without trailing slash.")
	}
	if in.File == nil || in.File.Size < 0 || in.File.Size > maxFile {
		return nil, errors.New("Provide a platform file of at most 32 MiB.")
	}
	b, e := in.File.Blob(ctx)
	if e != nil {
		return nil, errors.New("Could not read the platform file.")
	}
	if len(b) > maxFile {
		return nil, errors.New("Platform file exceeds 32 MiB.")
	}
	res, e := c.content(ctx, "upload", map[string]any{"path": in.Path, "mode": "add", "autorename": false, "strict_conflict": true}, b)
	if e != nil {
		return nil, e
	}
	var v schema.Entry
	if e = jsonResponse(res, &v); e != nil {
		return nil, e
	}
	if validEntry(&v, true) != nil || v.Size == nil || *v.Size != int64(len(b)) {
		return nil, errors.New("Dropbox returned an invalid upload receipt. Inspect the destination before retrying.")
	}
	return &UploadFileOut{Entry: v}, nil
}
func (c *client) download_file(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
	if !validPath(in.Path, false, true) {
		return nil, errors.New("Provide an absolute file path or file ID.")
	}
	res, e := c.content(ctx, "download", map[string]string{"path": in.Path}, nil)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, apiError(res.StatusCode, res.Header)
	}
	raw := res.Header.Get("Dropbox-API-Result")
	var v schema.Entry
	if len(raw) > 65536 || decode([]byte(raw), &v) != nil || validEntry(&v, true) != nil {
		return nil, errors.New("Dropbox returned invalid download metadata.")
	}
	if *v.Size > maxFile {
		return nil, errors.New("Dropbox file exceeds the 32 MiB download limit.")
	}
	if strings.HasPrefix(in.Path, "id:") && *v.ID != in.Path {
		return nil, errors.New("Dropbox returned a different file ID.")
	}
	b, e := bounded(res.Body, maxFile)
	if e != nil {
		return nil, e
	}
	if int64(len(b)) != *v.Size {
		return nil, errors.New("Downloaded byte count does not match Dropbox metadata.")
	}
	f, e := ctx.Upload(v.Name, fileMIME(res.Header.Get("Content-Type")), b)
	if e != nil || f == nil {
		return nil, errors.New("Could not store the downloaded file in the platform.")
	}
	return &DownloadFileOut{File: f, Entry: v}, nil
}
