package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const sampleMedia = `{"id":9,"source_url":"https://example.com/uploads/a.png","mime_type":"image/png","alt_text":"picture","title":{"rendered":"Image"}}`

func TestGeneratedUpdatePreservesStatusAndClearsCategories(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if r.Method != "POST" || r.URL.Path != "/wp-json/wp/v2/posts/10" || json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Error("invalid update request")
		}
		if _, ok := b["status"]; ok {
			t.Fatal("update changed publication status")
		}
		if b["content"] != "" || b["excerpt"] != "" || len(b["categories"].([]any)) != 0 {
			t.Fatalf("explicit empty values lost %+v", b)
		}
		reply(w, examplePost)
	})
	t.Setenv("WORDPRESS_SITE_URL", c.base)
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	ctx := &testCtx{Context: context.Background(), cred: map[string]string{"username": "editor", "application_password": "test-secret"}}
	sink := &testSink{}
	if e := h.calls["update_post"](ctx, json.RawMessage(`{"post_id":"10","title":"Title","content":"","categories":[]}`), sink); e != nil {
		t.Fatal(e)
	}
	p := sink.vars["post"].(map[string]any)
	if p["id"] != "10" || p["status"] != "draft" {
		t.Fatalf("output %+v", p)
	}
}
func TestReadCategoriesMediaAndGet(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-WP-Total", "1")
		w.Header().Set("X-WP-TotalPages", "1")
		switch r.URL.Path {
		case "/wp-json/wp/v2/posts/10":
			reply(w, examplePost)
		case "/wp-json/wp/v2/categories":
			reply(w, `[{"id":1,"name":"News","slug":"news","parent":0,"count":2}]`)
		case "/wp-json/wp/v2/media":
			reply(w, "["+sampleMedia+"]")
		default:
			t.Error("unexpected path")
		}
	})
	p, e := c.getPost(context.Background(), &GetPostIn{PostID: "10"})
	if e != nil || p.Post.ID != "10" {
		t.Fatal(e)
	}
	cats, e := c.listCategories(context.Background(), &ListCategoriesIn{Search: "News"})
	if e != nil || cats.Categories[0].ParentID != "0" || cats.NextPage != "" {
		t.Fatalf("categories %+v %v", cats, e)
	}
	m, e := c.listMedia(context.Background(), &ListMediaIn{})
	if e != nil || m.Media[0].MimeType != "image/png" {
		t.Fatalf("media %+v %v", m, e)
	}
}
func TestMediaMultipartExactBytes(t *testing.T) {
	payload := []byte("\x89PNG\r\n\x00你好")
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/wp-json/wp/v2/media" {
			t.Error("bad upload path")
		}
		mr, e := r.MultipartReader()
		if e != nil {
			t.Fatal(e)
		}
		part, e := mr.NextPart()
		if e != nil || part.FormName() != "file" || part.FileName() != "图.png" || part.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("invalid part %v", e)
		}
		if got := part.Header.Get("Content-Disposition"); got != `form-data; name="file"; filename="图.png"` {
			t.Fatalf("PHP requires a regular filename parameter, received %q", got)
		}
		data, _ := io.ReadAll(part)
		if !reflect.DeepEqual(data, payload) {
			t.Fatal("upload bytes changed")
		}
		alt, e := mr.NextPart()
		if e != nil || alt.FormName() != "alt_text" {
			t.Fatal("missing alt text")
		}
		b, _ := io.ReadAll(alt)
		if string(b) != "diagram" {
			t.Fatal("wrong alt")
		}
		reply(w, sampleMedia)
	})
	ctx := &testCtx{Context: context.Background(), file: payload}
	r, e := c.uploadMedia(ctx, &UploadMediaIn{File: &plugin.File{Name: "图.png", Mime: "image/png", Size: int64(len(payload))}, AltText: "diagram"})
	if e != nil || r.Media.ID != "9" {
		t.Fatalf("upload %+v %v", r, e)
	}
	ctx.fetchErr = errors.New("signed-url-secret")
	if _, e = c.uploadMedia(ctx, &UploadMediaIn{File: &plugin.File{Name: "a.png", Mime: "image/png"}}); e == nil || strings.Contains(e.Error(), "signed-url-secret") {
		t.Fatal("file error leaked")
	}
}
func TestValidationAndMissingPagination(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid input reached network") })
	for _, in := range []*ListPostsIn{{Page: "0"}, {Page: "-1"}, {Page: "1&x=y"}, {Status: "trash"}} {
		if _, e := c.listPosts(context.Background(), in); e == nil {
			t.Fatal("invalid pagination/status")
		}
	}
	if _, e := c.getPost(context.Background(), &GetPostIn{PostID: "../10"}); e == nil {
		t.Fatal("path traversal")
	}
	if _, e := c.createDraft(context.Background(), &CreateDraftIn{Content: "text"}); e == nil {
		t.Fatal("missing title")
	}
	for _, file := range []*plugin.File{nil, {Name: "../a.png", Mime: "image/png"}, {Name: "x.svg", Mime: "image/svg+xml"}, {Name: "a.png", Mime: "image/png", Size: maxMedia + 1}} {
		if _, e := c.uploadMedia(&testCtx{Context: context.Background()}, &UploadMediaIn{File: file}); e == nil {
			t.Fatal("invalid file")
		}
	}
	c = mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, "[]") })
	if _, e := c.listPosts(context.Background(), &ListPostsIn{}); e == nil {
		t.Fatal("missing pagination headers")
	}
	c = mockClient(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if h, _ := c.health(context.Background()); h.OK {
		t.Fatal("public empty response falsely verified credentials")
	}
	if _, e := c.createDraft(context.Background(), &CreateDraftIn{Title: "a"}); e == nil {
		t.Fatal("missing write receipt")
	}
}

func TestUploadQuotedFilenameUsesPHPCompatibleWireHeader(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		want := "Content-Disposition: form-data; name=\"file\"; filename=\"图\\\"片.png\"\r\n"
		if !strings.Contains(string(body), want) || strings.Contains(string(body), "filename*=") {
			t.Fatalf("multipart wire header is not PHP-compatible: %q", body)
		}
		reply(w, sampleMedia)
	})
	ctx := &testCtx{Context: context.Background(), file: []byte("image bytes")}
	_, err := c.uploadMedia(ctx, &UploadMediaIn{File: &plugin.File{Name: `图"片.png`, Mime: "image/png"}})
	if err != nil {
		t.Fatal(err)
	}
}
