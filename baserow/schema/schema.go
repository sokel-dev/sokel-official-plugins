package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Table struct {
	ID         string `json:"id" sokel:"id" label:"Table ID"`
	Name       string `json:"name" sokel:"name" label:"Table name"`
	DatabaseID string `json:"database_id" sokel:"database_id" label:"Database ID"`
}
type TableField struct {
	ID             string `json:"id" sokel:"id" label:"Field ID"`
	Name           string `json:"name" sokel:"name" label:"Field name"`
	Type           string `json:"type" sokel:"type" label:"Field type"`
	Primary        bool   `json:"primary" sokel:"primary" label:"Primary field"`
	DefinitionJSON string `json:"definition_json" sokel:"definition_json" label:"Complete field definition JSON"`
}
type Row struct {
	ID   string `json:"id" sokel:"id" label:"Row ID"`
	JSON string `json:"json" sokel:"json" label:"Complete row JSON"`
}
type RowOptions struct {
	Page           *int    `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	Size           *int    `json:"size,omitempty" sokel:"size,optional" label:"Page size"`
	Search         *string `json:"search,omitempty" sokel:"search,optional" label:"Search text"`
	OrderBy        *string `json:"order_by,omitempty" sokel:"order_by,optional" label:"Sort fields"`
	FiltersJSON    *string `json:"filters_json,omitempty" sokel:"filters_json,optional" label:"Filter tree JSON"`
	UserFieldNames *bool   `json:"user_field_names,omitempty" sokel:"user_field_names,optional" label:"Use field names instead of field IDs"`
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check connection and token", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListTables struct{}

func (ListTables) Meta() contract.Meta {
	return contract.Meta{ID: "list_tables", Label: "List accessible tables", TimeoutSec: 60}
}
func (ListTables) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListTables) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("tables", []Table{}).Label("Tables")}
}

type ListFields struct{}

func (ListFields) Meta() contract.Meta {
	return contract.Meta{ID: "list_fields", Label: "List table fields", TimeoutSec: 60}
}
func (ListFields) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("table_id").Label("Table ID")}
}
func (ListFields) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("fields", []TableField{}).Label("Fields")}
}

type ListRows struct{}

func (ListRows) Meta() contract.Meta {
	return contract.Meta{ID: "list_rows", Label: "List rows", TimeoutSec: 60}
}
func (ListRows) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("table_id").Label("Table ID"), field.Json("options", RowOptions{}).Label("Row query options").Optional()}
}
func (ListRows) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("rows", []Row{}).Label("Rows"), field.String("total").Label("Total row count"), field.Int("next_page").Label("Next page; zero means complete"), field.Bool("has_more").Label("More results")}
}

type GetRow struct{}

func (GetRow) Meta() contract.Meta {
	return contract.Meta{ID: "get_row", Label: "Get row", TimeoutSec: 60}
}
func (GetRow) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("table_id").Label("Table ID"), field.String("row_id").Label("Row ID"), field.Bool("user_field_names").Label("Use field names instead of field IDs").Optional()}
}
func (GetRow) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("row", Row{}).Label("Row")}
}

type CreateRow struct{}

func (CreateRow) Meta() contract.Meta {
	return contract.Meta{ID: "create_row", Label: "Create row", TimeoutSec: 60}
}
func (CreateRow) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("table_id").Label("Table ID"), field.String("data_json").Label("Field values as a JSON object string"), field.Bool("user_field_names").Label("Use field names instead of field IDs").Optional()}
}
func (CreateRow) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("row", Row{}).Label("Created row")}
}

type UpdateRow struct{}

func (UpdateRow) Meta() contract.Meta {
	return contract.Meta{ID: "update_row", Label: "Update row fields", TimeoutSec: 60}
}
func (UpdateRow) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("table_id").Label("Table ID"), field.String("row_id").Label("Row ID"), field.String("data_json").Label("Field values as a JSON object string"), field.Bool("user_field_names").Label("Use field names instead of field IDs").Optional()}
}
func (UpdateRow) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("row", Row{}).Label("Updated row")}
}

type DeleteRow struct{}

func (DeleteRow) Meta() contract.Meta {
	return contract.Meta{ID: "delete_row", Label: "Delete row", TimeoutSec: 60}
}
func (DeleteRow) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("table_id").Label("Table ID"), field.String("row_id").Label("Row ID")}
}
func (DeleteRow) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("Deleted")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("access_token").Label("Database token").Desc("Use a database token with the required table permissions; JWT account tokens are not supported.")}
}
