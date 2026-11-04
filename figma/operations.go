package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/figma/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func (a *api) health(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	b, e := a.call(ctx, "GET", "/v1/me", nil, nil, 200)
	if e == nil {
		var u schema.User
		e = decode(b, &u)
		if e == nil && u.ID == "" {
			e = badResponse()
		}
	}
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Figma user identity verified"}, nil
}
func (a *api) getFileMetadata(ctx context.Context, in *GetFileMetadataIn) (*GetFileMetadataOut, error) {
	if e := requireID(in.FileKey); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/v1/files/"+in.FileKey+"/meta", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		File *struct {
			schema.FileMetadata
			EditorType string `json:"editorType"`
		} `json:"file"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.File == nil || v.File.Name == "" || v.File.EditorType == "" || v.File.Creator.ID == "" {
		return nil, badResponse()
	}
	result := v.File.FileMetadata
	result.EditorType = v.File.EditorType
	return &GetFileMetadataOut{File: result}, nil
}

var nodePattern = regexp.MustCompile(`^[A-Za-z0-9:;_-]+$`)

func nodeQuery(file string, ids []string) (url.Values, error) {
	if e := requireID(file); e != nil {
		return nil, e
	}
	if len(ids) < 1 || len(ids) > 100 {
		return nil, errors.New("provide between 1 and 100 node IDs")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if len(id) > 200 || !nodePattern.MatchString(id) || seen[id] {
			return nil, errors.New("node IDs must be valid and unique")
		}
		seen[id] = true
	}
	return url.Values{"ids": {strings.Join(ids, ",")}}, nil
}

type nodeWire struct {
	schema.NodeSummary
	Children []schema.NodeSummary `json:"children"`
}

func (a *api) getNodes(ctx context.Context, in *GetNodesIn) (*GetNodesOut, error) {
	q, e := nodeQuery(in.FileKey, in.NodeIDs)
	if e != nil {
		return nil, e
	}
	q.Set("depth", "1")
	if in.Version != "" {
		if e = requireID(in.Version); e != nil {
			return nil, e
		}
		q.Set("version", in.Version)
	}
	b, e := a.call(ctx, "GET", "/v1/files/"+in.FileKey+"/nodes", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Name  string `json:"name"`
		Nodes map[string]*struct {
			Document *nodeWire `json:"document"`
		} `json:"nodes"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Nodes == nil || v.Name == "" {
		return nil, badResponse()
	}
	o := &GetNodesOut{FileName: v.Name, Nodes: []schema.NodeResult{}}
	for _, id := range in.NodeIDs {
		w, ok := v.Nodes[id]
		if !ok {
			return nil, badResponse()
		}
		r := schema.NodeResult{NodeID: id, Children: []schema.NodeSummary{}}
		if w != nil {
			if w.Document == nil || w.Document.ID == "" || w.Document.Type == "" {
				return nil, badResponse()
			}
			r.Found = true
			r.Node = &w.Document.NodeSummary
			if w.Document.Children != nil {
				r.Children = w.Document.Children
			}
		}
		o.Nodes = append(o.Nodes, r)
	}
	return o, nil
}
func (a *api) renderImages(ctx context.Context, in *RenderImagesIn) (*RenderImagesOut, error) {
	q, e := nodeQuery(in.FileKey, in.NodeIDs)
	if e != nil {
		return nil, e
	}
	format := in.Format
	if format == "" {
		format = "png"
	}
	switch format {
	case "png", "jpg", "svg", "pdf":
	default:
		return nil, errors.New("image format must be png, jpg, svg or pdf")
	}
	q.Set("format", format)
	if in.Options.Scale != nil {
		v := *in.Options.Scale
		if v < 0.01 || v > 4 {
			return nil, errors.New("image scale must be between 0.01 and 4")
		}
		q.Set("scale", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if in.Options.ContentsOnly != nil {
		q.Set("contents_only", strconv.FormatBool(*in.Options.ContentsOnly))
	}
	if in.Options.UseAbsoluteBounds != nil {
		q.Set("use_absolute_bounds", strconv.FormatBool(*in.Options.UseAbsoluteBounds))
	}
	b, e := a.call(ctx, "GET", "/v1/images/"+in.FileKey, q, nil, 200)
	if e != nil {
		return nil, e
	}
	var v struct {
		Images map[string]*string `json:"images"`
	}
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Images == nil {
		return nil, badResponse()
	}
	o := &RenderImagesOut{Images: []schema.Image{}}
	for _, id := range in.NodeIDs {
		u, ok := v.Images[id]
		if !ok {
			return nil, badResponse()
		}
		if u != nil {
			parsed, e := url.Parse(*u)
			if e != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return nil, badResponse()
			}
		}
		o.Images = append(o.Images, schema.Image{NodeID: id, URL: u, Rendered: u != nil})
	}
	return o, nil
}
func (a *api) listTeamFolders(ctx context.Context, in *ListTeamFoldersIn) (*ListTeamFoldersOut, error) {
	if e := requireID(in.TeamID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/v2/teams/"+in.TeamID+"/folders", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v ListTeamFoldersOut
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if e = validFolders(v.Folders); e != nil {
		return nil, e
	}
	return &v, nil
}
func validFolders(v []schema.Folder) error {
	if v == nil {
		return badResponse()
	}
	for _, f := range v {
		if f.ID == "" {
			return badResponse()
		}
	}
	return nil
}
func (a *api) listSubfolders(ctx context.Context, in *ListSubfoldersIn) (*ListSubfoldersOut, error) {
	if e := requireID(in.FolderID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/v2/folders/"+in.FolderID+"/folders", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v ListSubfoldersOut
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if e = validFolders(v.Folders); e != nil {
		return nil, e
	}
	return &v, nil
}
func (a *api) listFolderFiles(ctx context.Context, in *ListFolderFilesIn) (*ListFolderFilesOut, error) {
	if e := requireID(in.FolderID); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/v2/folders/"+in.FolderID+"/files", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v ListFolderFilesOut
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Files == nil {
		return nil, badResponse()
	}
	for _, f := range v.Files {
		if f.Key == "" {
			return nil, badResponse()
		}
	}
	return &v, nil
}
func (a *api) listComments(ctx context.Context, in *ListCommentsIn) (*ListCommentsOut, error) {
	if e := requireID(in.FileKey); e != nil {
		return nil, e
	}
	b, e := a.call(ctx, "GET", "/v1/files/"+in.FileKey+"/comments", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var v ListCommentsOut
	if e = decode(b, &v); e != nil {
		return nil, e
	}
	if v.Comments == nil {
		return nil, badResponse()
	}
	for _, c := range v.Comments {
		if c.ID == "" {
			return nil, badResponse()
		}
	}
	return &v, nil
}
func (a *api) createComment(ctx context.Context, in *CreateCommentIn) (*CreateCommentOut, error) {
	if e := requireID(in.FileKey); e != nil {
		return nil, e
	}
	if e := requiredText(in.Message, 10000); e != nil {
		return nil, e
	}
	if in.ReplyTo != "" {
		if e := requireID(in.ReplyTo); e != nil {
			return nil, e
		}
	}
	v := struct {
		Message   string `json:"message"`
		CommentID string `json:"comment_id,omitempty"`
	}{in.Message, in.ReplyTo}
	b, e := a.call(ctx, "POST", "/v1/files/"+in.FileKey+"/comments", nil, v, 200)
	if e != nil {
		return nil, e
	}
	var c schema.Comment
	if e = decode(b, &c); e != nil {
		return nil, e
	}
	if c.ID == "" {
		return nil, badResponse()
	}
	return &CreateCommentOut{Comment: c}, nil
}
