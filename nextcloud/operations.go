package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/nextcloud/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode"
)

func (c *client) createDirectory(ctx context.Context, in *CreateDirectoryIn) (*CreateDirectoryOut, error) {
	p, e := safePath(in.Path, false)
	if e != nil {
		return nil, e
	}
	if _, _, _, e = c.request(ctx, "MKCOL", p, nil, nil, maxXML, 201); e != nil {
		return nil, e
	}
	return &CreateDirectoryOut{Created: true}, nil
}
func (c *client) uploadFile(ctx plugin.Ctx, in *UploadFileIn) (*UploadFileOut, error) {
	p, e := safePath(in.Path, false)
	if e != nil {
		return nil, e
	}
	if e = expectedETag(in.IfMatch); e != nil {
		return nil, e
	}
	if in.IfMatch != "" && !in.Overwrite {
		return nil, errors.New("expected ETag requires allow overwrite")
	}
	if in.File == nil || in.File.Size > maxFile || in.File.Size < 0 || (in.File.ID == "" && len(in.File.Data) == 0) {
		return nil, errors.New("provide a platform file no larger than 32 MiB")
	}
	if _, e = c.endpoint(p); e != nil {
		return nil, e
	}
	b, e := in.File.Blob(ctx)
	if e != nil {
		return nil, errors.New("could not read platform file")
	}
	if len(b) > maxFile {
		return nil, errors.New("platform file exceeds 32 MiB")
	}
	ct := in.File.Mime
	if _, _, e = mime.ParseMediaType(ct); e != nil {
		ct = "application/octet-stream"
	}
	h := http.Header{"Content-Type": {ct}}
	if !in.Overwrite {
		h.Set("If-None-Match", "*")
	} else if in.IfMatch != "" {
		h.Set("If-Match", in.IfMatch)
	}
	_, rh, status, e := c.request(ctx, "PUT", p, b, h, maxXML, 201, 204)
	if e != nil {
		return nil, e
	}
	out := &UploadFileOut{Receipt: schema.WriteReceipt{Created: status == 201}}
	if etag := rh.Get("ETag"); etag != "" {
		out.Receipt.ETag = &etag
	}
	return out, nil
}
func outputName(s string) bool {
	return s != "" && s != "." && s != ".." && len(s) <= 255 && !strings.ContainsAny(s, "/\\") && strings.IndexFunc(s, unicode.IsControl) < 0
}
func (c *client) downloadFile(ctx plugin.Ctx, in *DownloadFileIn) (*DownloadFileOut, error) {
	p, e := safePath(in.Path, false)
	if e != nil {
		return nil, e
	}
	name := in.OutputName
	if name == "" {
		name = path.Base(p)
	}
	if !outputName(name) {
		return nil, errors.New("output filename must be a safe basename of at most 255 bytes")
	}
	info, e := c.getInfo(ctx, &GetInfoIn{Path: p})
	if e != nil {
		return nil, e
	}
	if info.Resource.IsDirectory {
		return nil, errors.New("download file requires a file, not a directory")
	}
	if info.Resource.Size != nil && *info.Resource.Size > maxFile {
		return nil, errors.New("Nextcloud file exceeds 32 MiB")
	}
	h := http.Header{}
	if etag := info.Resource.ETag; etag != nil && expectedETag(*etag) == nil && *etag != "" {
		h.Set("If-Match", *etag)
	}
	b, rh, _, e := c.request(ctx, "GET", p, nil, h, maxFile, 200)
	if e != nil {
		return nil, e
	}
	ct := rh.Get("Content-Type")
	if _, _, e = mime.ParseMediaType(ct); e != nil {
		ct = "application/octet-stream"
	}
	f, e := ctx.Upload(name, ct, b)
	if e != nil || f == nil {
		return nil, errors.New("could not store downloaded file in platform storage")
	}
	return &DownloadFileOut{File: f}, nil
}
func (c *client) moveResource(ctx context.Context, in *MoveResourceIn) (*MoveResourceOut, error) {
	src, e := safePath(in.SourcePath, false)
	if e != nil {
		return nil, e
	}
	dst, e := safePath(in.DestinationPath, false)
	if e != nil {
		return nil, e
	}
	if src == dst || strings.HasPrefix(dst, src+"/") {
		return nil, errors.New("destination must differ from source and must not be inside it")
	}
	if e = expectedETag(in.IfMatch); e != nil {
		return nil, e
	}
	target, e := c.endpoint(dst)
	if e != nil {
		return nil, e
	}
	h := http.Header{"Destination": {target.String()}, "Overwrite": {"F"}}
	if in.IfMatch != "" {
		h.Set("If-Match", in.IfMatch)
	}
	if _, _, _, e = c.request(ctx, "MOVE", src, nil, h, maxXML, 201, 204); e != nil {
		return nil, e
	}
	return &MoveResourceOut{Moved: true}, nil
}
