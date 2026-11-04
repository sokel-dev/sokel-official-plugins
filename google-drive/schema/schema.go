// Package schema declares the public Google integration contract.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type FileInfo struct {
	ID           string   `json:"id" sokel:"id" label:"File ID"`
	Name         string   `json:"name" sokel:"name" label:"File name"`
	MIMEType     string   `json:"mimeType" sokel:"mime_type" label:"MIME type"`
	Size         string   `json:"size" sokel:"size" label:"Size in bytes (decimal string)"`
	ModifiedTime string   `json:"modifiedTime" sokel:"modified_time" label:"Modified time"`
	WebViewLink  string   `json:"webViewLink" sokel:"web_view_link" label:"Web view link"`
	Parents      []string `json:"parents" sokel:"parents" label:"Parent folders"`
	Trashed      bool     `json:"trashed" sokel:"trashed" label:"Trashed"`
}
type ListFiles struct{}

func (ListFiles) Meta() contract.Meta {
	return contract.Meta{ID: "list_files", Label: "List or search files", TimeoutSec: 60}
}
func (ListFiles) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Drive query").Optional().Desc("Drive v3 q syntax; defaults to trashed = false"), field.Int("page_size").Label("Page size").Optional(), field.String("page_token").Label("Page token").Optional()}
}
func (ListFiles) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("files", []FileInfo{}).Label("Files"), field.String("next_page_token").Label("Next page token").Optional()}
}

type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "upload_file", Label: "Upload file", TimeoutSec: 120}
}
func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform file"), field.String("name").Label("File name").Optional(), field.String("parent_id").Label("Destination folder ID").Optional()}
}
func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_id").Label("File ID"), field.String("name").Label("File name"), field.String("mime_type").Label("MIME type")}
}

type DownloadFile struct{}

func (DownloadFile) Meta() contract.Meta {
	return contract.Meta{ID: "download_file", Label: "Download binary file", TimeoutSec: 120}
}
func (DownloadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_id").Label("File ID"), field.String("filename").Label("Output file name").Optional()}
}
func (DownloadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform file")}
}

type ExportFile struct{}

func (ExportFile) Meta() contract.Meta {
	return contract.Meta{ID: "export_file", Label: "Export Workspace file", TimeoutSec: 120}
}
func (ExportFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_id").Label("File ID"), field.String("mime_type").Label("Export MIME type"), field.String("filename").Label("Output file name")}
}
func (ExportFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform file")}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("google", "https://www.googleapis.com/auth/drive.readonly", "https://www.googleapis.com/auth/drive.file", "https://www.googleapis.com/auth/userinfo.email")
}
func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token (injected by platform)").Optional(), field.Secret("refresh_token").Label("Refresh token (managed by platform)").Optional(), field.Text("poll_cursor").Label("Polling cursor (managed automatically)").Optional(), field.Text("next_poll_at").Label("Next poll time (managed automatically)").Optional(), field.Text("watch_changes").Label("Enable change polling").Optional().Desc("Set true to enable; initial setup only records the current position")}
}

type FileChanged struct{}

func (FileChanged) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_changed", Label: "File changed"}
}
func (FileChanged) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_id").Label("File ID"), field.String("name").Label("File name"), field.String("mime_type").Label("MIME type"), field.String("time").Label("Change time"), field.Bool("removed").Label("Access removed or file deleted"), field.Bool("trashed").Label("Trashed")}
}
