package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Index struct {
	UID        string  `json:"uid" sokel:"uid" label:"Index UID"`
	PrimaryKey *string `json:"primary_key,omitempty" sokel:"primary_key,optional" label:"Primary key"`
	CreatedAt  string  `json:"created_at" sokel:"created_at" label:"Created at"`
	UpdatedAt  string  `json:"updated_at" sokel:"updated_at" label:"Updated at"`
}
type Page struct {
	Limit  *int `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Offset *int `json:"offset,omitempty" sokel:"offset,optional" label:"Offset"`
}
type SearchOptions struct {
	Limit      *int     `json:"limit,omitempty" sokel:"limit,optional" label:"Result limit"`
	Offset     *int     `json:"offset,omitempty" sokel:"offset,optional" label:"Result offset"`
	Filter     *string  `json:"filter,omitempty" sokel:"filter,optional" label:"Filter expression"`
	Sort       []string `json:"sort,omitempty" sokel:"sort,optional" label:"Sort expressions"`
	Attributes []string `json:"attributes_to_retrieve,omitempty" sokel:"attributes_to_retrieve,optional" label:"Attributes to retrieve"`
}
type Document struct {
	JSON string `json:"json" sokel:"json" label:"Document JSON"`
}
type TaskReceipt struct {
	UID        string  `json:"uid" sokel:"uid" label:"Task UID"`
	IndexUID   *string `json:"index_uid,omitempty" sokel:"index_uid,optional" label:"Index UID"`
	Status     string  `json:"status" sokel:"status" label:"Task status"`
	Type       string  `json:"type" sokel:"type" label:"Task type"`
	EnqueuedAt string  `json:"enqueued_at" sokel:"enqueued_at" label:"Enqueued at"`
}
type Task struct {
	UID        string  `json:"uid" sokel:"uid" label:"Task UID"`
	IndexUID   *string `json:"index_uid,omitempty" sokel:"index_uid,optional" label:"Index UID"`
	Status     string  `json:"status" sokel:"status" label:"Task status"`
	Type       string  `json:"type" sokel:"type" label:"Task type"`
	EnqueuedAt string  `json:"enqueued_at" sokel:"enqueued_at" label:"Enqueued at"`
	StartedAt  *string `json:"started_at,omitempty" sokel:"started_at,optional" label:"Started at"`
	FinishedAt *string `json:"finished_at,omitempty" sokel:"finished_at,optional" label:"Finished at"`
	ErrorCode  *string `json:"error_code,omitempty" sokel:"error_code,optional" label:"Task error code"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check connection and key", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListIndexes struct{}

func (ListIndexes) Meta() contract.Meta {
	return contract.Meta{ID: "list_indexes", Label: "List indexes", TimeoutSec: 60}
}
func (ListIndexes) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("page", Page{}).Label("Pagination").Optional()}
}
func (ListIndexes) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("indexes", []Index{}).Label("Indexes"), field.Int("total").Label("Total indexes"), field.Int("next_offset").Label("Next offset; zero means complete"), field.Bool("has_more").Label("More results")}
}

type GetIndex struct{}

func (GetIndex) Meta() contract.Meta {
	return contract.Meta{ID: "get_index", Label: "Get index", TimeoutSec: 60}
}
func (GetIndex) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("index_uid").Label("Index UID")}
}
func (GetIndex) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("index", Index{}).Label("Index")}
}

type CreateIndex struct{}

func (CreateIndex) Meta() contract.Meta {
	return contract.Meta{ID: "create_index", Label: "Create index", TimeoutSec: 60}
}
func (CreateIndex) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("index_uid").Label("Index UID"), field.String("primary_key").Label("Primary key").Optional()}
}
func (CreateIndex) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", TaskReceipt{}).Label("Enqueued task")}
}

type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "Search documents", TimeoutSec: 60}
}
func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("index_uid").Label("Index UID"), field.String("query").Label("Search query").Optional(), field.Json("options", SearchOptions{}).Label("Search options").Optional()}
}
func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("hits", []Document{}).Label("Matching documents"), field.Int("estimated_total_hits").Label("Estimated total hits"), field.Int("processing_time_ms").Label("Processing time in milliseconds"), field.Int("next_offset").Label("Next result offset"), field.Bool("may_have_more").Label("A further result page may exist")}
}

type GetDocument struct{}

func (GetDocument) Meta() contract.Meta {
	return contract.Meta{ID: "get_document", Label: "Get document", TimeoutSec: 60}
}
func (GetDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("index_uid").Label("Index UID"), field.String("document_id").Label("Document ID")}
}
func (GetDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("document_json").Label("Document JSON")}
}

type AddDocuments struct{}

func (AddDocuments) Meta() contract.Meta {
	return contract.Meta{ID: "add_documents", Label: "Add or replace documents", TimeoutSec: 60}
}
func (AddDocuments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("index_uid").Label("Index UID"), field.String("documents_json").Label("Document array JSON"), field.String("primary_key").Label("Primary key").Optional()}
}
func (AddDocuments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", TaskReceipt{}).Label("Enqueued task")}
}

type DeleteDocument struct{}

func (DeleteDocument) Meta() contract.Meta {
	return contract.Meta{ID: "delete_document", Label: "Delete document", TimeoutSec: 60}
}
func (DeleteDocument) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("index_uid").Label("Index UID"), field.String("document_id").Label("Document ID")}
}
func (DeleteDocument) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", TaskReceipt{}).Label("Enqueued task")}
}

type GetTask struct{}

func (GetTask) Meta() contract.Meta {
	return contract.Meta{ID: "get_task", Label: "Get asynchronous task", TimeoutSec: 60}
}
func (GetTask) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("task_uid").Label("Task UID")}
}
func (GetTask) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("task", Task{}).Label("Task"), field.Bool("finished").Label("Terminal task state"), field.Bool("succeeded").Label("Task succeeded")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Optional().Desc("Optional for an unprotected self-hosted instance. Use a key with permissions for the requested operations.")}
}
