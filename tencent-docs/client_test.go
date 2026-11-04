package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testClient(t *testing.T, send func(*http.Request) string) *client {
	t.Helper()
	return &client{base: "https://docs.qq.com", cred: Cred{AccessToken: "token", ClientID: "client", OpenID: "user"}, http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Access-Token") != "token" || r.Header.Get("Client-Id") != "client" || r.Header.Get("Open-Id") != "user" {
			t.Fatalf("missing auth headers")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(send(r))), Request: r}, nil
	})}}
}
func TestFileCreateUsesForm(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		if r.Method != "POST" || r.URL.Path != "/openapi/drive/v2/files" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("bad request %s %s", r.Method, r.URL.Path)
		}
		r.ParseForm()
		if r.Form.Get("title") != "季度 & review" || r.Form.Get("type") != "sheet" {
			t.Fatalf("bad form %#v", r.Form)
		}
		return `{"ret":0,"data":{"ID":"id","title":"t","type":"sheet","url":"https://docs.qq.com/sheet/x"}}`
	})
	out, err := c.FileCreate(context.Background(), &FileCreateIn{Title: "季度 & review", FileType: "sheet"})
	if err != nil || out.File.ID != "id" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
func TestSearchPagination(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		q := r.URL.Query()
		if q.Get("searchKey") != "报告" || q.Get("offset") != "20" || q.Get("size") != "10" {
			t.Fatalf("query %v", q)
		}
		return `{"ret":0,"data":{"list":[{"ID":"id"}],"next":30,"total":45,"hasMore":true}}`
	})
	out, err := c.FilesSearch(context.Background(), &FilesSearchIn{Query: "报告", Offset: 20, Limit: 10})
	if err != nil || out.Next != 30 || !out.HasMore || out.Total != 45 || out.Files[0].ID != "id" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
