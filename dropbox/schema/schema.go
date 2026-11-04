package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Entry struct {
	Kind           string  `json:".tag" sokel:"kind" label:"Item kind"`
	Name           string  `json:"name" sokel:"name" label:"Name"`
	ID             *string `json:"id,omitempty" sokel:"id,optional" label:"Item ID"`
	PathLower      *string `json:"path_lower,omitempty" sokel:"path_lower,optional" label:"Lowercase path"`
	PathDisplay    *string `json:"path_display,omitempty" sokel:"path_display,optional" label:"Display path"`
	Size           *int64  `json:"size,omitempty" sokel:"size,optional" label:"Size in bytes"`
	Rev            *string `json:"rev,omitempty" sokel:"rev,optional" label:"Revision"`
	ServerModified *string `json:"server_modified,omitempty" sokel:"server_modified,optional" label:"Server modification time"`
	IsDownloadable *bool   `json:"is_downloadable,omitempty" sokel:"is_downloadable,optional" label:"Downloadable"`
}
type ListOptions struct {
	Recursive      *bool `json:"recursive,omitempty" sokel:"recursive,optional" label:"Include subfolders"`
	IncludeDeleted *bool `json:"include_deleted,omitempty" sokel:"include_deleted,optional" label:"Include deleted entries"`
	Limit          *int  `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
}
type SearchOptions struct {
	Path         *string `json:"path,omitempty" sokel:"path,optional" label:"Search folder path"`
	FilenameOnly *bool   `json:"filename_only,omitempty" sokel:"filename_only,optional" label:"Match filenames only"`
	MaxResults   *int    `json:"max_results,omitempty" sokel:"max_results,optional" label:"Page size"`
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
	return contract.Meta{ID: "list_folder", Label: "List folder", TimeoutSec: 60}
}
func (ListFolder) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Folder path; empty means root").Optional(), field.Json("options", ListOptions{}).Label("List options").Optional()}
}
func (ListFolder) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("entries", []Entry{}).Label("Entries"), field.String("cursor").Label("Cursor"), field.Bool("has_more").Label("More results available")}
}

type ContinueList struct{}

func (ContinueList) Meta() contract.Meta {
	return contract.Meta{ID: "continue_list", Label: "Continue listing or read changes", TimeoutSec: 60}
}
func (ContinueList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("cursor").Label("List cursor")}
}
func (ContinueList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("entries", []Entry{}).Label("Entries"), field.String("cursor").Label("Cursor"), field.Bool("has_more").Label("More results available")}
}

type GetMetadata struct{}

func (GetMetadata) Meta() contract.Meta {
	return contract.Meta{ID: "get_metadata", Label: "Get file or folder metadata", TimeoutSec: 60}
}
func (GetMetadata) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Path or item ID")}
}
func (GetMetadata) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("entry", Entry{}).Label("Entry")}
}

type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "Search files and folders", TimeoutSec: 60}
}
func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Search query"), field.Json("options", SearchOptions{}).Label("Search options").Optional()}
}
func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("entries", []Entry{}).Label("Entries"), field.String("cursor").Label("Cursor"), field.Bool("has_more").Label("More results available")}
}

type ContinueSearch struct{}

func (ContinueSearch) Meta() contract.Meta {
	return contract.Meta{ID: "continue_search", Label: "Continue search", TimeoutSec: 60}
}
func (ContinueSearch) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("cursor").Label("Search cursor")}
}
func (ContinueSearch) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("entries", []Entry{}).Label("Entries"), field.String("cursor").Label("Cursor"), field.Bool("has_more").Label("More results available")}
}

type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "upload_file", Label: "Upload file without overwriting", TimeoutSec: 180}
}
func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("Destination file path"), field.File("file").Label("Platform file")}
}
func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("entry", Entry{}).Label("Uploaded file")}
}

type DownloadFile struct{}

func (DownloadFile) Meta() contract.Meta {
	return contract.Meta{ID: "download_file", Label: "Download file", TimeoutSec: 180}
}
func (DownloadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("path").Label("File path or ID")}
}
func (DownloadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform file"), field.Json("entry", Entry{}).Label("File metadata")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Supply a current access token. Refresh it outside this plugin before expiry.")}
}
