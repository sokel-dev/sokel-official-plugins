package main

import (
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"path"
	"strings"
)

func outputName(v, fallback string) string {
	v = path.Base(strings.ReplaceAll(v, "\\", "/"))
	if v == "" || v == "." || v == "/" {
		return fallback
	}
	return v
}
func uploadWith(ctx plugin.Ctx, a *api, in *UploadFileIn) (*UploadFileOut, error) {
	if in.File == nil {
		return nil, fmt.Errorf("a platform file is required")
	}
	if in.File.Size > maxUpload {
		return nil, fmt.Errorf("multipart upload is limited to 5 MiB")
	}
	data, e := ctx.Fetch(in.File)
	if e != nil {
		return nil, fmt.Errorf("failed to read the platform file")
	}
	name := in.Name
	if name == "" {
		name = in.File.Name
	}
	return a.upload(ctx, outputName(name, "upload.bin"), in.File.Mime, in.ParentID, data)
}
func downloadWith(ctx plugin.Ctx, a *api, in *DownloadFileIn) (*DownloadFileOut, error) {
	data, name, mimeType, e := a.download(ctx, in.FileID)
	if e != nil {
		return nil, e
	}
	if in.Filename != "" {
		name = in.Filename
	}
	f, e := ctx.Upload(outputName(name, "download.bin"), mimeType, data)
	if e != nil {
		return nil, fmt.Errorf("failed to store downloaded bytes in the platform file layer")
	}
	return &DownloadFileOut{File: f}, nil
}
func exportWith(ctx plugin.Ctx, a *api, in *ExportFileIn) (*ExportFileOut, error) {
	if strings.TrimSpace(in.Filename) == "" {
		return nil, fmt.Errorf("filename is required for export")
	}
	data, e := a.export(ctx, in.FileID, in.MimeType)
	if e != nil {
		return nil, e
	}
	f, e := ctx.Upload(outputName(in.Filename, "export.bin"), in.MimeType, data)
	if e != nil {
		return nil, fmt.Errorf("failed to store exported bytes in the platform file layer")
	}
	return &ExportFileOut{File: f}, nil
}
