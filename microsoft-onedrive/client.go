package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type upstreamDrive struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DriveType string `json:"driveType"`
	WebURL    string `json:"webUrl"`
}

func (d upstreamDrive) typed() (Drive, error) {
	if _, e := segment(d.ID); e != nil || d.DriveType == "" {
		return Drive{}, errors.New("Microsoft Graph returned incomplete drive metadata.")
	}
	return Drive{ID: d.ID, Name: d.Name, DriveType: d.DriveType, WebURL: d.WebURL}, nil
}
func drivePath(id string) (string, error) { s, e := segment(id); return "/v1.0/drives/" + s, e }
func itemPath(drive, id string) (string, error) {
	p, e := drivePath(drive)
	if e != nil {
		return "", e
	}
	s, e := segment(id)
	return p + "/items/" + s, e
}
func parentPath(drive, parent string) (string, error) {
	if parent == "" {
		p, e := drivePath(drive)
		return p + "/root", e
	}
	return itemPath(drive, parent)
}
func (c *client) getDrive(ctx context.Context, in *GetDriveIn) (*GetDriveOut, error) {
	path := "/v1.0/me/drive"
	var e error
	if in.DriveID != "" {
		path, e = drivePath(in.DriveID)
		if e != nil {
			return nil, e
		}
	}
	var d upstreamDrive
	if e = c.json(ctx, "GET", graphOrigin+path, nil, nil, &d, 200); e != nil {
		return nil, e
	}
	out, e := d.typed()
	if e == nil && in.DriveID != "" && out.ID != in.DriveID {
		return nil, errors.New("Microsoft Graph returned a different drive ID.")
	}
	return &GetDriveOut{Drive: out}, e
}

type upstreamItem struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Size     *int64          `json:"size"`
	WebURL   string          `json:"webUrl"`
	ETag     string          `json:"eTag"`
	Modified string          `json:"lastModifiedDateTime"`
	Folder   json.RawMessage `json:"folder"`
	Deleted  json.RawMessage `json:"deleted"`
	File     *struct {
		MIME string `json:"mimeType"`
	} `json:"file"`
	DownloadURL string `json:"@microsoft.graph.downloadUrl"`
}

func facet(b json.RawMessage) bool { return len(b) > 0 && string(b) != "null" }
func (r upstreamItem) typed() (DriveItem, error) {
	return r.project(false)
}
func (r upstreamItem) project(partial bool) (DriveItem, error) {
	if _, e := segment(r.ID); e != nil {
		return DriveItem{}, errors.New("Microsoft Graph returned an invalid drive item ID.")
	}
	deleted := facet(r.Deleted)
	if !partial && !deleted && (r.Name == "" || r.Size == nil) {
		return DriveItem{}, errors.New("Microsoft Graph returned incomplete drive item metadata.")
	}
	var size int64
	if r.Size != nil {
		size = *r.Size
		if size < 0 {
			return DriveItem{}, errors.New("Microsoft Graph returned a negative file size.")
		}
	}
	mime := ""
	if r.File != nil {
		mime = r.File.MIME
	}
	return DriveItem{ID: r.ID, Name: r.Name, Size: size, SizeKnown: r.Size != nil, WebURL: r.WebURL, Etag: r.ETag, LastModified: r.Modified, IsFolder: facet(r.Folder), Deleted: deleted, MimeType: mime}, nil
}

func drivePageURL(path, link string, q url.Values) (string, error) {
	if link == "" || !strings.HasSuffix(path, "/root/delta") {
		return pageURL(path, link, q)
	}
	u, err := url.Parse(link)
	root, unescapeErr := url.PathUnescape(strings.TrimSuffix(path, "/root/delta"))
	if err != nil || unescapeErr != nil || len(link) > 16384 || !graphURL(link) {
		return "", errors.New("Delta URL must use the global Graph origin and the same drive.")
	}
	// Graph's official examples use both /root/delta and /delta, plus the
	// delta(token='...') function form. Keep the drive ID fixed in all forms.
	for _, endpoint := range []string{root + "/root/delta", root + "/delta"} {
		if u.Path == endpoint {
			return link, nil
		}
		pattern := "^" + regexp.QuoteMeta(endpoint) + `\(token=(?:'[A-Za-z0-9._~+/=%-]+'|[A-Za-z0-9._~+/=%-]+)\)$`
		if regexp.MustCompile(pattern).MatchString(u.Path) {
			return link, nil
		}
	}
	return "", errors.New("Delta URL must identify the same drive and only its delta endpoint.")
}

type drivePage struct {
	Value []upstreamItem `json:"value"`
	Next  string         `json:"@odata.nextLink"`
	Delta string         `json:"@odata.deltaLink"`
}

