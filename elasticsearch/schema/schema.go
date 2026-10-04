// Package schema declares the operation and credential contracts for the elasticsearch plugin.
//
// Positioning: treat ES as **the workflow's search and write backend** — query
// logs/documents, pump in data, manage indices. It uses plain HTTP instead of the
// official SDK: ES's REST interface has barely changed across 7/8/9, so plain HTTP lets
// one codebase cover all three major versions, **and it even works with OpenSearch**
// (a fork of ES 7); the official SDK, on the other hand, locks the major version to the
// client library.
//
// Four conventions the operation design revolves around:
//
//   - **The query body is passed through verbatim**: query/aggs accept raw ES Query DSL
//     JSON directly, with no extra wrapping. ES's query language is itself the product;
//     wrapping it would only make people read the official docs and still get it wrong.
//     For a quick one-off, use q (Lucene shorthand, e.g. `status:error AND host:a1`).
//   - **total lies**: ES only counts exactly up to 10000 by default; beyond that it
//     returns a lower bound. The contract surfaces a dedicated total_is_lower_bound field
//     to say so — otherwise the false conclusion "there are only 10000 in total" would
//     propagate straight into reports.
//   - **Dangerous operations need a gate**: delete-by-query with no query = wipe the
//     whole index, delete-index with a wildcard = wipe the whole cluster. Both are
//     blocked on the plugin side, forcing an explicit switch to a different operation
//     instead of relying on a steady hand.
//   - **Write and visibility are separate**: a write in ES isn't searchable immediately
//     (refresh defaults to ~1 second). The refresh toggle has a place in the contract with
//     its cost spelled out — "wrote it and immediately queried but couldn't find it" is
//     the most common confusion with this system.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

func indexField() contract.FieldSpec {
	return field.String("index").Label("索引").
		Desc("索引名或别名；多个用逗号分隔，支持通配（如 logs-2026.*）")
}

func queryField() contract.FieldSpec {
	return field.Object("query", "ES Query DSL 的 query 部分，如 "+
		`{"bool":{"must":[{"match":{"message":"timeout"}}]}}`+"；留空 = 全部").
		Label("查询").Optional()
}

// —— search ——

// Search runs a search.
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "search", Label: "搜索", TimeoutSec: 60,
		Desc: "按 Query DSL 或 Lucene 简式检索文档，可带排序、分页与聚合"}
}

func (Search) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		queryField(),
		field.String("q").Label("简式查询").
			Desc("Lucene 语法，如 status:error AND host:a1。与「查询」二选一（都给时以「查询」为准）").Optional(),
		field.Int("size").Label("取几条").
			Desc("默认 10。给了「聚合」而这里留空 = 只回聚合不回文档").Optional(),
		field.Int("from").Label("跳过几条").Desc("分页用；ES 默认最多翻到第 10000 条").Optional(),
		field.Strings("sort").Label("排序").
			Desc("字段:方向，如 @timestamp:desc。可给多个，先给的优先").Optional(),
		field.Strings("source_fields").Label("只取这些字段").
			Desc("留空 = 整个文档。字段多的索引里指定几个能省大量带宽").Optional(),
		field.Object("aggs", "ES 聚合定义，如 "+
			`{"by_host":{"terms":{"field":"host.keyword"}}}`).Label("聚合").Optional(),
		field.Bool("exact_total").Label("精确总数").
			Desc("默认只精确到 10000 条（超出回下界）。要准确总数就打开，大索引上更慢").Optional(),
	}
}

func (Search) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("total").Label("命中总数"),
		field.Bool("total_is_lower_bound").Label("总数是下界").
			Desc("true = 实际命中**不止**这么多（ES 默认统计上限 10000）。要准数打开「精确总数」"),
		field.Array("hits", []Hit{}).Label("命中文档"),
		field.Int("count").Label("本页条数"),
		field.Object("aggregations", "聚合结果，形状由「聚合」定义决定").Label("聚合结果").Optional(),
		field.Int("took_ms").Label("耗时(ms)"),
	}
}

// Hit is one matched document.
type Hit struct {
	ID     string         `sokel:"id" label:"文档 ID"`
	Index  string         `sokel:"index" label:"所在索引"`
	Score  float64        `sokel:"score" label:"相关性得分" desc:"按字段排序时 ES 不算分，这里是 0"`
	Source map[string]any `sokel:"source" label:"文档内容" desc:"原始 _source，字段由索引里的文档决定"`
}

