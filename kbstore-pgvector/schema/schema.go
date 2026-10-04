// kbstore-pgvector 的操作契约 —— **与 kbstore-es 逐字相同**。
//
// 相同是刻意的:平台侧(pipeline_provider.go)只认这 8 个操作,换存储引擎不该改平台一行。
// 差异只允许出现在**凭证**(连的是 Postgres 不是 ES)与实现里。
// 抄的过程本身就是一次契约体检:哪个字段是 ES 才有的概念,抄到这儿会立刻硌手
// (硌到的都记在 docs/contract-notes.md)。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// ChunksBrowse （迁移自旧契约）
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

// ChunksUpsert （迁移自旧契约）
type ChunksUpsert struct{}

func (ChunksUpsert) Meta() contract.Meta {
	return contract.Meta{ID: "chunks_upsert", Internal: true}
}

func (ChunksUpsert) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kb_id"),
		field.String("doc_id"),
		field.Array("chunks", []Chunk{}),
		// append=true：不先删该 doc 的既有 chunk。平台按 NATS 单帧上限分批时，
		// 只有第一批是 false——每批都替换的话，后一批会把前一批刚写的删掉。
		field.Bool("append").Optional(),
	}
}

func (ChunksUpsert) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok"),
		field.Int("count"),
	}
}

// DocDelete （迁移自旧契约）
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

// KbCreate （迁移自旧契约）
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

// KbDrop （迁移自旧契约）
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

// KeywordQuery （迁移自旧契约）
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

// Mget （迁移自旧契约）
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

// VectorQuery （迁移自旧契约）
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

// —— 凭证 ——

// Credential：Postgres(pgvector) 连接（平台凭证下发）。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("pg_url").Label("Postgres 连接串").Desc("postgres://user:pass@host:5432/db?sslmode=disable；需已装 vector 与 pg_trgm 扩展"),
		field.Text("namespace").Label("表名前缀").Default("kb"),
	}
}

// HealthCheck：体检这条凭证 —— 连一下库，报 PostgreSQL 版本与关键扩展。
//
// 平台侧的约定：操作 id 必须是 health_check，凭证页的「检查」按钮据此判断这个插件能不能
// 验活；出参 ok=false + message 表示不可用（而不是抛错——抛错在界面上只剩一个红叉，
// 说不出是连不上、密码错，还是**没装 vector 扩展**，而最后那条正是本插件最常见的部署故障：
// 不体检的话要等到建知识库那一刻才炸，那时人已经在怀疑是自己流程写错了）。
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
