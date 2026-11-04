package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func sign(appID, key, method, uri, date string, b []byte) string {
	bodyHash := ""
	if len(b) > 0 {
		sum := sha256.Sum256(b)
		bodyHash = hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("KSO-1" + method + uri + "application/json" + date + bodyHash))
	return "KSO-1 " + appID + ":" + hex.EncodeToString(mac.Sum(nil))
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func filePath(d, f string) (string, error) {
	if !idPattern.MatchString(d) || !idPattern.MatchString(f) {
		return "", errors.New("Provide WPS drive and file IDs without paths, query strings or URL escapes.")
	}
	return "/v7/drives/" + d + "/files/" + f, nil
}
func validName(s string) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s) && len([]rune(s)) <= 240 && !strings.ContainsAny(s, `\/|":*?<>`) && s != "." && s != ".." && strings.IndexFunc(s, unicode.IsControl) < 0
}
func (c *client) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	b, e := c.request(ctx, method, path, q, body)
	if e != nil {
		return e
	}
	var envelope struct {
		Code *int64          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if e = decode(b, &envelope); e != nil {
		return e
	}
	if envelope.Code == nil {
		return errors.New("WPS365 returned an envelope without a status code.")
	}
	if *envelope.Code != 0 {
		return fmt.Errorf("WPS365 API error %d. Check token, API permissions, resource access and request parameters; no automatic retry was made.", *envelope.Code)
	}
	return decode(envelope.Data, out)
}
func validFile(f File) error {
	if !idPattern.MatchString(f.ID) || !idPattern.MatchString(f.DriveID) || f.Name == "" || f.Type == "" || f.Size < 0 || f.Version < 0 {
		return errors.New("WPS365 returned incomplete or invalid file metadata.")
	}
	return nil
}
func (c *client) healthCheck(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	var app struct {
		Name      string `json:"name"`
		CompanyID string `json:"company_id"`
	}
	e := c.call(ctx, "GET", "/v7/developer/applications/current", nil, nil, &app)
	if e == nil && (app.Name == "" || app.CompanyID == "") {
		e = errors.New("WPS365 returned incomplete current application metadata.")
	}
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "WPS365 current application read succeeded. File permissions must be verified separately."}, nil
}
func (c *client) listFiles(ctx context.Context, in *ListFilesIn) (*ListFilesOut, error) {
	p, e := filePath(in.DriveID, in.ParentID)
	if e != nil {
		return nil, e
	}
	size := in.PageSize
	if size == 0 {
		size = 100
	}
	if size < 1 || size > 500 {
		return nil, errors.New("Page size must be between 1 and 500.")
	}
	if !cursorOK(in.PageToken) {
		return nil, errors.New("Invalid WPS page token.")
	}
	q := url.Values{"page_size": {strconv.FormatInt(size, 10)}}
	if in.PageToken != "" {
		q.Set("page_token", in.PageToken)
	}
	var w struct {
		Items []File `json:"items"`
		Next  string `json:"next_page_token"`
	}
	if e = c.call(ctx, "GET", p+"/children", q, nil, &w); e != nil {
		return nil, e
	}
	if w.Items == nil || len(w.Items) > int(size) || !cursorOK(w.Next) || (w.Next != "" && w.Next == in.PageToken) {
		return nil, errors.New("WPS365 returned an invalid file page or repeated page token.")
	}
	for _, f := range w.Items {
		if e = validFile(f); e != nil {
			return nil, e
		}
	}
	return &ListFilesOut{Files: w.Items, NextPageToken: w.Next}, nil
}
func (c *client) getFile(ctx context.Context, in *GetFileIn) (*GetFileOut, error) {
	p, e := filePath(in.DriveID, in.FileID)
	if e != nil {
		return nil, e
	}
	var f File
	if e = c.call(ctx, "GET", p+"/meta", nil, nil, &f); e != nil {
		return nil, e
	}
	if e = validFile(f); e != nil {
		return nil, e
	}
	if f.ID != in.FileID || f.DriveID != in.DriveID {
		return nil, errors.New("WPS365 returned metadata for a different file.")
	}
	return &GetFileOut{File: f}, nil
}
func (c *client) createFolder(ctx context.Context, in *CreateFolderIn) (*CreateFolderOut, error) {
	p, e := filePath(in.DriveID, in.ParentID)
	if e != nil {
		return nil, e
	}
	if !validName(in.Name) {
		return nil, errors.New("Provide a folder name of 1 to 240 characters without reserved filename characters or controls.")
	}
	var f File
	e = c.call(ctx, "POST", p+"/create", nil, map[string]any{"file_type": "folder", "name": in.Name, "on_name_conflict": "fail"}, &f)
	if e != nil {
		return nil, e
	}
	if e = validFile(f); e != nil {
		return nil, e
	}
	if f.DriveID != in.DriveID || f.ParentID != in.ParentID || f.Name != in.Name || f.Type != "folder" {
		return nil, errors.New("WPS365 folder receipt did not match the request. Inspect the destination before retrying.")
	}
	return &CreateFolderOut{File: f}, nil
}
func (c *client) renameFile(ctx context.Context, in *RenameFileIn) (*RenameFileOut, error) {
	p, e := filePath(in.DriveID, in.FileID)
	if e != nil {
		return nil, e
	}
	if !validName(in.Name) {
		return nil, errors.New("Provide a name of 1 to 240 characters without reserved filename characters or controls.")
	}
	var f File
	e = c.call(ctx, "POST", p+"/rename", nil, map[string]any{"dst_name": in.Name}, &f)
	if e != nil {
		return nil, e
	}
	if e = validFile(f); e != nil {
		return nil, e
	}
	if f.ID != in.FileID || f.DriveID != in.DriveID || f.Name != in.Name {
		return nil, errors.New("WPS365 rename receipt did not match the request. Inspect the file before retrying.")
	}
	return &RenameFileOut{File: f}, nil
}
func (c *client) getFilePath(ctx context.Context, in *GetFilePathIn) (*GetFilePathOut, error) {
	p, e := filePath(in.DriveID, in.FileID)
	if e != nil {
		return nil, e
	}
	var out GetFilePathOut
	if e = c.call(ctx, "GET", p+"/path", nil, nil, &out); e != nil {
		return nil, e
	}
	if out.LocationID == "" || out.LocationType == "" || out.Paths == nil {
		return nil, errors.New("WPS365 returned an incomplete file path.")
	}
	for _, v := range out.Paths {
		if !idPattern.MatchString(v.FileID) || v.Name == "" {
			return nil, errors.New("WPS365 returned an invalid file path segment.")
		}
	}
	return &out, nil
}
func (c *client) getDownloadInfo(ctx context.Context, in *GetDownloadInfoIn) (*GetDownloadInfoOut, error) {
	p, e := filePath(in.DriveID, in.FileID)
	if e != nil {
		return nil, e
	}
	var out GetDownloadInfoOut
	if e = c.call(ctx, "GET", p+"/download", url.Values{"with_hash": {"true"}, "internal": {"false"}}, nil, &out); e != nil {
		return nil, e
	}
	u, e := url.Parse(out.URL)
	if e != nil || len(out.URL) > 16384 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("WPS365 returned an invalid HTTPS download URL.")
	}
	if out.Hashes == nil {
		out.Hashes = []ContentHash{}
	}
	for _, h := range out.Hashes {
		if h.Type == "" || h.Sum == "" {
			return nil, errors.New("WPS365 returned incomplete content hashes.")
		}
	}
	return &out, nil
}
