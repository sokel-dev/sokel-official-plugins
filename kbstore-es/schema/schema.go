// kbstore-es 的操作契约。
//
// 迁移记录：此前 17 个字段是 []map[string]any / map[string]any，本次逐个查了实际用法
// （buildBool 取 f["field"]/f["values"]、时间范围取 from/to、recency 取 pivot/boost…）
// 全部补出了结构，见 types.go。一个 Opaque 都没剩——形状本来就是确定的，只是当年没写。
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

// Credential：ES 连接（平台凭证下发）。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("es_url").Label("Elasticsearch 地址"),
		field.Secret("api_key").Label("API Key").Optional(),
		field.Text("namespace").Label("索引命名空间").Default("sokel"),
	}
}

// HealthCheck：体检这条凭证 —— 连一下集群，报版本与健康状态。
//
// 平台侧的约定：操作 id 必须是 health_check（credential.HealthCheckOp），
// 凭证页的「检查」按钮据此判断这个插件能不能验活；出参 ok=false + message 表示
// 不可用（而不是抛错——抛错在界面上只剩一个红叉，说不出为什么）。
//
// 知识库存储没有它的后果实报过：ES 挂了要等到摄入/检索真跑才炸，而那时人已经
// 在怀疑是自己的流程写错了。
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