// Count counts matching documents.
type Count struct{}

func (Count) Meta() contract.Meta {
	return contract.Meta{ID: "count", Label: "统计条数",
		Desc: "只数不取文档，且**总数是精确的**（不受搜索那 10000 上限影响）"}
}

func (Count) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		queryField(),
		field.String("q").Label("简式查询").Desc("Lucene 语法；与「查询」二选一").Optional(),
	}
}

func (Count) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("count").Label("条数")}
}

// —— document read/write ——

// DocGet fetches a document.
type DocGet struct{}

func (DocGet) Meta() contract.Meta {
	return contract.Meta{ID: "doc_get", Label: "取文档",
		Desc: "按 ID 取一篇文档；不存在不是错误（found=false）"}
}

func (DocGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.String("id").Label("文档 ID"),
	}
}

func (DocGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("found").Label("找到了"),
		field.Object("source", "文档内容（_source），字段由文档本身决定").Label("内容"),
		field.Int("version").Label("版本号").Optional(),
	}
}

// DocIndex writes a document.
type DocIndex struct{}

func (DocIndex) Meta() contract.Meta {
	return contract.Meta{ID: "doc_index", Label: "写入文档",
		Desc: "写一篇文档；给 ID 就是覆盖写，不给 ID 由 ES 生成"}
}

func (DocIndex) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.Object("document", "文档内容 JSON，字段名照索引 mapping").Label("文档"),
		field.String("id").Label("文档 ID").
			Desc("留空由 ES 生成。**给业务主键当 ID 就天然幂等**——重跑不会写出两条").Optional(),
		field.Bool("refresh").Label("立即可搜").
			Desc("默认关（ES 约 1 秒后自动可搜）。开了这条写入立刻能被搜到，但**每次都强制刷新分片，批量写时很慢**").Optional(),
	}
}

func (DocIndex) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("文档 ID"),
		field.String("result").Label("结果").Desc("created 新建 / updated 覆盖"),
		field.Int("version").Label("版本号"),
	}
}

// DocUpdate updates a document.
type DocUpdate struct{}

func (DocUpdate) Meta() contract.Meta {
	return contract.Meta{ID: "doc_update", Label: "更新文档",
		Desc: "局部更新：只改给出的字段，其余不动（写入文档是整篇覆盖）"}
}

func (DocUpdate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.String("id").Label("文档 ID"),
		field.Object("doc", "要改的字段，如 {\"status\":\"done\"}").Label("字段"),
		field.Bool("upsert").Label("不存在就新建").Desc("默认关：文档不存在时报错").Optional(),
		field.Bool("refresh").Label("立即可搜").Desc("同「写入文档」").Optional(),
	}
}

func (DocUpdate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("result").Label("结果").Desc("updated 改了 / noop 内容一样没动 / created 新建"),
		field.Int("version").Label("版本号"),
	}
}

// DocDelete deletes a document.
type DocDelete struct{}

func (DocDelete) Meta() contract.Meta {
	return contract.Meta{ID: "doc_delete", Label: "删除文档",
		Desc: "按 ID 删一篇；文档本来就没有不是错误（deleted=false）"}
}

func (DocDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.String("id").Label("文档 ID"),
		field.Bool("refresh").Label("立即生效").Desc("同「写入文档」").Optional(),
	}
}

func (DocDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// BulkIndex writes documents in bulk.
type BulkIndex struct{}

func (BulkIndex) Meta() contract.Meta {
	return contract.Meta{ID: "bulk_index", Label: "批量写入", TimeoutSec: 120,
		Desc: "一次写入多篇（一个请求搞定，比逐条快一个量级）"}
}

func (BulkIndex) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.Array("documents", []map[string]any{}).
			Label("文档数组").Desc("每个元素是一篇文档的 JSON"),
		field.String("id_field").Label("拿哪个字段当 ID").
			Desc("留空由 ES 生成 ID。**填上业务主键字段名就天然幂等**——重跑覆盖同一批而不是写出重复").Optional(),
		field.Bool("refresh").Label("立即可搜").Desc("整批写完刷一次；默认关").Optional(),
	}
}

func (BulkIndex) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("indexed").Label("成功条数"),
		field.Int("failed").Label("失败条数"),
		field.Strings("errors").Label("失败原因").
			Desc("最多列前 10 条。**批量是部分成功的**：失败几条不会让整批回滚"),
		field.Int("took_ms").Label("耗时(ms)"),
	}
}

