// kbstore-pgvector's operation contracts — **identical, byte for byte, to kbstore-es's**.
//
// The sameness is deliberate: the platform side (pipeline_provider.go) only recognizes these 8
// operations, and swapping the storage engine shouldn't require changing a single line of the
// platform. Differences are only allowed to show up in the **credential** (connecting to Postgres,
// not ES) and in the implementation.
// The act of copying is itself a contract health check: whichever field turns out to be an
// ES-only concept will snag immediately when copied here (whatever snags gets recorded in
// docs/contract-notes.md).
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

// Credential: a Postgres (pgvector) connection (issued by the platform's credential manager).
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("pg_url").Label("Postgres 连接串").Desc("postgres://user:pass@host:5432/db?sslmode=disable；需已装 vector 与 pg_trgm 扩展"),
		field.Text("namespace").Label("表名前缀").Default("kb"),
	}
}

// HealthCheck checks this credential — connects to the database and reports the PostgreSQL version
// and key extensions.
//
// Platform-side convention: the operation id must be health_check; the "check" button on the
// credential page relies on it to decide whether this plugin can be health-checked. An output of
// ok=false + message means unavailable (rather than throwing an error — an error would leave only
// a red X on the UI, unable to say whether it's a connection failure, a wrong password, or
// **a missing vector extension**, and that last one is this plugin's most common deployment
// failure: without this check it wouldn't blow up until the moment a knowledge base gets created,
// by which point people are already second-guessing their own workflow).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "连一下 Postgres 并报版本与 vector / pg_trgm 扩展是否就位"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("version").Label("版本").Optional(),
		field.String("extensions").Label("已装扩展").Optional().Desc("本插件需要 vector（必需）与 pg_trgm（关键词检索）"),
		field.String("message").Label("说明"),
	}
}