func (c *client) readPage(ctx context.Context, path, link string, q url.Values) ([]DriveItem, string, string, error) {
	target, e := drivePageURL(path, link, q)
	if e != nil {
		return nil, "", "", e
	}
	var page drivePage
	if e = c.json(ctx, "GET", target, nil, nil, &page, 200); e != nil {
		return nil, "", "", e
	}
	if page.Value == nil {
		return nil, "", "", errors.New("Microsoft Graph response is missing the item collection.")
	}
	next := ""
	if page.Next != "" {
		if page.Next == target {
			return nil, "", "", errors.New("Microsoft Graph repeated the current pagination URL.")
		}
		next, e = drivePageURL(path, page.Next, nil)
		if e != nil {
			return nil, "", "", e
		}
	}
	delta := ""
	if page.Delta != "" {
		delta, e = drivePageURL(path, page.Delta, nil)
		if e != nil {
			return nil, "", "", e
		}
	}
	if next != "" && delta != "" {
		return nil, "", "", errors.New("Microsoft Graph returned both a next page and a delta checkpoint.")
	}
	out := make([]DriveItem, 0, len(page.Value))
	for _, row := range page.Value {
		item, e := row.project(strings.HasSuffix(path, "/root/delta"))
		if e != nil {
			return nil, "", "", e
		}
		out = append(out, item)
	}
	return out, next, delta, nil
}
func (c *client) listItems(ctx context.Context, in *ListItemsIn) (*ListItemsOut, error) {
	p, e := parentPath(in.DriveID, in.ParentID)
	if e != nil {
		return nil, e
	}
	items, next, _, e := c.readPage(ctx, p+"/children", in.NextLink, url.Values{"$top": {"100"}})
	return &ListItemsOut{Items: items, NextLink: next}, e
}
func (c *client) fetchItem(ctx context.Context, drive, id string) (upstreamItem, error) {
	p, e := itemPath(drive, id)
	if e != nil {
		return upstreamItem{}, e
	}
	var r upstreamItem
	e = c.json(ctx, "GET", graphOrigin+p, nil, nil, &r, 200)
	if e == nil && r.ID != id {
		return r, errors.New("Microsoft Graph returned a different item ID.")
	}
	return r, e
}
func (c *client) getItem(ctx context.Context, in *GetItemIn) (*GetItemOut, error) {
	r, e := c.fetchItem(ctx, in.DriveID, in.ItemID)
	if e != nil {
		return nil, e
	}
	out, e := r.typed()
	return &GetItemOut{Item: out}, e
}
func (c *client) searchItems(ctx context.Context, in *SearchItemsIn) (*SearchItemsOut, error) {
	p, e := drivePath(in.DriveID)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 1024 || strings.ContainsAny(in.Query, "\r\n\x00") {
		return nil, errors.New("Provide a search query of 1 to 1024 bytes.")
	}
	p += "/root/search(q='" + url.PathEscape(strings.ReplaceAll(in.Query, "'", "''")) + "')"
	items, next, _, e := c.readPage(ctx, p, in.NextLink, url.Values{"$top": {"100"}})
	return &SearchItemsOut{Items: items, NextLink: next}, e
}
func (c *client) delta(ctx context.Context, in *DeltaIn) (*DeltaOut, error) {
	p, e := drivePath(in.DriveID)
	if e != nil {
		return nil, e
	}
	rows, next, delta, e := c.readPage(ctx, p+"/root/delta", in.ContinuationURL, nil)
	if e == nil && next == "" && delta == "" {
		return nil, errors.New("Microsoft Graph delta response has no next page or checkpoint.")
	}
	return &DeltaOut{Items: rows, NextLink: next, DeltaLink: delta}, e
}
func (c *client) uploadFile(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
	p, e := parentPath(in.DriveID, in.ParentID)
	if e != nil {
		return nil, e
	}
	if in.File == nil || in.File.Size < 0 || in.File.Size > 4<<20 {
		return nil, errors.New("Provide a platform file of at most 4 MiB.")
	}
	name := in.File.Name
	if strings.TrimSpace(name) == "" || len(name) > 255 || strings.ContainsAny(name, "/\\:*?\"<>|\r\n\x00") || name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return nil, errors.New("Provide a valid OneDrive filename without paths or reserved characters.")
	}
	if !safeHeader(in.IfMatch) {
		return nil, errors.New("Invalid ETag.")
	}
	b, e := in.File.Blob(ctx)
	if e != nil {
		return nil, errors.New("Failed to read the platform file. Check file access.")
	}
	if len(b) > 4<<20 {
		return nil, errors.New("File content exceeds the 4 MiB upload limit.")
	}
	headers := http.Header{"Content-Type": {"application/octet-stream"}}
	if in.IfMatch != "" {
		headers.Set("If-Match", in.IfMatch)
	}
	raw, _, e := c.send(ctx, "PUT", graphOrigin+p+":/"+url.PathEscape(name)+":/content", b, headers, 200, 201)
	if e != nil {
		return nil, e
	}
	var r upstreamItem
	if e = decodeJSON(raw, &r); e != nil {
		return nil, e
	}
	out, e := r.typed()
	if e != nil || out.IsFolder || out.Deleted || out.Size != int64(len(b)) {
		return nil, errors.New("Microsoft Graph returned an invalid upload receipt. Inspect the drive before retrying.")
	}
	return &UploadFileOut{Item: out}, nil
}
func (c *client) getDownloadInfo(ctx context.Context, in *GetDownloadInfoIn) (*GetDownloadInfoOut, error) {
	r, e := c.fetchItem(ctx, in.DriveID, in.ItemID)
	if e != nil {
		return nil, e
	}
	out, e := r.typed()
	if e != nil {
		return nil, e
	}
	u, e := url.Parse(r.DownloadURL)
	if e != nil || r.File == nil || out.Deleted || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Fragment != "" || net.ParseIP(u.Hostname()) != nil || !strings.Contains(u.Hostname(), ".") || strings.HasSuffix(u.Hostname(), ".localhost") {
		return nil, errors.New("Microsoft Graph did not return a valid HTTPS download URL for this file.")
	}
	return &GetDownloadInfoOut{Item: out, DownloadURL: r.DownloadURL}, nil
}
