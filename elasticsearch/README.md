# elasticsearch — Elasticsearch 插件（第一方内置目录）

18 个操作覆盖三个域：检索（搜索/统计/聚合）、文档读写（单篇 CRUD + 批量 + 按查询删）、
索引管理（列表/建删/mapping/别名/重建）+ `call` 保底 + `health_check`。
说明书 [docs/elasticsearch.md](docs/elasticsearch.md)。

**走裸 HTTP 而不是官方 SDK**：ES 的 REST 接口在 7/8/9 之间基本没动，裸 HTTP 一套代码
通吃三个大版本，连 **OpenSearch**（ES 7 的分叉）也能用；官方 SDK 反过来会把大版本
锁死在客户端库上。同一判断见 `../kbstore-es`。

## 坑（改代码前先读）

- **`_bulk` 是 NDJSON 不是 JSON**：一行动作、一行文档，**末尾必须有换行**，
  Content-Type 得是 `application/x-ndjson`（发成 JSON 数组 ES 回 400/406）。测试钉着。
- **404 有两种**：文档不存在（应答无 `error` 体）是正常分支，索引不存在（有 `error` 体）
  是配置错必须报出来。混为一谈的话「查不到东西」会被当成「没数据」。测试钉着。
- **切别名必须一次 `_aliases` 请求里 remove+add**：分两次调用中间那一瞬别名指向空
  或同时指向两个索引，查询方撞上就读到错的数据。测试钉着调用次数。
- **`total` 默认是下界**：ES 只精确统计到 10000 条，`relation=gte` 时出参
  `total_is_lower_bound=true`。不透出这件事，「一共 10000 条」会一路传进报表。
- **size 的 0 是有意义的**（只要聚合不要文档），而留空的数字字段到手也是 0——
  用「有没有给 aggs」断意图：显式 size>0 用它，否则有 aggs 就 size=0，都没有走 ES 默认 10。
- **危险操作在插件侧挡**：`_delete_by_query` 不给 query = 删光索引；`DELETE /*` = 删光集群。
  两处都在发请求前拒绝并指向正确的操作。测试钉着「不该发出请求」。
- **`resource_already_exists` 是幂等结果不是错误**：建索引的流程会重跑。
- 认证：API Key（`Authorization: ApiKey`）优先于 basic；两个都不填也允许
  （自建集群关安全模块 / OpenSearch demo 配置是常见形态）。
- `InsecureSkipVerify` 换的是 Transport，**两种形态各留一个 http.Client**，
  别每次调用新建——那样连接池不复用，每次请求都重新握 TLS。

## 没做事件源

ES 没有推送机制，「有新文档」只能轮询查询。要事件驱动的话上游多半有更合适的出口
（消息队列 / Redis Stream / Webhook）。真需要轮询再加，别默认塞一个。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

测试用 httptest 假 ES，不依赖外部集群。
