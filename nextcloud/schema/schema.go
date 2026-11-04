package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("user_id").Label("User ID").Desc("Use the canonical Nextcloud user ID shown for WebDAV, not a display name or email alias."),
		field.Secret("app_password").Label("App password").Desc("Create an app password in personal security settings. Configure the installation URL with SOKEL_NEXTCLOUD_URL."),
	}
}

type Resource struct {
	Path        string  `json:"path" sokel:"path" label:"Path"`
	Name        string  `json:"name" sokel:"name" label:"Name"`
	IsDirectory bool    `json:"is_directory" sokel:"is_directory" label:"Is directory"`
	FileID      *string `json:"file_id,omitempty" sokel:"file_id,optional" label:"Nextcloud file ID"`
	ETag        *string `json:"etag,omitempty" sokel:"etag,optional" label:"ETag"`
	Size        *int64  `json:"size,omitempty" sokel:"size,optional" label:"Size in bytes"`
	ContentType *string `json:"content_type,omitempty" sokel:"content_type,optional" label:"Content type"`
	ModifiedAt  *string `json:"modified_at,omitempty" sokel:"modified_at,optional" label:"Last modified"`
}
type WriteReceipt struct {
	Created bool    `json:"created" sokel:"created" label:"Created new file"`
	ETag    *string `json:"etag,omitempty" sokel:"etag,optional" label:"ETag"`
}

type GetInfo struct{}

func (GetInfo) Meta() contract.Meta {
	return contract.Meta{ID: "get_info", Label: "Get file or folder metadata", TimeoutSec: 60}
}
func (GetInfo) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Path").Optional()}
}
func (GetInfo) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("resource", Resource{}).Label("Resource")}
}

type ListDirectory struct{}

func (ListDirectory) Meta() contract.Meta {
	return contract.Meta{ID: "list_directory", Label: "List directory", TimeoutSec: 60}
}
func (ListDirectory) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Directory path").Optional()}
}
func (ListDirectory) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("directory", Resource{}).Label("Directory"), field.Array("children", []Resource{}).Label("Children")}
}

type CreateDirectory struct{}

func (CreateDirectory) Meta() contract.Meta {
	return contract.Meta{ID: "create_directory", Label: "Create directory", TimeoutSec: 60}
}
func (CreateDirectory) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Directory path")}
}
func (CreateDirectory) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("created").Label("Created")}
}

type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "upload_file", Label: "Upload file", TimeoutSec: 180}
}
func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Destination path"), field.File("file").Label("Platform file"), field.Bool("overwrite").Label("Allow overwrite").Optional(), field.String("if_match").Label("Expected ETag").Optional()}
}
func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("receipt", WriteReceipt{}).Label("Upload receipt")}
}

type DownloadFile struct{}

func (DownloadFile) Meta() contract.Meta {
	return contract.Meta{ID: "download_file", Label: "Download file", TimeoutSec: 180}
}
func (DownloadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("File path"), field.String("output_name").Label("Output filename").Optional()}
}
func (DownloadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform file")}
}

type MoveResource struct{}

func (MoveResource) Meta() contract.Meta {
	return contract.Meta{ID: "move_resource", Label: "Move file or directory", TimeoutSec: 60}
}
func (MoveResource) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("source_path").Label("Source path"), field.String("destination_path").Label("Destination path"), field.String("if_match").Label("Expected ETag").Optional()}
}
func (MoveResource) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("moved").Label("Moved")}
}
