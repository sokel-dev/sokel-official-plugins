package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Record struct {
	ID             string `json:"id" sokel:"id" label:"Record ID"`
	CollectionID   string `json:"collection_id" sokel:"collection_id" label:"Collection ID"`
	CollectionName string `json:"collection_name" sokel:"collection_name" label:"Collection name"`
	JSON           string `json:"json" sokel:"json" label:"Complete record JSON"`
}
type ListOptions struct {
	Page    *int    `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	PerPage *int    `json:"per_page,omitempty" sokel:"per_page,optional" label:"Records per page"`
	Filter  *string `json:"filter,omitempty" sokel:"filter,optional" label:"PocketBase filter expression"`
	Sort    *string `json:"sort,omitempty" sokel:"sort,optional" label:"Sort fields"`
	Expand  *string `json:"expand,omitempty" sokel:"expand,optional" label:"Relations to expand"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check service availability", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Service reachable"), field.String("message").Label("Check details")}
}

type ListRecords struct{}

func (ListRecords) Meta() contract.Meta {
	return contract.Meta{ID: "list_records", Label: "List records", TimeoutSec: 60}
}
func (ListRecords) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection ID or name"), field.Json("options", ListOptions{}).Label("List options").Optional()}
}
func (ListRecords) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("records", []Record{}).Label("Records"), field.String("total").Label("Total records"), field.Int("next_page").Label("Next page; zero means complete"), field.Bool("has_more").Label("More results")}
}

type FindRecord struct{}

func (FindRecord) Meta() contract.Meta {
	return contract.Meta{ID: "find_record", Label: "Find first matching record", TimeoutSec: 60}
}
func (FindRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection ID or name"), field.String("filter").Label("PocketBase filter expression"), field.String("sort").Label("Sort fields").Optional(), field.String("expand").Label("Relations to expand").Optional()}
}
func (FindRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("found").Label("Match found"), field.Array("records", []Record{}).Label("Matching record; empty when not found")}
}

type GetRecord struct{}

func (GetRecord) Meta() contract.Meta {
	return contract.Meta{ID: "get_record", Label: "Get record", TimeoutSec: 60}
}
func (GetRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection ID or name"), field.String("record_id").Label("Record ID"), field.String("expand").Label("Relations to expand").Optional()}
}
func (GetRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("record", Record{}).Label("Record")}
}

type CreateRecord struct{}

func (CreateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "create_record", Label: "Create record", TimeoutSec: 60}
}
func (CreateRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection ID or name"), field.String("data_json").Label("Field values as a JSON object string")}
}
func (CreateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("record", Record{}).Label("Created record")}
}

type UpdateRecord struct{}

func (UpdateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "update_record", Label: "Update record", TimeoutSec: 60}
}
func (UpdateRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection ID or name"), field.String("record_id").Label("Record ID"), field.String("data_json").Label("Field values as a JSON object string")}
}
func (UpdateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("record", Record{}).Label("Updated record")}
}

type DeleteRecord struct{}

func (DeleteRecord) Meta() contract.Meta {
	return contract.Meta{ID: "delete_record", Label: "Delete record", TimeoutSec: 60}
}
func (DeleteRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection ID or name"), field.String("record_id").Label("Record ID")}
}
func (DeleteRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("Deleted")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Access token").Desc("Preobtained auth record token; replace it outside this plugin when expired. Do not use a superuser token.")}
}
