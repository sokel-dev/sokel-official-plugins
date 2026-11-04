package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type PageOptions struct {
	Page     *int `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	PageSize *int `json:"page_size,omitempty" sokel:"page_size,optional" label:"Page size"`
}
type Document struct {
	ID               string   `json:"id" sokel:"id" label:"Document ID"`
	Title            string   `json:"title" sokel:"title" label:"Title"`
	Content          string   `json:"content" sokel:"content" label:"Extracted text"`
	Tags             []string `json:"tags" sokel:"tags" label:"Tag IDs"`
	CorrespondentID  *string  `json:"correspondent_id,omitempty" sokel:"correspondent_id,optional" label:"Correspondent ID"`
	DocumentTypeID   *string  `json:"document_type_id,omitempty" sokel:"document_type_id,optional" label:"Document type ID"`
	Created          string   `json:"created" sokel:"created" label:"Created date"`
	Modified         string   `json:"modified" sokel:"modified" label:"Modified at"`
	OriginalFileName string   `json:"original_file_name" sokel:"original_file_name" label:"Original filename"`
	MIMEType         string   `json:"mime_type" sokel:"mime_type" label:"Media type"`
}
type Metadata struct {
	Title           *string   `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Tags            *[]string `json:"tags,omitempty" sokel:"tags,optional" label:"Replace tag IDs"`
	CorrespondentID *string   `json:"correspondent_id,omitempty" sokel:"correspondent_id,optional" label:"Correspondent ID"`
	DocumentTypeID  *string   `json:"document_type_id,omitempty" sokel:"document_type_id,optional" label:"Document type ID"`
}
type UploadOptions struct {
	Title           *string  `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Tags            []string `json:"tags,omitempty" sokel:"tags,optional" label:"Tag IDs"`
	CorrespondentID *string  `json:"correspondent_id,omitempty" sokel:"correspondent_id,optional" label:"Correspondent ID"`
	DocumentTypeID  *string  `json:"document_type_id,omitempty" sokel:"document_type_id,optional" label:"Document type ID"`
}
type DownloadOptions struct {
	Original  *bool   `json:"original,omitempty" sokel:"original,optional" label:"Download original"`
	VersionID *string `json:"version_id,omitempty" sokel:"version_id,optional" label:"Document version ID"`
}
type TagOptions struct {
	Color      *string `json:"color,omitempty" sokel:"color,optional" label:"Hex color"`
	IsInboxTag *bool   `json:"is_inbox_tag,omitempty" sokel:"is_inbox_tag,optional" label:"Inbox tag"`
}
type Tag struct {
	ID         string `json:"id" sokel:"id" label:"Tag ID"`
	Name       string `json:"name" sokel:"name" label:"Tag name"`
	Color      string `json:"color" sokel:"color" label:"Hex color"`
	IsInboxTag bool   `json:"is_inbox_tag" sokel:"is_inbox_tag" label:"Inbox tag"`
}
type IngestionTask struct {
	TaskID             string   `json:"task_id" sokel:"task_id" label:"Task UUID"`
	TaskType           string   `json:"task_type" sokel:"task_type" label:"Task type"`
	Status             string   `json:"status" sokel:"status" label:"Task status"`
	Complete           bool     `json:"complete" sokel:"complete" label:"Task finished"`
	Succeeded          bool     `json:"succeeded" sokel:"succeeded" label:"Task succeeded"`
	RelatedDocumentIDs []string `json:"related_document_ids" sokel:"related_document_ids" label:"Related document IDs"`
	DateDone           *string  `json:"date_done,omitempty" sokel:"date_done,optional" label:"Completed at"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check token and service", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Healthy"), field.String("message").Label("Check details")}
}

type ListDocuments struct{}

func (ListDocuments) Meta() contract.Meta {
	return contract.Meta{ID: "list_documents", Label: "Search and list documents", TimeoutSec: 60}
}
func (ListDocuments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Full-text search query").Optional(), field.Json("pagination", PageOptions{}).Label("Pagination").Optional()}
}
func (ListDocuments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("documents", []Document{}).Label("Documents"), field.Int("next_page").Label("Next page; zero means finished"), field.String("total_count").Label("Total document count")}
}

type GetDocument struct{}

func (GetDocument) Meta() contract.Meta {
	return contract.Meta{ID: "get_document", Label: "Get document", TimeoutSec: 60}
}
func (GetDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("document_id").Label("Document ID")}
}
func (GetDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("document", Document{}).Label("Document")}
}

type UploadDocument struct{}

func (UploadDocument) Meta() contract.Meta {
	return contract.Meta{ID: "upload_document", Label: "Upload document for asynchronous ingestion", TimeoutSec: 300}
}
func (UploadDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Platform document file"), field.Json("options", UploadOptions{}).Label("Document metadata").Optional()}
}
func (UploadDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Consumption task UUID"), field.Bool("accepted").Label("Accepted for processing")}
}

type GetIngestionTask struct{}

func (GetIngestionTask) Meta() contract.Meta {
	return contract.Meta{ID: "get_ingestion_task", Label: "Get ingestion task status", TimeoutSec: 60}
}
func (GetIngestionTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_id").Label("Consumption task UUID")}
}
func (GetIngestionTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("found").Label("Task found"), field.Json("task", IngestionTask{}).Label("Task").Optional()}
}

type DownloadDocument struct{}

func (DownloadDocument) Meta() contract.Meta {
	return contract.Meta{ID: "download_document", Label: "Download document to platform file", TimeoutSec: 300}
}
func (DownloadDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("document_id").Label("Document ID"), field.Json("options", DownloadOptions{}).Label("Download options").Optional()}
}
func (DownloadDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.File("file").Label("Downloaded document")}
}

type UpdateDocument struct{}

func (UpdateDocument) Meta() contract.Meta {
	return contract.Meta{ID: "update_document", Label: "Update document metadata", TimeoutSec: 60}
}
func (UpdateDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("document_id").Label("Document ID"), field.Json("metadata", Metadata{}).Label("Metadata changes")}
}
func (UpdateDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("document", Document{}).Label("Document")}
}

type ListTags struct{}

func (ListTags) Meta() contract.Meta {
	return contract.Meta{ID: "list_tags", Label: "List tags", TimeoutSec: 60}
}
func (ListTags) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("pagination", PageOptions{}).Label("Pagination").Optional()}
}
func (ListTags) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tags", []Tag{}).Label("Tags"), field.Int("next_page").Label("Next page; zero means finished"), field.String("total_count").Label("Total tag count")}
}

type CreateTag struct{}

func (CreateTag) Meta() contract.Meta {
	return contract.Meta{ID: "create_tag", Label: "Create tag", TimeoutSec: 60}
}
func (CreateTag) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Tag name"), field.Json("options", TagOptions{}).Label("Tag options").Optional()}
}
func (CreateTag) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("tag", Tag{}).Label("Tag")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("token").Label("API token").Desc("Use a token issued by your configured self-hosted service. The token is sent only to that service.")}
}