// DeleteByQuery deletes documents matching a query.
type DeleteByQuery struct{}

func (DeleteByQuery) Meta() contract.Meta {
	return contract.Meta{ID: "delete_by_query", Label: "按查询删除", TimeoutSec: 300,
		Desc: "删掉匹配的文档（清理过期数据常用）。**必须给查询**——不给等于删光索引"}
}

func (DeleteByQuery) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.Object("query", "ES Query DSL 的 query 部分。**必填**：留空会被插件拒绝，"+
			"要清空整个索引请用「删除索引」再重建").Label("查询"),
	}
}

func (DeleteByQuery) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("deleted").Label("删除条数"),
		field.Int("took_ms").Label("耗时(ms)"),
	}
}

// —— index management ——

// IndexInfo is a summary of one index.
type IndexInfo struct {
	Name      string `sokel:"name" label:"索引名"`
	Health    string `sokel:"health" label:"健康" desc:"green / yellow / red"`
	Status    string `sokel:"status" label:"状态" desc:"open / close"`
	Docs      int    `sokel:"docs" label:"文档数"`
	StoreSize string `sokel:"store_size" label:"占用空间"`
}

// IndicesList lists indices.
type IndicesList struct{}

func (IndicesList) Meta() contract.Meta {
	return contract.Meta{ID: "indices_list", Label: "索引列表",
		Desc: "列出索引及其健康、文档数与占用空间"}
}

func (IndicesList) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("pattern").Label("匹配模式").Desc("如 logs-*；留空 = 全部（不含系统索引）").Optional(),
	}
}

func (IndicesList) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("indices", []IndexInfo{}).Label("索引"),
		field.Int("count").Label("个数"),
	}
}

// IndexCreate creates an index.
type IndexCreate struct{}

func (IndexCreate) Meta() contract.Meta {
	return contract.Meta{ID: "index_create", Label: "建索引",
		Desc: "建一个索引，可带 mapping 与 settings；已存在不是错误（created=false）"}
}

func (IndexCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("name").Label("索引名").Desc("小写，不能含空格与大部分符号"),
		field.Object("mappings", "字段类型定义，如 "+
			`{"properties":{"title":{"type":"text"},"ts":{"type":"date"}}}`+
			"；留空 = 由 ES 按首批文档猜（动态 mapping）").Label("字段定义").Optional(),
		field.Object("settings", "索引设置，如 "+`{"number_of_shards":1,"number_of_replicas":0}`).
			Label("设置").Optional(),
	}
}

func (IndexCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("created").Label("已创建"),
		field.Bool("existed").Label("本来就有"),
	}
}

// IndexDelete deletes an index.
type IndexDelete struct{}

func (IndexDelete) Meta() contract.Meta {
	return contract.Meta{ID: "index_delete", Label: "删索引",
		Desc: "**整个索引连数据一起删，不可恢复**。插件拒绝通配与 _all（那是删全集群）"}
}

func (IndexDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("name").Label("索引名").Desc("必须是具体的一个名字，不接受 * 或 _all"),
	}
}

func (IndexDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// MappingGet retrieves field definitions (mapping).
type MappingGet struct{}

func (MappingGet) Meta() contract.Meta {
	return contract.Meta{ID: "mapping_get", Label: "看字段定义",
		Desc: "取索引的 mapping（排查「为什么这个字段搜不到」的第一步）"}
}

func (MappingGet) Inputs() []contract.FieldSpec { return []contract.FieldSpec{indexField()} }

func (MappingGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Object("mappings", "各索引的字段定义，键是索引名").Label("字段定义"),
	}
}

// MappingPut adds field definitions (mapping).
type MappingPut struct{}

func (MappingPut) Meta() contract.Meta {
	return contract.Meta{ID: "mapping_put", Label: "加字段定义",
		Desc: "给已有索引**新增**字段定义。**已有字段的类型改不了**（ES 的硬限制，要改只能重建索引再切别名）"}
}

func (MappingPut) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		indexField(),
		field.Object("properties", "新字段定义，如 "+`{"tags":{"type":"keyword"}}`).Label("字段"),
	}
}

func (MappingPut) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("已生效")}
}

// AliasSwitch switches an alias.
type AliasSwitch struct{}

