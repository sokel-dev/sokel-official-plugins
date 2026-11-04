package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/figma/schema"
	"testing"
)

func TestInvalidInputsNeverRequest(t *testing.T) {
	a, n := fakeAPI(t, "GET", "unused", 200, "{}", nil)
	ctx := context.Background()
	for _, id := range []string{"", "../file", "https://evil.invalid", "%2f"} {
		if _, e := a.getFileMetadata(ctx, &GetFileMetadataIn{FileKey: id}); e == nil {
			t.Fatal("ID accepted")
		}
	}
	for _, ids := range [][]string{nil, {"1:2", "1:2"}, {"https://evil.invalid"}, make([]string, 101)} {
		if _, e := a.getNodes(ctx, &GetNodesIn{FileKey: "FILE", NodeIDs: ids}); e == nil {
			t.Fatal("nodes accepted")
		}
	}
	zero := 0.
	if _, e := a.renderImages(ctx, &RenderImagesIn{FileKey: "FILE", NodeIDs: []string{"1:2"}, Options: schema.ImageOptions{Scale: &zero}}); e == nil {
		t.Fatal("zero scale")
	}
	if _, e := a.renderImages(ctx, &RenderImagesIn{FileKey: "FILE", NodeIDs: []string{"1:2"}, Format: "gif"}); e == nil {
		t.Fatal("format")
	}
	if _, e := a.createComment(ctx, &CreateCommentIn{FileKey: "FILE", Message: " "}); e == nil {
		t.Fatal("blank comment")
	}
	if _, e := a.createComment(ctx, &CreateCommentIn{FileKey: "FILE", Message: "ok", ReplyTo: "../root"}); e == nil {
		t.Fatal("reply ID")
	}
	if *n != 0 {
		t.Fatal("invalid requests sent")
	}
}
func TestResponseShapes(t *testing.T) {
	ctx := context.Background()
	for _, body := range []string{"{}", "null", `{"folders":[{}],"files":[{}],"comments":[{}]}`} {
		a, _ := fakeAPI(t, "GET", "/v2/teams/123/folders", 200, body, nil)
		if _, e := a.listTeamFolders(ctx, &ListTeamFoldersIn{TeamID: "123"}); e == nil {
			t.Fatal("folders")
		}
		a, _ = fakeAPI(t, "GET", "/v2/folders/123/files", 200, body, nil)
		if _, e := a.listFolderFiles(ctx, &ListFolderFilesIn{FolderID: "123"}); e == nil {
			t.Fatal("files")
		}
		a, _ = fakeAPI(t, "GET", "/v1/files/FILE/comments", 200, body, nil)
		if _, e := a.listComments(ctx, &ListCommentsIn{FileKey: "FILE"}); e == nil {
			t.Fatal("comments")
		}
		a, _ = fakeAPI(t, "GET", "/v1/files/FILE/meta", 200, body, nil)
		if _, e := a.getFileMetadata(ctx, &GetFileMetadataIn{FileKey: "FILE"}); e == nil {
			t.Fatal("metadata")
		}
	}
	for _, body := range []string{`{}`, `{"name":"X","nodes":{}}`, `{"name":"X","nodes":{"1:2":{}}}`, `{"name":"X","nodes":{"1:2":{"document":{}}}}`} {
		a, _ := fakeAPI(t, "GET", "/v1/files/FILE/nodes", 200, body, nil)
		if _, e := a.getNodes(ctx, &GetNodesIn{FileKey: "FILE", NodeIDs: []string{"1:2"}}); e == nil {
			t.Fatal("nodes shape")
		}
	}
	for _, body := range []string{`{}`, `{"images":{}}`, `{"images":{"1:2":"http://evil.invalid"}}`, `{"images":{"1:2":"https://user:password@evil.invalid"}}`} {
		a, _ := fakeAPI(t, "GET", "/v1/images/FILE", 200, body, nil)
		if _, e := a.renderImages(ctx, &RenderImagesIn{FileKey: "FILE", NodeIDs: []string{"1:2"}}); e == nil {
			t.Fatal("images shape")
		}
	}
}
