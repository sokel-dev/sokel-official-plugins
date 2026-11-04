package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"net/http"
	"testing"
)

func TestFoldersUseV2(t *testing.T) {
	a, n := fakeAPI(t, "GET", "/v2/teams/123/folders", 200, fixture(t, "folders"), nil)
	o, e := a.listTeamFolders(context.Background(), &ListTeamFoldersIn{TeamID: "123"})
	if e != nil || o == nil || len(o.Folders) != 1 || *n != 1 {
		t.Fatalf("folders %+v %v", o, e)
	}
}
func TestNodesNullAndBoundedChildren(t *testing.T) {
	a, _ := fakeAPI(t, "GET", "/v1/files/FILE/nodes", 200, fixture(t, "nodes"), func(r *http.Request, b []byte) {
		if r.URL.Query().Get("depth") != "1" || r.URL.Query().Get("ids") != "1:2,9:9" {
			t.Fatal("query")
		}
	})
	o, e := a.getNodes(context.Background(), &GetNodesIn{FileKey: "FILE", NodeIDs: []string{"1:2", "9:9"}})
	if e != nil || o == nil || len(o.Nodes) != 2 || !o.Nodes[0].Found || o.Nodes[1].Found || len(o.Nodes[0].Children) != 1 {
		t.Fatalf("nodes %+v %v", o, e)
	}
}
func TestRenderFalseAndNull(t *testing.T) {
	var in RenderImagesIn
	if e := bindTestInput(map[string]any{"file_key": "FILE", "node_ids": []any{"1:2", "9:9"}, "options": map[string]any{"contents_only": false, "scale": 0.5}}, &in); e != nil {
		t.Fatal(e)
	}
	a, _ := fakeAPI(t, "GET", "/v1/images/FILE", 200, fixture(t, "images"), func(r *http.Request, b []byte) {
		if r.URL.Query().Get("contents_only") != "false" || r.URL.Query().Get("scale") != "0.5" {
			t.Fatal("optional values lost")
		}
	})
	o, e := a.renderImages(context.Background(), &in)
	if e != nil || o == nil || !o.Images[0].Rendered || o.Images[1].Rendered || o.Images[1].URL != nil {
		t.Fatalf("images %+v %v", o, e)
	}
}
func TestCommentBytes(t *testing.T) {
	a, n := fakeAPI(t, "POST", "/v1/files/FILE/comments", 200, fixture(t, "comment"), func(r *http.Request, b []byte) {
		var x map[string]string
		json.Unmarshal(b, &x)
		if x["message"] != "Review ready" || x["comment_id"] != "123" {
			t.Fatalf("body %s", b)
		}
	})
	o, e := a.createComment(context.Background(), &CreateCommentIn{FileKey: "FILE", Message: "Review ready", ReplyTo: "123"})
	if e != nil || o == nil || o.Comment.ID != "123" || *n != 1 {
		t.Fatalf("comment %+v %v", o, e)
	}
}

func bindTestInput(v map[string]any, out any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return contract.BindInput(b, out)
}