func (AliasSwitch) Meta() contract.Meta {
	return contract.Meta{ID: "alias_switch", Label: "切别名",
		Desc: "把别名原子地指向新索引（重建索引后的切换动作：查询方一直用别名，无感知）"}
}

func (AliasSwitch) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("alias").Label("别名"),
		field.String("to_index").Label("指向索引").Desc("切换后别名指向的索引"),
		field.Bool("keep_others").Label("保留原有指向").
			Desc("默认关：把别名从其它索引上摘掉，只指向新的。开了则是追加").Optional(),
	}
}

func (AliasSwitch) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("已切换"),
		field.Strings("removed_from").Label("从这些索引上摘掉了"),
	}
}

// Reindex rebuilds an index.
type Reindex struct{}

func (Reindex) Meta() contract.Meta {
	return contract.Meta{ID: "reindex", Label: "重建索引", TimeoutSec: 600,
		Desc: "把一个索引的数据灌进另一个（改字段类型的唯一办法）。大索引建议关掉「等它跑完」拿任务 id"}
}

func (Reindex) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("source").Label("源索引"),
		field.String("dest").Label("目标索引").Desc("要先建好（带新的 mapping），否则字段类型还是让 ES 猜"),
		queryField(),
		field.Bool("async").Label("后台跑").
			Desc("默认同步等结果（超时会断，大索引慎用）。打开则立刻返回任务 id，用通用调用查 /_tasks/<id>").Optional(),
	}
}

func (Reindex) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("created").Label("写入条数"),
		field.Int("failures").Label("失败条数"),
		field.String("task").Label("任务 ID").Desc("后台跑时才有").Optional(),
		field.Int("took_ms").Label("耗时(ms)"),
	}
}

// ClusterHealth reports cluster health.
type ClusterHealth struct{}

func (ClusterHealth) Meta() contract.Meta {
	return contract.Meta{ID: "cluster_health", Label: "集群健康",
		Desc: "集群状态与分片情况（做巡检/告警的输入）"}
}

func (ClusterHealth) Inputs() []contract.FieldSpec { return nil }

func (ClusterHealth) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("status").Label("状态").
			Desc("green 都好 / yellow 副本没分配（单节点常态）/ red **有主分片丢了，数据不全**"),
		field.String("cluster_name").Label("集群名"),
		field.Int("nodes").Label("节点数"),
		field.Int("active_shards").Label("活跃分片"),
		field.Int("unassigned_shards").Label("未分配分片"),
	}
}

// —— fallback ——

// Call makes a generic call.
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "通用调用", TimeoutSec: 120,
		Desc: "直达任意 ES REST 接口（上面没覆盖的：_tasks、_ilm、_snapshot、_sql…）"}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("method", field.Opt("GET", "GET"), field.Opt("POST", "POST"),
			field.Opt("PUT", "PUT"), field.Opt("DELETE", "DELETE"), field.Opt("HEAD", "HEAD")).
			Label("方法").Optional(),
		field.String("path").Label("路径").Desc("以 / 开头，如 /_cat/nodes?format=json 或 /my-index/_settings"),
		field.Object("body", "请求体 JSON，键名照 ES 官方文档；GET 也可以带体（ES 允许）").
			Label("请求体").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("status").Label("HTTP 状态码"),
		field.Any("data", "ES 应答 JSON，形状随接口而变，由官方文档定义").Label("应答"),
	}
}

// HealthCheck is the platform's standard credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "连一下集群并报版本、发行版与健康状态"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("version").Label("版本"),
		field.String("distribution").Label("发行版").Desc("elasticsearch / opensearch"),
		field.String("cluster_name").Label("集群名"),
		field.String("status").Label("集群状态").Optional(),
		field.String("message").Label("说明"),
	}
}

// —— credential ——

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("base_url").Label("地址").
			Desc("集群地址，如 https://es.internal:9200（带协议与端口）。多节点填其中一个即可"),
		field.Text("username").Label("用户名").Desc("basic 认证；用 API Key 就留空").Optional(),
		field.Secret("password").Label("密码").Optional(),
		field.Secret("api_key").Label("API Key").
			Desc("Kibana → Stack Management → API Keys 生成的 encoded 值。填了它就不用用户名密码").Optional(),
		field.Select("tls_insecure", "off", "on").Label("跳过证书校验").
			Desc("自建集群用自签证书时选 on（连接仍加密，但不校验对端身份）。默认 off").Optional(),
	}
}
