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
func fixture(status int, ct, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body))}
}

const directoryXML = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:response><d:href>/cloud/remote.php/dav/files/alice/Reports/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype><oc:fileid>9007199254740993</oc:fileid></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response><d:response><d:href>/cloud/remote.php/dav/files/alice/Reports/a%20b.txt</d:href><d:propstat><d:prop><d:resourcetype/><d:getcontentlength>3</d:getcontentlength><d:getetag>&quot;v1&quot;</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat><d:propstat><d:prop><d:getcontenttype/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat></d:response></d:multistatus>`

func TestWebDAVDirectory(t *testing.T) {
	c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
	c.base = "http://cloud.local/cloud"
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "alice" || p != "secret" || r.Method != "PROPFIND" || r.Header.Get("Depth") != "1" || r.URL.Path != "/cloud/remote.php/dav/files/alice/Reports" {
			t.Fatalf("request %v", r)
		}
		return fixture(207, "application/xml", directoryXML), nil
	})}
	o, e := c.listDirectory(context.Background(), &ListDirectoryIn{Path: "Reports"})
	if e != nil {
		t.Fatal(e)
	}
	if !o.Directory.IsDirectory || *o.Directory.FileID != "9007199254740993" || len(o.Children) != 1 || o.Children[0].Path != "Reports/a b.txt" || o.Children[0].ContentType != nil {
		t.Fatalf("%+v", o)
	}
}
func TestInvalidPathsAndFailedProperties(t *testing.T) {
	c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
	c.base = "http://cloud.local/cloud"
	calls := 0
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return fixture(207, "application/xml", strings.ReplaceAll(directoryXML, "200 OK", "403 Forbidden")), nil
	})}
	for _, p := range []string{"//", "../secret", "a//b", "a/%2fsecret", "http://host/path", "a\\b"} {
		if _, e := c.getInfo(context.Background(), &GetInfoIn{Path: p}); e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	if _, e := c.getInfo(context.Background(), &GetInfoIn{Path: "Reports"}); e == nil {
		t.Fatal("accepted failed properties")
	}
}
func TestMoveNoOverwrite(t *testing.T) {
	c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
	c.base = "http://cloud.local/cloud"
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "MOVE" || r.Header.Get("Overwrite") != "F" || r.Header.Get("Destination") != "http://cloud.local/cloud/remote.php/dav/files/alice/New%20name.txt" || r.Header.Get("If-Match") != `"old"` || r.GetBody != nil {
			t.Fatalf("request %v", r)
		}
		return fixture(201, "", ""), nil
	})}
	if _, e := c.moveResource(context.Background(), &MoveResourceIn{SourcePath: "old.txt", DestinationPath: "New name.txt", IfMatch: `"old"`}); e != nil {
		t.Fatal(e)
	}
}
func TestTransportBoundaries(t *testing.T) {
	for _, status := range []int{302, 401, 412, 423, 500} {
		c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
		c.base = "http://cloud.local/cloud"
		calls := 0
		c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			r := fixture(status, "text/plain", "SECRET")
			r.Header.Set("Location", "https://evil.local")
			return r, nil
		})}
		if _, e := c.getInfo(context.Background(), &GetInfoIn{}); e == nil || strings.Contains(e.Error(), "SECRET") || calls != 1 {
			t.Fatalf("status%d %v calls%d", status, e, calls)
		}
	}
	c := newClient(Cred{UserID: "alice", AppPassword: "secret"})
	c.base = "http://cloud.local/cloud"
	c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return fixture(207, "application/xml", strings.Repeat(" ", maxXML+1)), nil
	})}
	if _, e := c.getInfo(context.Background(), &GetInfoIn{}); e == nil {
		t.Fatal("oversized XML accepted")
	}
}
