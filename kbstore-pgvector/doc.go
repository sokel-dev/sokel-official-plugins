package main

// usageDoc：插件详情页里显示的使用说明（随握手上报）。
const usageDoc = `# kbstore-pgvector（知识库存储：Postgres + pgvector）

与 **kbstore-es** 实现同一份存储契约（8 个内部操作），可整库替换。
它的第一用途其实是**给契约做体检**：只有一个实现时，契约里混进多少 ES 的形状是看不出来的。

## 依赖

- Postgres 13+，装 ` + "`vector`" + `（pgvector ≥0.5，要 HNSW）与 ` + "`pg_trgm`" + ` 扩展。
- 凭证只有两项：连接串 ` + "`pg_url`" + `、表名前缀 ` + "`namespace`" + `（默认 kb）。

配好点「检查凭证」：报 PostgreSQL 版本与 ` + "`vector`" + ` / ` + "`pg_trgm`" + ` 是否就位。
缺扩展是这里最常见的部署故障，不体检的话要等到建知识库那一刻才炸。

一库一表（` + "`<namespace>_<kb_id>`" + `），因为 pgvector 的 ` + "`vector(N)`" + ` 把维度写死在列类型上，
不同知识库维度不同，塞一张表建不出索引。

## 与 ES 版的**能力差异**（选型前必读）

| 维度 | kbstore-es | 本插件 |
|---|---|---|
| 向量检索 | HNSW / cosine | HNSW / cosine（等价） |
| 关键词检索 | BM25 + ik 中文分词 | **trigram 相似度** |
| 时效加权 | distance_feature | 未实现（recency 入参被忽略） |
| 字段加权 | multi_match ^boost | 固定加权（title×1.5 / summary×1.2） |

**关键词那一栏是真差距**：Postgres 自带分词器对中文无能为力（'simple' 按空白切，
一句中文就是一个巨型 token，to_tsquery 命中率近 0），要做中文 BM25 得装 zhparser/pg_jieba。
trigram 对中英混排都能给出有意义的排序，但召回质量弱于 ik，长查询尤甚。
中文为主、且关键词腿重要的库，仍应选 ES。
`
