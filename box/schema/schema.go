package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Item struct {
	ID         string  `json:"id" sokel:"id" label:"Item ID"`
	Type       string  `json:"type" sokel:"type" label:"Item type"`
	Name       string  `json:"name" sokel:"name" label:"Name"`
	Size       *int64  `json:"size,omitempty" sokel:"size,optional" label:"Size in bytes"`
	SHA1       *string `json:"sha1,omitempty" sokel:"sha1,optional" label:"SHA-1 checksum"`
	ETag       *string `json:"etag,omitempty" sokel:"etag,optional" label:"ETag"`
	ModifiedAt *string `json:"modified_at,omitempty" sokel:"modified_at,optional" label:"Modified at"`
}
type SearchPage struct {
	Offset *int `json:"offset,omitempty" sokel:"offset,optional" label:"Page offset"`
	Limit  *int `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return nil }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListFolder struct{}

func (ListFolder) Meta() contract.Meta {
	return contract.Meta{ID: "list_folder", Label: "List folder items", TimeoutSec: 60}
}
func (ListFolder) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("folder_id").Label("Folder ID; 0 means root"), field.String("marker").Label("Pagination marker").Optional()}
}
func (ListFolder) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("items", []Item{}).Label("Items"), field.String("next_marker").Label("Next marker"), field.Bool("has_more").Label("More results available")}
}

type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "Search files and folders", TimeoutSec: 60}
}
func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Search query"), field.String("ancestor_folder_id").Label("Ancestor folder ID").Optional(), field.Json("page", SearchPage{}).Label("Pagination").Optional()}
}
func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("items", []Item{}).Label("Items"), field.Int("total_count").Label("Total offset bound"), field.Int("next_offset").Label("Next offset"), field.Bool("has_more").Label("More results available")}
}

type GetFile struct{}

func (GetFile) Meta() contract.Meta {
	return contract.Meta{ID: "get_file", Label: "Get file metadata", TimeoutSec: 60}
}
func (GetFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_id").Label("File ID")}
}
func (GetFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Item")}
}

type GetFolder struct{}

func (GetFolder) Meta() contract.Meta {
	return contract.Meta{ID: "get_folder", Label: "Get folder metadata", TimeoutSec: 60}
}
func (GetFolder) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("folder_id").Label("Folder ID; 0 means root")}
}
func (GetFolder) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Item")}
}

type CreateFolder struct{}

func (CreateFolder) Meta() contract.Meta {
	return contract.Meta{ID: "create_folder", Label: "Create folder", TimeoutSec: 60}
}
func (CreateFolder) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("parent_id").Label("Parent folder ID; 0 means root"), field.String("name").Label("Folder name")}
}
func (CreateFolder) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Item")}
}

type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "upload_file", Label: "Upload new file", TimeoutSec: 180}
}
func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("parent_id").Label("Parent folder ID; 0 means root"), field.File("file").Label("Platform file")}
}
func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("item", Item{}).Label("Item")}
}

type DownloadFile struct{}

func (DownloadFile) Meta() contract.Meta {
	return contract.Meta{ID: "download_file", Label: "Download file", TimeoutSec: 180}
}
func (DownloadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("file_id").Label("File ID")}
}
func (DownloadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform file"), field.Json("item", Item{}).Label("File metadata")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Supply a current access token. Refresh it outside this plugin before expiry.")}
}
