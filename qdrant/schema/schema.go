package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Collection struct {
	Name string `json:"name" sokel:"name" label:"Collection name"`
}
type CollectionInfo struct {
	Name                string `json:"name" sokel:"name" label:"Collection name"`
	Status              string `json:"status" sokel:"status" label:"Collection status"`
	PointsCount         *int64 `json:"points_count,omitempty" sokel:"points_count,optional" label:"Approximate point count"`
	IndexedVectorsCount *int64 `json:"indexed_vectors_count,omitempty" sokel:"indexed_vectors_count,optional" label:"Indexed vector count"`
	ConfigJSON          string `json:"config_json" sokel:"config_json" label:"Collection configuration JSON"`
}
type Point struct {
	ID          string   `json:"id" sokel:"id" label:"Point ID"`
	PayloadJSON *string  `json:"payload_json,omitempty" sokel:"payload_json,optional" label:"Payload JSON"`
	Score       *float64 `json:"score,omitempty" sokel:"score,optional" label:"Similarity score"`
}
type PointInput struct {
	ID          string    `json:"id" sokel:"id" label:"Point ID"`
	Vector      []float64 `json:"vector" sokel:"vector" label:"Dense vector"`
	PayloadJSON *string   `json:"payload_json,omitempty" sokel:"payload_json,optional" label:"Payload JSON"`
}
type QueryOptions struct {
	Limit          *int     `json:"limit,omitempty" sokel:"limit,optional" label:"Result limit"`
	Offset         *int     `json:"offset,omitempty" sokel:"offset,optional" label:"Result offset"`
	ScoreThreshold *float64 `json:"score_threshold,omitempty" sokel:"score_threshold,optional" label:"Score threshold"`
	FilterJSON     *string  `json:"filter_json,omitempty" sokel:"filter_json,optional" label:"Qdrant filter JSON"`
}
type ScrollOptions struct {
	Limit      *int    `json:"limit,omitempty" sokel:"limit,optional" label:"Page size"`
	Offset     *string `json:"offset,omitempty" sokel:"offset,optional" label:"Next point offset"`
	FilterJSON *string `json:"filter_json,omitempty" sokel:"filter_json,optional" label:"Qdrant filter JSON"`
}
type WriteReceipt struct {
	Status      string  `json:"status" sokel:"status" label:"Write status"`
	OperationID *string `json:"operation_id,omitempty" sokel:"operation_id,optional" label:"Operation ID"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check connection and key", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Usable"), field.String("message").Label("Check details")}
}

type ListCollections struct{}

func (ListCollections) Meta() contract.Meta {
	return contract.Meta{ID: "list_collections", Label: "List collections", TimeoutSec: 60}
}
func (ListCollections) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (ListCollections) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("collections", []Collection{}).Label("Collections")}
}

type GetCollection struct{}

func (GetCollection) Meta() contract.Meta {
	return contract.Meta{ID: "get_collection", Label: "Get collection", TimeoutSec: 60}
}
func (GetCollection) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name")}
}
func (GetCollection) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("collection", CollectionInfo{}).Label("Collection")}
}

type CreateCollection struct{}

func (CreateCollection) Meta() contract.Meta {
	return contract.Meta{ID: "create_collection", Label: "Create dense-vector collection", TimeoutSec: 60}
}
func (CreateCollection) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name"), field.Int("size").Label("Vector dimensions"), field.String("distance").Label("Distance metric"), field.String("vector_name").Label("Named vector").Optional()}
}
func (CreateCollection) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("created").Label("Created")}
}

type QueryPoints struct{}

func (QueryPoints) Meta() contract.Meta {
	return contract.Meta{ID: "query_points", Label: "Query similar points", TimeoutSec: 60}
}
func (QueryPoints) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name"), field.Numbers("vector").Label("Dense vector"), field.String("vector_name").Label("Named vector").Optional(), field.Json("options", QueryOptions{}).Label("Query options").Optional()}
}
func (QueryPoints) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("points", []Point{}).Label("Points")}
}

type UpsertPoints struct{}

func (UpsertPoints) Meta() contract.Meta {
	return contract.Meta{ID: "upsert_points", Label: "Upsert points", TimeoutSec: 60}
}
func (UpsertPoints) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name"), field.Array("points", []PointInput{}).Label("Points to upsert"), field.String("vector_name").Label("Named vector").Optional()}
}
func (UpsertPoints) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("receipt", WriteReceipt{}).Label("Completed write receipt")}
}

type RetrievePoints struct{}

func (RetrievePoints) Meta() contract.Meta {
	return contract.Meta{ID: "retrieve_points", Label: "Retrieve points", TimeoutSec: 60}
}
func (RetrievePoints) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name"), field.Strings("ids").Label("Point IDs")}
}
func (RetrievePoints) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("points", []Point{}).Label("Points"), field.Strings("missing_ids").Label("IDs not returned")}
}

type ScrollPoints struct{}

func (ScrollPoints) Meta() contract.Meta {
	return contract.Meta{ID: "scroll_points", Label: "Scroll points", TimeoutSec: 60}
}
func (ScrollPoints) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name"), field.Json("options", ScrollOptions{}).Label("Scroll options").Optional()}
}
func (ScrollPoints) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("points", []Point{}).Label("Points"), field.String("next_offset").Label("Next point offset"), field.Bool("has_more").Label("More results")}
}

type DeletePoints struct{}

func (DeletePoints) Meta() contract.Meta {
	return contract.Meta{ID: "delete_points", Label: "Delete points by ID", TimeoutSec: 60}
}
func (DeletePoints) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("collection").Label("Collection name"), field.Strings("ids").Label("Point IDs")}
}
func (DeletePoints) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("receipt", WriteReceipt{}).Label("Completed write receipt")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("API key").Optional().Desc("Optional for an unprotected self-hosted instance. Use a key with permissions for the requested operations.")}
}
