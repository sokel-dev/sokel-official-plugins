package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Synthetic protocol examples from official API documentation, not live account captures.
type handlerTransport struct{ h http.HandlerFunc }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.h.ServeHTTP(w, r)
	res := w.Result()
	res.Request = r
	return res, nil
}
func fakeAPI(h http.HandlerFunc) *api {
	a := newAPI(Cred{Site: "https://demo.atlassian.net", Email: "test@example.com", APIToken: "test-secret"})
	a.client = &http.Client{Transport: handlerTransport{h}}
	return a
}
func reply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s)
}
func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func str(v string) *string { return &v }
func TestHealthChecksReadOnly(t *testing.T) {
	calls := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/wiki/rest/api/user/current" {
			t.Fatal("health is not identity GET")
		}
		reply(w, `{"accountId":"u","displayName":"Tester","type":"known"}`)
	})
	out, e := a.health(context.Background(), &HealthCheckIn{})
	if e != nil || out == nil || !out.OK || calls != 1 {
		t.Fatalf("health %v %v calls=%d", out, e, calls)
	}
}

func TestV2PagesAndVersionGuard(t *testing.T) {
	puts := 0
	a := fakeAPI(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/wiki/api/v2/") {
			t.Fatal("wrong API version")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/spaces"):
			w.Header().Set("Link", `</wiki/api/v2/spaces?cursor=c%2B2&limit=2>; rel="next"`)
			reply(w, `{"results":[{"id":"10","key":"DOC","name":"Docs"}]}`)
		case strings.Contains(r.URL.Path, "/spaces/"):
			reply(w, `{"id":"10","key":"DOC","name":"Docs"}`)
		case r.Method == "PUT":
			puts++
			b := body(t, r)
			if b["version"].(map[string]any)["number"] != float64(8) || b["status"] != "current" {
				t.Fatal(b)
			}
			reply(w, `{"id":"20","spaceId":"10","title":"Changed","version":{"number":8},"body":{"storage":{"value":"<p>new</p>","representation":"storage"}}}`)
		case r.Method == "POST":
			b := body(t, r)
			if b["spaceId"] != "10" || b["body"].(map[string]any)["representation"] != "storage" {
				t.Fatal(b)
			}
			reply(w, `{"id":"20","spaceId":"10","title":"New","version":{"number":1}}`)
		case strings.HasSuffix(r.URL.Path, "/pages"):
			if r.URL.Query().Get("space-id") != "10" || r.URL.Query().Get("cursor") != "c+2" {
				t.Fatal(r.URL.RawQuery)
			}
			reply(w, `{"results":[{"id":"20","spaceId":"10","title":"Page","version":{"number":7}}],"_links":{"next":"/wiki/api/v2/pages?cursor=next"}}`)
		default:
			reply(w, `{"id":"20","spaceId":"10","status":"current","title":"Page","version":{"number":7},"body":{"storage":{"value":"<p>old</p>","representation":"storage"}}}`)
		}
	})
	ctx := context.Background()
	if o, e := a.spaces(ctx, &ListSpacesIn{Limit: 2}); e != nil || o == nil || o.NextCursor != "c+2" || len(o.Spaces) != 1 {
		t.Fatalf("spaces %v %v", o, e)
	}
	if o, e := a.getSpace(ctx, &GetSpaceIn{SpaceID: "10"}); e != nil || o == nil || o.Space.ID != "10" {
		t.Fatalf("space %v %v", o, e)
	}
	if o, e := a.pages(ctx, &ListPagesIn{SpaceID: "10", Cursor: "c+2"}); e != nil || o == nil || o.NextCursor != "next" || len(o.Pages) != 1 {
		t.Fatalf("pages %v %v", o, e)
	}
	if o, e := a.getPage(ctx, &GetPageIn{PageID: "20"}); e != nil || o == nil || o.Page.Version != 7 || o.Page.Body != "<p>old</p>" {
		t.Fatalf("page %v %v", o, e)
	}
	if o, e := a.createPage(ctx, &CreatePageIn{SpaceID: "10", Title: "New", Body: "<p>new</p>"}); e != nil || o == nil || o.Page.ID != "20" {
		t.Fatalf("create %v %v", o, e)
	}
	if _, e := a.updatePage(ctx, &UpdatePageIn{PageID: "20", Title: "Changed", Body: "<p>new</p>", ExpectedVersion: 6}); e == nil || puts != 0 {
		t.Fatal("stale version updated")
	}
	if o, e := a.updatePage(ctx, &UpdatePageIn{PageID: "20", Title: "Changed", Body: "<p>new</p>", ExpectedVersion: 7}); e != nil || o == nil || o.Page.Version != 8 || puts != 1 {
		t.Fatalf("update %v %v %d", o, e, puts)
	}
}
