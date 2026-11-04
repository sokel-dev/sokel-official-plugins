package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, fn func(*http.Request) (int, string)) *client {
	t.Helper()
	c := newClient(Cred{AccessToken: "fixture-token"})
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "openapi.wps.cn" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("Incorrect origin or authentication")
		}
		status, body := fn(r)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"17"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return c
}

const fileFixture = `{"id":"file-1","drive_id":"drive-1","parent_id":"0","name":"中文","type":"folder","size":0,"version":1}`

func TestCreateFolderExactBytes(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/v7/drives/drive-1/files/0/create" || string(b) != `{"file_type":"folder","name":"中文","on_name_conflict":"fail"}` {
			t.Errorf("request %s %s %s", r.Method, r.URL.Path, b)
		}
		if r.Header.Get("X-Kso-Date") != "" || r.Header.Get("X-Kso-Authorization") != "" {
			t.Error("signing should be disabled")
		}
		return 200, `{"code":0,"data":` + fileFixture + `}`
	})
	out, e := c.createFolder(context.Background(), &CreateFolderIn{DriveID: "drive-1", ParentID: "0", Name: "中文"})
	if e != nil {
		t.Fatal(e)
	}
	if out.File.ID != "file-1" || out.File.Name != "中文" {
		t.Fatalf("file %+v", out)
	}
}
func TestFileListOpaqueToken(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("page_token") != "a+/=?" || r.URL.Query().Get("page_size") != "2" {
			t.Error("pagination wire changed")
		}
		return 200, `{"code":0,"data":{"items":[` + fileFixture + `],"next_page_token":"next+/="}}`
	})
	out, e := c.listFiles(context.Background(), &ListFilesIn{DriveID: "drive-1", ParentID: "0", PageSize: 2, PageToken: "a+/=?"})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Files) != 1 || out.NextPageToken != "next+/=" {
		t.Fatalf("page %+v", out)
	}
}
func TestEnvelopeErrorAndHealth(t *testing.T) {
	c := fixture(t, func(r *http.Request) (int, string) {
		return 200, `{"code":403000001,"msg":"fixture-token private-data"}`
	})
	if _, e := c.getFile(context.Background(), &GetFileIn{DriveID: "d", FileID: "f"}); e == nil || strings.Contains(e.Error(), "fixture-token") {
		t.Fatalf("unsafe error %v", e)
	}
	out, e := c.healthCheck(context.Background(), &HealthCheckIn{})
	if e != nil || out == nil || out.OK {
		t.Fatalf("health %v %v", out, e)
	}
}