func TestWriteTextV3(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		if r.URL.Path != "/openapi/spreadsheet/v3/files/id/batchUpdate" {
			t.Fatalf("path %s", r.URL.Path)
		}
		var b struct {
			Requests []struct {
				Update struct {
					SheetID string `json:"sheetId"`
					Grid    struct {
						StartRow    int `json:"startRow"`
						StartColumn int `json:"startColumn"`
						Rows        []struct {
							Values []struct {
								Value struct {
									Text string `json:"text"`
								} `json:"cellValue"`
							} `json:"values"`
						} `json:"rows"`
					} `json:"gridData"`
				} `json:"updateRangeRequest"`
			} `json:"requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Fatal(err)
		}
		u := b.Requests[0].Update
		if u.SheetID != "s" || u.Grid.StartRow != 0 || u.Grid.StartColumn != 2 || u.Grid.Rows[0].Values[0].Value.Text != "=text" {
			t.Fatalf("body %+v", b)
		}
		return `{"responses":[{"updateRangeResponse":{"updatedCells":1}}]}`
	})
	out, err := c.SheetTextWrite(context.Background(), &SheetTextWriteIn{FileID: "id", SheetID: "s", StartColumn: 2, Rows: []TextRow{{Values: []string{"=text"}}}})
	if err != nil || !out.Updated {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestReadOperationsAndHealth(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/openapi/drive/v2/folders/":
			return `{"ret":0,"data":{"list":[],"next":0}}`
		case "/openapi/drive/v2/folders/folder":
			if r.URL.Query().Get("start") != "20" {
				t.Fatal("missing offset")
			}
			return `{"ret":0,"data":{"list":[{"ID":"f"}],"next":40}}`
		case "/openapi/drive/v2/files/id/metadata":
			return `{"ret":0,"data":{"ID":"id","title":"Title"}}`
		case "/openapi/spreadsheet/v3/files/id":
			return `{"properties":[{"sheetId":"s","title":"Sheet","rowCount":10,"columnCount":4}]}`
		case "/openapi/spreadsheet/v3/files/id/s/A1:C2":
			return `{"ret":0,"data":{"gridData":{"startRow":0,"rows":[{"values":[{"cellValue":{"text":"value"}}]}]}}}`
		case "/openapi/doc/v3/id":
			return `{"document":{"type":"Document","children":[]},"version":10}`
		default:
			t.Fatalf("unexpected URL %s", r.URL)
			return ""
		}
	})
	ctx := context.Background()
	if out, err := c.FilesList(ctx, &FilesListIn{FolderID: "folder", Offset: 20}); err != nil || len(out.Files) != 1 || out.Next != 40 {
		t.Fatalf("files=%+v err=%v", out, err)
	}
	if out, err := c.FileGet(ctx, &FileGetIn{FileID: "id"}); err != nil || out.File.Title != "Title" {
		t.Fatalf("file=%+v err=%v", out, err)
	}
	if out, err := c.SheetsList(ctx, &SheetsListIn{FileID: "id"}); err != nil || out.Sheets[0].SheetID != "s" || out.Sheets[0].RowCount != 10 {
		t.Fatalf("sheets=%+v err=%v", out, err)
	}
	if out, err := c.SheetRangeGet(ctx, &SheetRangeGetIn{FileID: "id", SheetID: "s", Range: "A1:C2"}); err != nil || out.GridData["rows"] == nil {
		t.Fatalf("grid=%+v err=%v", out, err)
	}
	if out, err := c.DocumentGet(ctx, &DocumentGetIn{FileID: "id"}); err != nil || out.Version != 10 || out.Document["type"] != "Document" {
		t.Fatalf("doc=%+v err=%v", out, err)
	}
	if out, err := c.HealthCheck(ctx, &HealthCheckIn{}); err != nil || !out.OK {
		t.Fatalf("health=%+v err=%v", out, err)
	}
	c.cred.OpenID = ""
	if out, err := c.HealthCheck(ctx, &HealthCheckIn{}); err != nil || out.OK {
		t.Fatal("invalid credentials reported healthy")
	}
}
func TestDocumentInsertVersionAndIndex(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		if r.URL.Path != "/openapi/doc/v3/id:batchUpdate" {
			t.Fatal(r.URL.Path)
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		req := b["requests"].([]any)[0].(map[string]any)["insertText"].(map[string]any)
		if b["version"] != float64(42) || req["text"] != "hello" || req["location"].(map[string]any)["index"] != float64(0) {
			t.Fatalf("bad write %#v", b)
		}
		return `{}`
	})
	out, err := c.DocumentInsertText(context.Background(), &DocumentInsertTextIn{FileID: "id", Text: "hello", Version: 42})
	if err != nil || !out.Updated {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
func TestBusinessErrorsAndMalformedSuccess(t *testing.T) {
	for _, body := range []string{`{"ret":37019,"msg":"token secret"}`, `{"code":400006,"message":"token secret"}`, `{}`, `null`, `not json`, `{"ret":0}`, `{"ret":0,"data":{}}`} {
		t.Run(body, func(t *testing.T) {
			c := testClient(t, func(*http.Request) string { return body })
			out, err := c.FileGet(context.Background(), &FileGetIn{FileID: "id"})
			if err == nil || strings.Contains(err.Error(), "token secret") {
				t.Fatalf("out=%+v err=%v", out, err)
			}
		})
	}
	for _, status := range []int{302, 400, 401, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c := testClient(t, func(*http.Request) string { return "" })
			c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"message":"token secret"}`)), Header: make(http.Header)}, nil
			})
			_, err := c.FileCreate(context.Background(), &FileCreateIn{Title: "title", FileType: "doc"})
			if err == nil || calls != 1 || strings.Contains(err.Error(), "token secret") {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
	c := newClient("https://docs.qq.com", Cred{})
	if c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects enabled")
	}
}
func TestInputValidation(t *testing.T) {
	c := testClient(t, func(*http.Request) string { t.Fatal("unexpected request"); return "" })
	ctx := context.Background()
	for _, id := range []string{"", "..", "https://docs.qq.com/doc/x", "id?token=secret", "a/b"} {
		if _, err := c.FileGet(ctx, &FileGetIn{FileID: id}); err == nil {
			t.Fatalf("accepted id %q", id)
		}
	}
	for _, r := range []string{"A:A", "A0:B1", "A10:A1", "A1:ZZ1", "A1:A1001", "A1:Z1000", "A1?x"} {
		if _, err := c.SheetRangeGet(ctx, &SheetRangeGetIn{FileID: "id", SheetID: "s", Range: r}); err == nil {
			t.Fatalf("accepted range %q", r)
		}
	}
	for _, r := range []string{"A1", "a1:c3", "AA1:AC10"} {
		if err := validRange(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, in := range []*SheetTextWriteIn{{FileID: "id", SheetID: "s"}, {FileID: "id", SheetID: "s", StartRow: -1, Rows: []TextRow{{Values: []string{"x"}}}}, {FileID: "id", SheetID: "s", Rows: []TextRow{{Values: []string{"x"}}, {Values: []string{"a", "b"}}}}} {
		if _, err := c.SheetTextWrite(ctx, in); err == nil {
			t.Fatal("accepted invalid matrix")
		}
	}
	if _, err := c.FilesSearch(ctx, &FilesSearchIn{Query: "q", Offset: -1}); err == nil {
		t.Fatal("negative offset")
	}
	if _, err := c.FilesSearch(ctx, &FilesSearchIn{Query: "q", Limit: 51}); err == nil {
		t.Fatal("large limit")
	}
	if _, err := c.FilesSearch(ctx, &FilesSearchIn{}); err == nil {
		t.Fatal("empty query")
	}
	for _, in := range []*FileCreateIn{{FileType: "doc"}, {Title: strings.Repeat("x", 37), FileType: "doc"}, {Title: "t", FileType: "unknown"}} {
		if _, err := c.FileCreate(ctx, in); err == nil {
			t.Fatal("invalid create")
		}
	}
	if _, err := c.DocumentInsertText(ctx, &DocumentInsertTextIn{FileID: "id", Text: "t", Index: -1}); err == nil {
		t.Fatal("negative index")
	}
}

func TestRejectPlaintextCredentialEndpoint(t *testing.T) {
	c := testClient(t, func(*http.Request) string { return `{"ret":0,"data":{"list":[]}}` })
	c.base = "http://api.example.invalid"
	out, err := c.HealthCheck(context.Background(), &HealthCheckIn{})
	if err != nil || out.OK {
		t.Fatalf("plaintext credential endpoint was accepted: %+v %v", out, err)
	}
}

func TestSheetWriteRequiresTypedAcknowledgement(t *testing.T) {
	for _, ack := range []string{`[]`, `"ok"`, `null`, `{}`, `{"updatedCells":-1}`} {
		t.Run(ack, func(t *testing.T) {
			c := testClient(t, func(*http.Request) string { return `{"responses":[{"updateRangeResponse":` + ack + `}]}` })
			_, err := c.SheetTextWrite(context.Background(), &SheetTextWriteIn{FileID: "id", SheetID: "s", Rows: []TextRow{{Values: []string{"value"}}}})
			if err == nil {
				t.Fatalf("accepted invalid acknowledgement %s", ack)
			}
		})
	}
}

func TestSheetWriteReportsPartialCellCount(t *testing.T) {
	c := testClient(t, func(*http.Request) string { return `{"responses":[{"updateRangeResponse":{"updatedCells":1}}]}` })
	out, err := c.SheetTextWrite(context.Background(), &SheetTextWriteIn{FileID: "id", SheetID: "s", Rows: []TextRow{{Values: []string{"one", "two"}}}})
	if err != nil || out.Updated || out.UpdatedCells != 1 {
		t.Fatalf("partial result %+v %v", out, err)
	}
}
