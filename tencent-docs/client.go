package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxResponse = 16 << 20

type client struct {
	base string
	http *http.Client
	cred Cred
}

func newClient(base string, cred Cred) *client {
	return &client{base: strings.TrimRight(base, "/"), cred: cred, http: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// v2 requires an explicit ret envelope. v3 has both direct and enveloped responses in
// the official examples; reject nonzero ret/code in either shape before decoding.
func (c *client) request(ctx context.Context, method, path string, q url.Values, body any, v2 bool, out any) error {
	if strings.TrimSpace(c.cred.AccessToken) == "" || strings.TrimSpace(c.cred.ClientID) == "" || strings.TrimSpace(c.cred.OpenID) == "" {
		return errors.New("access_token, client_id and open_id are required")
	}
	u, err := url.Parse(c.base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("invalid API base URL")
	}
	host := u.Hostname()
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !loopback {
		return errors.New("API base URL must use HTTPS; HTTP is allowed only on loopback")
	}
	var raw []byte
	contentType := "application/json"
	switch b := body.(type) {
	case nil:
	case url.Values:
		raw = []byte(b.Encode())
		contentType = "application/x-www-form-urlencoded"
	default:
		raw, err = json.Marshal(body)
		if err != nil {
			return errors.New("could not encode request")
		}
	}
	endpoint := c.base + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("could not create request")
	}
	req.Header.Set("Access-Token", c.cred.AccessToken)
	req.Header.Set("Client-Id", c.cred.ClientID)
	req.Header.Set("Open-Id", c.cred.OpenID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Tencent Docs request failed; check the result before retrying a write")
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return errors.New("could not read Tencent Docs response")
	}
	if len(raw) > maxResponse {
		return errors.New("Tencent Docs response exceeds 16 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Tencent Docs HTTP %d; verify authorization, document version and rate limits", resp.StatusCode)
	}
	var env struct {
		Ret  *int            `json:"ret"`
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(raw, &env); err != nil {
		return errors.New("invalid Tencent Docs JSON response")
	}
	if env.Ret != nil && *env.Ret != 0 {
		return fmt.Errorf("Tencent Docs business error %d", *env.Ret)
	}
	if env.Code != nil && *env.Code != 0 {
		return fmt.Errorf("Tencent Docs business error %d", *env.Code)
	}
	if v2 && env.Ret == nil {
		return errors.New("missing Tencent Docs response status")
	}
	if env.Ret != nil || env.Code != nil {
		if len(env.Data) > 0 && string(env.Data) != "null" {
			raw = env.Data
		} else if v2 {
			return errors.New("missing Tencent Docs response data")
		}
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return errors.New("unexpected Tencent Docs response shape")
	}
	return nil
}
func validID(s string) error {
	if strings.TrimSpace(s) == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\?#\r\n") {
		return errors.New("provide an API resource ID, not a URL or path")
	}
	return nil
}
func resourcePath(prefix string, ids ...string) (string, error) {
	for _, id := range ids {
		if err := validID(id); err != nil {
			return "", err
		}
		prefix += "/" + url.PathEscape(id)
	}
	return prefix, nil
}
func page(offset, limit, max int) (int, error) {
	if offset < 0 {
		return 0, errors.New("offset must be nonnegative")
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > max {
		return 0, fmt.Errorf("limit must be between 1 and %d", max)
	}
	return limit, nil
}
func (c *client) FilesList(ctx context.Context, in *FilesListIn) (*FilesListOut, error) {
	limit, err := page(in.Offset, in.Limit, 100)
	if err != nil {
		return nil, err
	}
	path := "/openapi/drive/v2/folders/"
	if in.FolderID != "" && in.FolderID != "/" {
		path, err = resourcePath("/openapi/drive/v2/folders", in.FolderID)
		if err != nil {
			return nil, err
		}
	}
	var out struct {
		List []DocFile `json:"list"`
		Next int       `json:"next"`
	}
	err = c.request(ctx, "GET", path, url.Values{"start": {strconv.Itoa(in.Offset)}, "limit": {strconv.Itoa(limit)}, "sortType": {"browse"}, "asc": {"0"}}, nil, true, &out)
	if err != nil {
		return nil, err
	}
	if out.List == nil {
		return nil, errors.New("missing file list")
	}
	return &FilesListOut{Files: out.List, Next: out.Next}, nil
}
func (c *client) FilesSearch(ctx context.Context, in *FilesSearchIn) (*FilesSearchOut, error) {
	if strings.TrimSpace(in.Query) == "" {
		return nil, errors.New("query is required")
	}
	limit, err := page(in.Offset, in.Limit, 50)
	if err != nil {
		return nil, err
	}
	var out struct {
		List    []DocFile `json:"list"`
		Next    int       `json:"next"`
		HasMore bool      `json:"hasMore"`
		Total   int       `json:"total"`
	}
	err = c.request(ctx, "GET", "/openapi/drive/v2/search", url.Values{"searchKey": {in.Query}, "searchType": {"title"}, "offset": {strconv.Itoa(in.Offset)}, "size": {strconv.Itoa(limit)}}, nil, true, &out)
	if err != nil {
		return nil, err
	}
	if out.List == nil {
		return nil, errors.New("missing search result list")
	}
	return &FilesSearchOut{Files: out.List, Next: out.Next, HasMore: out.HasMore, Total: out.Total}, nil
}
func (c *client) FileGet(ctx context.Context, in *FileGetIn) (*FileGetOut, error) {
	path, err := resourcePath("/openapi/drive/v2/files", in.FileID)
	if err != nil {
		return nil, err
	}
	var file DocFile
	if err = c.request(ctx, "GET", path+"/metadata", nil, nil, true, &file); err != nil {
		return nil, err
	}
	if file.ID == "" {
		return nil, errors.New("missing file metadata")
	}
	return &FileGetOut{File: file}, nil
}
func (c *client) FileCreate(ctx context.Context, in *FileCreateIn) (*FileCreateOut, error) {
	if strings.TrimSpace(in.Title) == "" || utf8.RuneCountInString(in.Title) > 36 {
		return nil, errors.New("title must contain 1 to 36 characters")
	}
	switch in.FileType {
	case "doc", "sheet", "smartsheet":
	default:
		return nil, errors.New("file_type must be doc, sheet or smartsheet")
	}
	form := url.Values{"title": {in.Title}, "type": {in.FileType}}
	if in.FolderID != "" && in.FolderID != "/" {
		if err := validID(in.FolderID); err != nil {
			return nil, err
		}
		form.Set("folderID", in.FolderID)
	}
	var file DocFile
	if err := c.request(ctx, "POST", "/openapi/drive/v2/files", nil, form, true, &file); err != nil {
		return nil, err
	}
	if file.ID == "" {
		return nil, errors.New("Tencent Docs returned no created file ID; verify creation before retrying")
	}
	return &FileCreateOut{File: file}, nil
}
func (c *client) SheetsList(ctx context.Context, in *SheetsListIn) (*SheetsListOut, error) {
	path, err := resourcePath("/openapi/spreadsheet/v3/files", in.FileID)
	if err != nil {
		return nil, err
	}
	var data struct {
		Properties []struct {
			ID    string `json:"sheetId"`
			Title string `json:"title"`
			Rows  int    `json:"rowCount"`
			Cols  int    `json:"columnCount"`
		} `json:"properties"`
	}
	if err = c.request(ctx, "GET", path, nil, nil, false, &data); err != nil {
		return nil, err
	}
	if data.Properties == nil {
		return nil, errors.New("missing worksheet properties")
	}
	out := &SheetsListOut{Sheets: []Worksheet{}}
	for _, s := range data.Properties {
		if s.ID == "" {
			return nil, errors.New("missing worksheet ID")
		}
		out.Sheets = append(out.Sheets, Worksheet{SheetID: s.ID, Title: s.Title, RowCount: s.Rows, ColumnCount: s.Cols})
	}
	return out, nil
}

var rangePattern = regexp.MustCompile(`^([A-Za-z]{1,3})([1-9][0-9]{0,6})(?::([A-Za-z]{1,3})([1-9][0-9]{0,6}))?$`)

func validRange(s string) error {
	m := rangePattern.FindStringSubmatch(s)
	if m == nil {
		return errors.New("range must be a bounded A1 range, such as A1:C10")
	}
	col := func(s string) int {
		v := 0
		for _, r := range strings.ToUpper(s) {
			v = v*26 + int(r-'A'+1)
		}
		return v
	}
	start, _ := strconv.Atoi(m[2])
	end := start
	c1 := col(m[1])
	c2 := c1
	if m[3] != "" {
		end, _ = strconv.Atoi(m[4])
		c2 = col(m[3])
	}
	rows, cols := end-start+1, c2-c1+1
	if rows < 1 || cols < 1 || rows > 1000 || cols > 200 || rows*cols > 10000 {
		return errors.New("range must have at most 1000 rows, 200 columns and 10000 cells")
	}
	return nil
}
func (c *client) SheetRangeGet(ctx context.Context, in *SheetRangeGetIn) (*SheetRangeGetOut, error) {
	if err := validRange(in.Range); err != nil {
		return nil, err
	}
	path, err := resourcePath("/openapi/spreadsheet/v3/files", in.FileID, in.SheetID, in.Range)
	if err != nil {
		return nil, err
	}
	var data struct {
		Grid map[string]any `json:"gridData"`
	}
	if err = c.request(ctx, "GET", path, nil, nil, false, &data); err != nil {
		return nil, err
	}
	if data.Grid == nil {
		return nil, errors.New("missing gridData")
	}
	return &SheetRangeGetOut{GridData: data.Grid}, nil
}
func (c *client) SheetTextWrite(ctx context.Context, in *SheetTextWriteIn) (*SheetTextWriteOut, error) {
	path, err := resourcePath("/openapi/spreadsheet/v3/files", in.FileID)
	if err != nil {
		return nil, err
	}
	if err = validID(in.SheetID); err != nil {
		return nil, err
	}
	if in.StartRow < 0 || in.StartColumn < 0 || in.StartRow > 1000000 || in.StartColumn > 1000000 {
		return nil, errors.New("start coordinates must be nonnegative and bounded")
	}
	if len(in.Rows) < 1 || len(in.Rows) > 1000 {
		return nil, errors.New("rows must contain 1 to 1000 rows")
	}
	cols := len(in.Rows[0].Values)
	if cols < 1 || cols > 200 || cols*len(in.Rows) > 10000 {
		return nil, errors.New("write must contain at most 200 columns and 10000 cells")
	}
	rows := make([]any, len(in.Rows))
	size := 0
	for i, row := range in.Rows {
		if len(row.Values) != cols {
			return nil, errors.New("rows must form a rectangular matrix")
		}
		cells := make([]any, cols)
		for j, v := range row.Values {
			size += len(v)
			cells[j] = map[string]any{"cellValue": map[string]string{"text": v}}
		}
		rows[i] = map[string]any{"values": cells}
	}
	if size > 4<<20 {
		return nil, errors.New("text payload exceeds 4 MiB")
	}
	body := map[string]any{"requests": []any{map[string]any{"updateRangeRequest": map[string]any{"sheetId": in.SheetID, "gridData": map[string]any{"startRow": in.StartRow, "startColumn": in.StartColumn, "rows": rows}}}}}
	var data struct {
		Responses []json.RawMessage `json:"responses"`
	}
	if err = c.request(ctx, "POST", path+"/batchUpdate", nil, body, false, &data); err != nil {
		return nil, err
	}
	if len(data.Responses) != 1 {
		return nil, errors.New("missing update response; verify write before retrying")
	}
	var response struct {
		Update *struct {
			UpdatedCells *int `json:"updatedCells"`
		} `json:"updateRangeResponse"`
	}
	if json.Unmarshal(data.Responses[0], &response) != nil || response.Update == nil || response.Update.UpdatedCells == nil {
		return nil, errors.New("missing range update acknowledgement")
	}
	count := *response.Update.UpdatedCells
	expected := cols * len(in.Rows)
	if count < 0 || count > expected {
		return nil, errors.New("invalid updated cell count")
	}
	return &SheetTextWriteOut{Updated: count == expected, UpdatedCells: count}, nil
}
func (c *client) DocumentGet(ctx context.Context, in *DocumentGetIn) (*DocumentGetOut, error) {
	path, err := resourcePath("/openapi/doc/v3", in.FileID)
	if err != nil {
		return nil, err
	}
	var data struct {
		Document map[string]any `json:"document"`
		Version  *int           `json:"version"`
	}
	if err = c.request(ctx, "GET", path, nil, nil, false, &data); err != nil {
		return nil, err
	}
	if data.Document == nil || data.Version == nil {
		return nil, errors.New("missing document or version")
	}
	return &DocumentGetOut{Document: data.Document, Version: *data.Version}, nil
}
func (c *client) DocumentInsertText(ctx context.Context, in *DocumentInsertTextIn) (*DocumentInsertTextOut, error) {
	path, err := resourcePath("/openapi/doc/v3", in.FileID)
	if err != nil {
		return nil, err
	}
	if in.Index < 0 || in.Version < 0 || in.Text == "" || len(in.Text) > 1<<20 {
		return nil, errors.New("text must be nonempty and at most 1 MiB; index/version must be nonnegative")
	}
	body := map[string]any{"version": in.Version, "requests": []any{map[string]any{"insertText": map[string]any{"text": in.Text, "location": map[string]int{"index": in.Index}}}}}
	var out map[string]any
	if err = c.request(ctx, "POST", path+":batchUpdate", nil, body, false, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("missing document update response")
	}
	return &DocumentInsertTextOut{Updated: true}, nil
}
func (c *client) HealthCheck(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	_, err := c.FilesList(ctx, &FilesListIn{Limit: 1})
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "File read authorization succeeded; other operations require their own scopes."}, nil
}
