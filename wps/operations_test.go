package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOfficialKSO1Vectors(t *testing.T) {
	for _, v := range []struct{ method, uri, body, want string }{{"GET", "/v7/test?key=value", "", "ce8df66877175e5198c8ea1362ffddf82e4941c6f25a4ca205a1ad09d0faaf03"}, {"POST", "/v7/test/body", `{"key": "value"}`, "c46e6c988130818ecba2484d51ac685948fbbef6814602c7874d6bfc41dc17b3"}} {
		got := sign("AK123456", "sk098765", v.method, v.uri, "Mon, 02 Jan 2006 15:04:05 GMT", []byte(v.body))
		if got != "KSO-1 AK123456:"+v.want {
			t.Fatal(got)
		}
	}
}
func TestSignedWireUsesActualBodyAndEncodedQuery(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		date := r.Header.Get("X-Kso-Date")
		if !strings.HasSuffix(date, " GMT") {
			t.Fatal("date must be GMT")
		}
		if r.Header.Get("X-Kso-Authorization") != sign("AK", "SK", r.Method, r.URL.RequestURI(), date, b) {
			t.Error("signature wire mismatch")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("signed content type")
		}
		return 200, `{"code":0,"data":{"items":[],"next_page_token":""}}`
	})
	c.cred.AppID = "AK"
	c.cred.AppKey = "SK"
	if _, e := c.listFiles(context.Background(), &ListFilesIn{DriveID: "d", ParentID: "0", PageToken: "a+/=?"}); e != nil {
		t.Fatal(e)
	}
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"dst_name":"中文"}` || r.Header.Get("X-Kso-Authorization") != sign("AK", "SK", r.Method, r.URL.RequestURI(), r.Header.Get("X-Kso-Date"), b) {
			t.Fatal("signed write bytes")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":` + fileFixture + `}`)), Header: http.Header{}, Request: r}, nil
	})
	if _, e := c.renameFile(context.Background(), &RenameFileIn{DriveID: "drive-1", FileID: "file-1", Name: "中文"}); e != nil {
		t.Fatal(e)
	}
}
func TestAllGeneratedOperations(t *testing.T) {
	calls := 0
	c := fixture(t, func(r *http.Request) (int, string) {
		calls++
		switch {
		case strings.HasSuffix(r.URL.Path, "/current"):
			return 200, `{"code":0,"data":{"name":"app","company_id":"co"}}`
		case strings.HasSuffix(r.URL.Path, "/children"):
			return 200, `{"code":0,"data":{"items":[` + fileFixture + `],"next_page_token":""}}`
		case strings.HasSuffix(r.URL.Path, "/path"):
			return 200, `{"code":0,"data":{"location_id":"loc","location_type":"link","paths":[{"file_id":"file-1","name":"中文"}]}}`
		case strings.HasSuffix(r.URL.Path, "/download"):
			if r.URL.Query().Get("internal") != "false" || r.URL.Query().Get("with_hash") != "true" {
				t.Error("download query")
			}
			return 200, `{"code":0,"data":{"url":"https://storage.wps.cn/private?signature=sensitive","hashes":[{"type":"sha256","sum":"abc"}]}}`
		default:
			return 200, `{"code":0,"data":` + fileFixture + `}`
		}
	})
	if v := invoke(t, c, "health_check", `{}`); v["ok"] != true {
		t.Fatal(v)
	}
	invoke(t, c, "list_files", `{"drive_id":"drive-1","parent_id":"0"}`)
	invoke(t, c, "get_file", `{"drive_id":"drive-1","file_id":"file-1"}`)
	invoke(t, c, "create_folder", `{"drive_id":"drive-1","parent_id":"0","name":"中文"}`)
	invoke(t, c, "rename_file", `{"drive_id":"drive-1","file_id":"file-1","name":"中文"}`)
	invoke(t, c, "get_file_path", `{"drive_id":"drive-1","file_id":"file-1"}`)
	invoke(t, c, "get_download_info", `{"drive_id":"drive-1","file_id":"file-1"}`)
	if calls != 7 {
		t.Fatal("download URL was fetched or unexpected calls")
	}
}
func TestInputValidationPreventsNetwork(t *testing.T) {
	c := fixture(t, func(*http.Request) (int, string) { t.Fatal("unexpected network"); return 500, "" })
	ctx := context.Background()
	for _, id := range []string{"", "../x", "https://evil.example", "%2f"} {
		if _, e := c.getFile(ctx, &GetFileIn{DriveID: id, FileID: "f"}); e == nil {
			t.Fatal("unsafe ID")
		}
		if _, e := c.getFilePath(ctx, &GetFilePathIn{DriveID: "d", FileID: id}); e == nil {
			t.Fatal("unsafe path")
		}
		if _, e := c.getDownloadInfo(ctx, &GetDownloadInfoIn{DriveID: id, FileID: "f"}); e == nil {
			t.Fatal("unsafe download")
		}
	}
	for _, name := range []string{"", "../bad", "bad\nname", strings.Repeat("x", 241), "bad?name"} {
		if _, e := c.createFolder(ctx, &CreateFolderIn{DriveID: "d", ParentID: "0", Name: name}); e == nil {
			t.Fatal("invalid name")
		}
		if _, e := c.renameFile(ctx, &RenameFileIn{DriveID: "d", FileID: "f", Name: name}); e == nil {
			t.Fatal("invalid rename")
		}
	}
	for _, v := range []ListFilesIn{{DriveID: "d", ParentID: "0", PageSize: -1}, {DriveID: "d", ParentID: "0", PageSize: 501}, {DriveID: "d", ParentID: "0", PageToken: "x\n"}, {DriveID: "../x", ParentID: "0"}} {
		if _, e := c.listFiles(ctx, &v); e == nil {
			t.Fatal("invalid page")
		}
	}
	for _, cred := range []Cred{{AccessToken: "fixture-token", AppID: "ak"}, {AccessToken: "fixture-token", AppKey: "sk"}, {AccessToken: "fixture-token", AppID: "a:b", AppKey: "sk"}, {AccessToken: "fixture-token", AppID: "ak", AppKey: "bad\nkey"}} {
		c.cred = cred
		if _, e := c.healthCheck(ctx, &HealthCheckIn{}); e != nil {
			t.Fatal(e)
		}
	}
}
func TestMalformedEnvelopesAndMetadata(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{`null`, `{}`, `{"code":0}`, `{"code":0,"data":null}`, `{"code":0,"data":{}}`, `{"code":0,"data":{"items":null}}`, `{"code":0,"data":{"items":[{}]}}`, `{"code":0,"data":{"items":[],"next_page_token":"same"}}`, `{"code":0,"data":{"items":[],"next_page_token":"x\n"}}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := c.listFiles(ctx, &ListFilesIn{DriveID: "d", ParentID: "0", PageToken: "same"}); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	for _, body := range []string{`{"code":0,"data":{}}`, `{"code":0,"data":` + fileFixture + `}`} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, body })
		if _, e := c.getFile(ctx, &GetFileIn{DriveID: "d", FileID: "f"}); e == nil {
			t.Fatal("wrong metadata")
		}
		if _, e := c.createFolder(ctx, &CreateFolderIn{DriveID: "d", ParentID: "0", Name: "other"}); e == nil {
			t.Fatal("wrong create receipt")
		}
		if _, e := c.renameFile(ctx, &RenameFileIn{DriveID: "d", FileID: "f", Name: "other"}); e == nil {
			t.Fatal("wrong rename receipt")
		}
		if _, e := c.getFilePath(ctx, &GetFilePathIn{DriveID: "d", FileID: "f"}); e == nil {
			t.Fatal("wrong path")
		}
		if v, e := c.healthCheck(ctx, &HealthCheckIn{}); e != nil || v.OK {
			t.Fatal("fake health")
		}
	}
	for _, u := range []string{"http://storage.wps.cn/file", "https://user:pass@storage.wps.cn/file", "https://storage.wps.cn/file#fragment", ""} {
		c := fixture(t, func(*http.Request) (int, string) { return 200, `{"code":0,"data":{"url":"` + u + `"}}` })
		if _, e := c.getDownloadInfo(ctx, &GetDownloadInfoIn{DriveID: "d", FileID: "f"}); e == nil {
			t.Fatal("invalid URL")
		}
	}
	c := fixture(t, func(*http.Request) (int, string) {
		return 200, `{"code":0,"data":{"url":"https://storage.wps.cn/file"}}`
	})
	if v, e := c.getDownloadInfo(ctx, &GetDownloadInfoIn{DriveID: "d", FileID: "f"}); e != nil || v.Hashes == nil {
		t.Fatal(v, e)
	}
}
