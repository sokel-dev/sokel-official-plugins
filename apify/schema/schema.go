package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Run struct {
	ID                     string  `json:"id" sokel:"id" label:"Run ID"`
	ActorID                string  `json:"actId" sokel:"actor_id" label:"Actor ID"`
	Status                 string  `json:"status" sokel:"status" label:"Run status"`
	StartedAt              string  `json:"startedAt" sokel:"started_at" label:"Started at"`
	FinishedAt             *string `json:"finishedAt,omitempty" sokel:"finished_at,optional" label:"Finished at"`
	DefaultDatasetID       *string `json:"defaultDatasetId,omitempty" sokel:"default_dataset_id,optional" label:"Default dataset ID"`
	DefaultKeyValueStoreID *string `json:"defaultKeyValueStoreId,omitempty" sokel:"default_key_value_store_id,optional" label:"Default key-value store ID"`
	StatusMessage          *string `json:"statusMessage,omitempty" sokel:"status_message,optional" label:"Status message"`
}
type RunOptions struct {
	TimeoutSeconds    *int     `json:"timeout" sokel:"timeout_seconds,optional" label:"Actor timeout in seconds"`
	MaxItems          *int     `json:"maxItems" sokel:"max_items,optional" label:"Maximum paid dataset items"`
	MaxTotalChargeUSD *float64 `json:"maxTotalChargeUsd" sokel:"max_total_charge_usd,optional" label:"Maximum total charge in USD"`
}
type PageOptions struct {
	Offset *int  `json:"offset" sokel:"offset,optional" label:"Page offset"`
	Limit  *int  `json:"limit" sokel:"limit,optional" label:"Page size"`
	Desc   *bool `json:"desc" sokel:"desc,optional" label:"Descending order"`
}
type Dataset struct {
	ID             string  `json:"id" sokel:"id" label:"Dataset ID"`
	Name           *string `json:"name,omitempty" sokel:"name,optional" label:"Dataset name"`
	ItemCount      int     `json:"itemCount" sokel:"item_count" label:"Item count"`
	CleanItemCount int     `json:"cleanItemCount" sokel:"clean_item_count" label:"Clean item count"`
	CreatedAt      string  `json:"createdAt" sokel:"created_at" label:"Created at"`
	ModifiedAt     string  `json:"modifiedAt" sokel:"modified_at" label:"Modified at"`
}
type RunActor struct{}

func (RunActor) Meta() contract.Meta {
	return contract.Meta{ID: "run_actor", Label: "Start Actor run", TimeoutSec: 60}
}
func (RunActor) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("actor_id").Label("Actor ID or owner~name"), field.Object("input", "Each Actor defines its own input JSON schema.").Label("Actor input").Optional(), field.String("build").Label("Actor build tag or number").Optional(), field.Json("options", RunOptions{}).Label("Run limits").Optional().Desc("max_items limits paid result charges, not the number of returned records.")}
}
func (RunActor) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("run", Run{}).Label("Actor run")}
}

type GetRun struct{}

func (GetRun) Meta() contract.Meta {
	return contract.Meta{ID: "get_run", Label: "Get Actor run", TimeoutSec: 60}
}
func (GetRun) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("run_id").Label("Run ID")}
}
func (GetRun) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("run", Run{}).Label("Actor run")}
}

type ListRuns struct{}

func (ListRuns) Meta() contract.Meta {
	return contract.Meta{ID: "list_runs", Label: "List Actor runs", TimeoutSec: 60}
}
func (ListRuns) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("actor_id").Label("Actor ID or owner~name"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListRuns) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("runs", []Run{}).Label("Actor runs"), field.Int("offset").Label("Page offset"), field.Int("count").Label("Returned count"), field.Int("total").Label("Total count"), field.Int("next_offset").Label("Next offset"), field.Bool("has_more").Label("More results available")}
}

type GetDataset struct{}

func (GetDataset) Meta() contract.Meta {
	return contract.Meta{ID: "get_dataset", Label: "Get dataset metadata", TimeoutSec: 60}
}
func (GetDataset) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("dataset_id").Label("Dataset ID")}
}
func (GetDataset) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("dataset", Dataset{}).Label("Dataset metadata")}
}

type ListDatasetItems struct{}

func (ListDatasetItems) Meta() contract.Meta {
	return contract.Meta{ID: "list_dataset_items", Label: "Read dataset items", TimeoutSec: 60}
}
func (ListDatasetItems) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("dataset_id").Label("Dataset ID"), field.Json("page", PageOptions{}).Label("Pagination").Optional()}
}
func (ListDatasetItems) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Any("items", "Dataset rows are arbitrary JSON objects defined by the Actor that produced them.").Label("Dataset items"), field.Int("offset").Label("Page offset"), field.Int("count").Label("Returned count"), field.Int("total").Label("Total count"), field.Int("next_offset").Label("Next offset"), field.Bool("has_more").Label("More results available")}
}

type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credentials", TimeoutSec: 30}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return nil }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API token").Desc("Create a token in the Apify console. Deployment URLs belong in environment configuration.")}
}
