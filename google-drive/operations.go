package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/google-drive/schema"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

const googleOrigin = "https://www.googleapis.com"
const fileFields = "id,name,mimeType,size,modifiedTime,webViewLink,parents,trashed"
const maxUpload = 5 << 20
const maxDownload = 32 << 20

func (a *api) list(ctx context.Context, in *ListFilesIn) (*ListFilesOut, error) {
	size := in.PageSize
	if size == 0 {
		size = 100
	}
	if size < 1 || size > 1000 {
		return nil, fmt.Errorf("page_size must be between 1 and 1000")
	}
	query := in.Query
	if query == "" {
		query = "trashed = false"
	}
	q := url.Values{"q": {query}, "pageSize": {strconv.Itoa(size)}, "fields": {"nextPageToken,files(" + fileFields + ")"}, "spaces": {"drive"}, "supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"}}
	if in.PageToken != "" {
		q.Set("pageToken", in.PageToken)
	}
	var r struct {
		Files         []schema.FileInfo `json:"files"`
		NextPageToken string            `json:"nextPageToken"`
	}
	e := a.json(ctx, "GET", googleOrigin, "/drive/v3/files", q, nil, &r)
	if r.Files == nil {
		r.Files = []schema.FileInfo{}
	}
	return &ListFilesOut{Files: r.Files, NextPageToken: r.NextPageToken}, e
}
func (a *api) upload(ctx context.Context, name, mimeType, parent string, data []byte) (*UploadFileOut, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("file name is required")
	}
	if len(data) > maxUpload {
		return nil, fmt.Errorf("multipart upload is limited to 5 MiB; resumable upload is not supported")
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if _, _, e := mime.ParseMediaType(mimeType); e != nil || strings.ContainsAny(mimeType, "\r\n") {
		return nil, fmt.Errorf("invalid file MIME type")
	}
	metadata := map[string]any{"name": name}
	if parent != "" {
		metadata["parents"] = []string{parent}
	}
	meta, e := json.Marshal(metadata)
	if e != nil {
		return nil, fmt.Errorf("invalid upload metadata")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, e := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	if e != nil {
		return nil, e
	}
	if _, e = part.Write(meta); e != nil {
		return nil, e
	}
	part, e = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {mimeType}})
	if e != nil {
		return nil, e
	}
	if _, e = part.Write(data); e != nil {
		return nil, e
	}
	if e = mw.Close(); e != nil {
		return nil, e
	}
	raw, _, e := a.request(ctx, "POST", googleOrigin, "/upload/drive/v3/files", url.Values{"uploadType": {"multipart"}, "supportsAllDrives": {"true"}, "fields": {"id,name,mimeType"}}, body.Bytes(), "multipart/related; boundary="+mw.Boundary(), jsonLimit)
	if e != nil {
		return nil, e
	}
	var r schema.FileInfo
	if json.Unmarshal(raw, &r) != nil || r.ID == "" {
		return nil, fmt.Errorf("Google returned invalid upload metadata")
	}
	return &UploadFileOut{FileID: r.ID, Name: r.Name, MimeType: r.MIMEType}, nil
}
func (a *api) download(ctx context.Context, id string) ([]byte, string, string, error) {
	id, e := segment(id, "file_id")
	if e != nil {
		return nil, "", "", e
	}
	p := "/drive/v3/files/" + id
	var meta schema.FileInfo
	if e = a.json(ctx, "GET", googleOrigin, p, url.Values{"fields": {"id,name,mimeType,size"}, "supportsAllDrives": {"true"}}, nil, &meta); e != nil {
		return nil, "", "", e
	}
	if strings.HasPrefix(meta.MIMEType, "application/vnd.google-apps.") {
		return nil, "", "", fmt.Errorf("Google Workspace files require export_file with an export MIME type")
	}
	if meta.Size != "" {
		size, e := strconv.ParseInt(meta.Size, 10, 64)
		if e != nil || size < 0 {
			return nil, "", "", fmt.Errorf("Google returned an invalid file size")
		}
		if size > maxDownload {
			return nil, "", "", fmt.Errorf("download exceeds 32 MiB")
		}
	}
	raw, _, e := a.request(ctx, "GET", googleOrigin, p, url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}}, nil, "", maxDownload)
	return raw, meta.Name, meta.MIMEType, e
}
func (a *api) export(ctx context.Context, id, mimeType string) ([]byte, error) {
	id, e := segment(id, "file_id")
	if e != nil {
		return nil, e
	}
	if _, _, e = mime.ParseMediaType(mimeType); e != nil {
		return nil, fmt.Errorf("valid export MIME type is required")
	}
	raw, _, e := a.request(ctx, "GET", googleOrigin, "/drive/v3/files/"+id+"/export", url.Values{"mimeType": {mimeType}}, nil, "", 10<<20)
	return raw, e
}
func (a *api) poll(ctx context.Context, _ Cred, cursor string, emit func(string, *FileChangedEvent) error) (string, error) {
	if cursor == "" {
		var r struct {
			Token string `json:"startPageToken"`
		}
		e := a.json(ctx, "GET", googleOrigin, "/drive/v3/changes/startPageToken", url.Values{"supportsAllDrives": {"true"}}, nil, &r)
		if e != nil {
			return cursor, e
		}
		if r.Token == "" {
			return cursor, fmt.Errorf("Google returned no start page token")
		}
		return r.Token, nil
	}
	page := cursor
	seen := map[string]bool{}
	for n := 0; n < 1000; n++ {
		if seen[page] {
			return cursor, fmt.Errorf("Google repeated a changes page token")
		}
		seen[page] = true
		var r struct {
			Changes []struct {
				FileID  string          `json:"fileId"`
				Time    string          `json:"time"`
				Removed bool            `json:"removed"`
				File    schema.FileInfo `json:"file"`
			} `json:"changes"`
			Next  string `json:"nextPageToken"`
			Start string `json:"newStartPageToken"`
		}
		q := url.Values{"pageToken": {page}, "pageSize": {"1000"}, "spaces": {"drive"}, "includeRemoved": {"true"}, "supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"}, "fields": {"nextPageToken,newStartPageToken,changes(fileId,time,removed,file(id,name,mimeType,trashed))"}}
		if e := a.json(ctx, "GET", googleOrigin, "/drive/v3/changes", q, nil, &r); e != nil {
			return cursor, e
		}
		for i, c := range r.Changes {
			if c.FileID == "" {
				continue
			}
			ev := &FileChangedEvent{FileID: c.FileID, Name: c.File.Name, MimeType: c.File.MIMEType, Time: c.Time, Removed: c.Removed, Trashed: c.File.Trashed}
			if e := emit(eventKey(page, strconv.Itoa(i), c.FileID, c.Time), ev); e != nil {
				return cursor, e
			}
		}
		if r.Next == "" {
			if r.Start == "" {
				return cursor, fmt.Errorf("Google returned no final changes cursor")
			}
			return r.Start, nil
		}
		page = r.Next
	}
	return cursor, fmt.Errorf("change backlog exceeds 1000 pages; cursor has not advanced")
}
