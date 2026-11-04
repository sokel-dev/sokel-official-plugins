package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type ObjectField struct {
	Name        string   `json:"name" sokel:"name" label:"API field name"`
	Label       string   `json:"label" sokel:"label" label:"Label"`
	Type        string   `json:"type" sokel:"type" label:"Field type"`
	Createable  bool     `json:"createable" sokel:"createable" label:"Can create"`
	Updateable  bool     `json:"updateable" sokel:"updateable" label:"Can update"`
	Nillable    bool     `json:"nillable" sokel:"nillable" label:"Allows null"`
	ExternalID  bool     `json:"externalId" sokel:"external_id" label:"External ID"`
	ReferenceTo []string `json:"referenceTo" sokel:"reference_to" label:"Referenced objects"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type DescribeObject struct{}

func (DescribeObject) Meta() contract.Meta {
	return contract.Meta{ID: "describe_object", Label: "Describe object", TimeoutSec: 60}
}
func (DescribeObject) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("object_name").Label("Object API name")}
}
func (DescribeObject) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("name").Label("Object API name"), field.String("label").Label("Object label"), field.Bool("createable").Label("Can create"), field.Bool("updateable").Label("Can update"), field.Array("fields", []ObjectField{}).Label("Fields")}
}

type Query struct{}

func (Query) Meta() contract.Meta {
	return contract.Meta{ID: "query", Label: "Run SOQL query", TimeoutSec: 60}
}
func (Query) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("soql").Label("Read-only SOQL query")}
}
func (Query) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("records", []map[string]any{}).Label("Records").Desc("Columns and nested relationships depend on the caller SOQL projection and tenant schema"), field.Int("total_size").Label("Total records"), field.Bool("done").Label("Complete"), field.String("next_records_url").Label("Next records path")}
}

type QueryMore struct{}

func (QueryMore) Meta() contract.Meta {
	return contract.Meta{ID: "query_more", Label: "Read next query page", TimeoutSec: 60}
}
func (QueryMore) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("next_records_url").Label("Next records path")}
}
func (QueryMore) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("records", []map[string]any{}).Label("Records").Desc("Columns and nested relationships depend on the caller SOQL projection and tenant schema"), field.Int("total_size").Label("Total records"), field.Bool("done").Label("Complete"), field.String("next_records_url").Label("Next records path")}
}

type GetRecord struct{}

func (GetRecord) Meta() contract.Meta {
	return contract.Meta{ID: "get_record", Label: "Get record", TimeoutSec: 60}
}
func (GetRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("object_name").Label("Object API name"), field.String("record_id").Label("Record ID"), field.Strings("field_names").Label("Requested field API names").Optional()}
}
func (GetRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Object("record", "Fields depend on the Salesforce object and tenant custom schema").Label("Record")}
}

type CreateRecord struct{}

func (CreateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "create_record", Label: "Create record", TimeoutSec: 60}
}
func (CreateRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("object_name").Label("Object API name"), field.Object("fields", "Writable fields depend on tenant object metadata; JSON null clears a nullable field").Label("Field values")}
}
func (CreateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("record_id").Label("Record ID"), field.Bool("success").Label("Successful")}
}

type UpdateRecord struct{}

func (UpdateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "update_record", Label: "Update record", TimeoutSec: 60}
}
func (UpdateRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("object_name").Label("Object API name"), field.String("record_id").Label("Record ID"), field.Object("fields", "Writable fields depend on tenant object metadata; JSON null clears a nullable field").Label("Field values")}
}
func (UpdateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("record_id").Label("Record ID"), field.Bool("success").Label("Successful")}
}

type UpsertRecord struct{}

func (UpsertRecord) Meta() contract.Meta {
	return contract.Meta{ID: "upsert_record", Label: "Upsert by external ID", TimeoutSec: 60}
}
func (UpsertRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("object_name").Label("Object API name"), field.String("external_id_field").Label("External ID field API name"), field.String("external_id").Label("External ID value"), field.Object("fields", "Writable fields depend on tenant object metadata; JSON null clears a nullable field").Label("Field values")}
}
func (UpsertRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("record_id").Label("Record ID"), field.Bool("success").Label("Successful"), field.Bool("created").Label("Created a new record")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("instance_url").Label("Salesforce instance URL").Desc("Use the HTTPS instance_url from Salesforce OAuth; only official Salesforce instance domains are accepted"), field.Secret("access_token").Label("OAuth access token").Desc("Use an OAuth token with api scope and API Enabled permission; refresh externally")}
}
