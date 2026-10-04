// kbstore-es's operation contracts.
//
// Migration note: 17 fields used to be []map[string]any / map[string]any. This pass checked how
// each one was actually used (buildBool reads f["field"]/f["values"], the time range reads
// from/to, recency reads pivot/boost…) and gave every one of them structure — see types.go. Not a
// single Opaque is left — the shape was always fixed, it just never got written down.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// ChunksBrowse (migrated from the legacy contract)
type ChunksBrowse struct{}

func (ChunksBrowse) Meta() contract.Meta {
	return contract.Meta{ID: "chunks_browse", Internal: true}
}

func (ChunksBrowse) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.Int("k").Default(20),
		field.Int("offset").Optional(),
		field.Array("filters", []Filter{}).Optional(),
		field.Json("time_range", TimeRange{}).Optional(),
	}
}

func (ChunksBrowse) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("chunks", []Chunk{}),
	}
}

// ChunksUpsert (migrated from the legacy contract)
type ChunksUpsert struct{}

func (ChunksUpsert) Meta() contract.Meta {
	return contract.Meta{ID: "chunks_upsert", Internal: true}
}

func (ChunksUpsert) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.String("doc_id"),
		field.Array("chunks", []Chunk{}),
		// append=true: don't delete the doc's existing chunks first. When the platform batches by
		// the NATS single-frame size limit, only the first batch is false — replacing on every
		// batch would delete what the previous batch just wrote.
		field.Bool("append").Optional(),
	}
}

func (ChunksUpsert) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok"),
		field.Int("count"),
	}
}

// DocDelete (migrated from the legacy contract)
type DocDelete struct{}

func (DocDelete) Meta() contract.Meta {
	return contract.Meta{ID: "doc_delete", Internal: true}
}

func (DocDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.String("doc_id"),
	}
}

func (DocDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok"),
	}
}

// KbCreate (migrated from the legacy contract)
type KbCreate struct{}

func (KbCreate) Meta() contract.Meta {
	return contract.Meta{ID: "kb_create", Internal: true}
}

func (KbCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.Int("dims").Default(1024),
		field.Array("fields", []MetaField{}).Label("元数据字段声明").Optional(),
	}
}

func (KbCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok"),
	}
}

// KbDrop (migrated from the legacy contract)
type KbDrop struct{}

func (KbDrop) Meta() contract.Meta {
	return contract.Meta{ID: "kb_drop", Internal: true}
}

func (KbDrop) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.Int("dims").Default(1024),
		field.Array("fields", []MetaField{}).Label("元数据字段声明").Optional(),
	}
}

func (KbDrop) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok"),
	}
}

// KeywordQuery (migrated from the legacy contract)
type KeywordQuery struct{}

func (KeywordQuery) Meta() contract.Meta {
	return contract.Meta{ID: "keyword_query", Internal: true}
}

func (KeywordQuery) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.String("query"),
		field.Int("k").Default(50),
		field.Array("filters", []Filter{}).Optional(),
		field.Json("time_range", TimeRange{}).Optional(),
		field.Json("field_boosts", map[string]float64{}).Optional(),
		field.Json("recency", Recency{}).Optional(),
	}
}

func (KeywordQuery) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("hits", []Hit{}),
	}
}

// Mget (migrated from the legacy contract)
type Mget struct{}

func (Mget) Meta() contract.Meta {
	return contract.Meta{ID: "mget", Internal: true}
}

func (Mget) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.Strings("ids"),
	}
}

func (Mget) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("chunks", []Chunk{}),
	}
}

// VectorQuery (migrated from the legacy contract)
type VectorQuery struct{}

func (VectorQuery) Meta() contract.Meta {
	return contract.Meta{ID: "vector_query", Internal: true}
}

func (VectorQuery) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.Numbers("embedding"),
		field.Int("k").Default(50),
		field.Array("filters", []Filter{}).Optional(),
		field.Json("time_range", TimeRange{}).Optional(),
	}
}

func (VectorQuery) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("hits", []Hit{}),
	}
}

// —— Credential ——

// Credential: an ES connection (issued by the platform's credential manager).
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("es_url").Label("Elasticsearch 地址"),
		field.Secret("api_key").Label("API Key").Optional(),
		field.Text("namespace").Label("索引命名空间").Default("sokel"),
	}
}

// HealthCheck checks this credential — connects to the cluster and reports version and health
// status.
//
// Platform-side convention: the operation id must be health_check (credential.HealthCheckOp); the
// "check" button on the credential page relies on it to decide whether this plugin can be
// health-checked. An output of ok=false + message means unavailable (rather than throwing an
// error — an error would leave only a red X on the UI, unable to say why).
//
// The cost of not having this has actually been paid: when ES goes down, it doesn't blow up until
// ingestion/retrieval is actually run, by which point people are already second-guessing their own
// workflow.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "连一下 Elasticsearch 并报版本、集群名与健康状态"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("version").Label("版本").Optional(),
		field.String("cluster_name").Label("集群名").Optional(),
		field.String("status").Label("集群状态").Optional().Desc("green / yellow / red"),
		field.String("message").Label("说明"),
	}
}
