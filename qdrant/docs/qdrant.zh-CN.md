# Qdrant

将 Sokel 连接到你自己的 Qdrant 实例，管理稠密向量集合、写入点、读取载荷、执行相似度查询并遍历点。Qdrant 可在本地或自有服务器运行，不需要云账户、付费服务或外部嵌入模型供应商。请由自己的工作流提供嵌入向量。这是直接访问数据库的集成；平台知识库存储后端需要另行配置。本版本已在 Qdrant 1.20.0 上完成测试，并遵循其 REST API。

## 部署与凭证

将必填部署环境变量 `SOKEL_QDRANT_URL` 设置为 Qdrant REST 服务 URL，例如 `http://qdrant:6333` 或 `https://vector.example.internal/qdrant`。插件没有默认服务或公共地址回退。内网部署可使用 HTTP，也可包含反向代理路径前缀。URL 不能包含用户名、密码、查询、片段或点路径段。流量经过不可信网络时应使用 HTTPS。

容器中的 `localhost` 指向该容器自身。请使用容器网络上可达的服务名，或部署中配置的宿主机地址；将插件指向自身的 `localhost` 无法访问另一个 Qdrant 容器。请配置 REST 端口，而不是 gRPC 端口。插件不会安装或启动 Qdrant。

实例要求鉴权时，在凭证表单填写 **API 密钥**。只有明确配置为不鉴权的实例才应留空。密钥通过 `api-key` 请求头发送。读取操作需要集合读取权限，变更操作需要相应写入权限。密钥创建和轮换在插件外完成。

`health_check` 读取集合列表，返回 `ok` 与 `message`；配置、传输或鉴权失败时返回 `ok=false`。它只确认集合列表访问有效，不代表拥有写入权限或每个集合都已就绪，也不会创建数据。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 验证已配置服务及集合列表访问权限。 |
| `list_collections` | 返回集合名称。该端点一次返回完整列表，受响应大小限制，没有分页输入。 |
| `get_collection` | 输入 `collection`；返回名称、状态、可选的近似点数/已索引向量数及 `config_json`。 |
| `create_collection` | 输入 `collection`、`size`、`distance`，可选 `vector_name`；创建一个无名或命名稠密向量配置。仅在 Qdrant 确认创建后返回 `created=true`。 |
| `query_points` | 输入 `collection` 和数字 `vector`，可选 `vector_name` 与 `options`；返回 ID、可选载荷 JSON 和相似度分数。 |
| `upsert_points` | 输入 `collection`、1–256 个类型化 `points`，可选 `vector_name`；写入 ID、向量和可选载荷，返回已完成写入回执。 |
| `retrieve_points` | 输入 `collection` 和 1–256 个 `ids`；返回匹配点及 `missing_ids`。ID 不存在属于正常结果。 |
| `scroll_points` | 输入 `collection`，可选 `options`；返回一页点、`next_offset` 与 `has_more`。 |
| `delete_points` | 输入 `collection` 和 1–256 个明确的 `ids`；删除这些 ID 并返回已完成写入回执。不提供按筛选条件批量删除。 |

集合名称输入接受 1–255 个 ASCII 字母、数字、连字符或下划线。列表可能显示其他名称，但本版本无法操作不符合该子集的集合。稠密向量必须包含 1–65536 个有限数字，并与集合维度一致。距离度量必须为 `Cosine`、`Euclid`、`Dot` 或 `Manhattan`。命名向量是集合内独立的配置；创建、写入和查询时请使用相同名称。本版本不支持稀疏向量、多向量或自动推理。

## 点 ID、载荷与写入

Sokel 中的每个点 ID 都是**字符串**：可以是标准无符号 64 位十进制字符串（包括 `"0"`），或带连字符的 UUID。十进制 ID 作为精确 JSON 整数发送；UUID 作为字符串发送并统一为小写。`"18446744073709551615"` 这样的 ID 不会经过浮点转换。同一次请求中的重复 ID 会被拒绝。

例如，创建 `size=3`、`distance=Cosine` 的集合后，可执行以下更新或插入：

```json
{
  "collection": "books",
  "points": [
    {"id": "0", "vector": [1, 0, 0], "payload_json": "{\"title\":\"Example\",\"available\":false}"},
    {"id": "76874cce-1fb9-4e16-9b0b-f085ac06ed6f", "vector": [0, 1, 0]}
  ]
}
```

载荷字段由应用自行定义，因此 `payload_json` 是可选 JSON 对象字符串。`{}` 有效，JSON 数组或 null 无效。载荷对象及返回的集合配置保留为 JSON 字符串，以保持任意嵌套字段和大整数的精度。null 或缺失载荷保持缺失。读取操作明确请求载荷，不返回存储的向量。

Upsert 会替换同 ID 的已有点，不是部分载荷更新。只有当提供的记录确实代表你要存储的点时才应使用，尤其要注意包含多个命名向量的集合。同一批次的向量维度必须一致。写入使用 `wait=true` 和 30 秒上游超时。只有点写入回执状态为 `completed` 时，插件才确认完成；`acknowledged` 不会被当作完成。操作 ID 可能不存在。完成回执不会说明删除前有多少个目标 ID 实际存在。

## 查询与遍历

`query_points.options` 支持 `limit`（默认 10，范围 1–1000）、`offset`（默认 0，范围 0–1000000）、可选 `score_threshold` 和可选 `filter_json`。显式零值会被保留。分数和阈值的含义取决于距离度量。`filter_json` 是 Qdrant 筛选对象字符串；插件验证 JSON 结构，Qdrant 验证筛选语义。例如：

```json
{
  "collection": "books",
  "vector": [1, 0, 0],
  "options": {
    "limit": 10,
    "score_threshold": 0,
    "filter_json": "{\"must\":[{\"key\":\"available\",\"match\":{\"value\":false}}]}"
  }
}
```

向量查询只返回一页排序结果，不是完整记录导出，也不返回总数。按 ID 遍历点请使用 `scroll_points`。其选项为 `limit`（默认 100，范围 1–1000）、可选 `offset` 和可选 `filter_json`。首次省略 `offset`；在 `has_more=true` 时，将 `next_offset` 原样传入 `options.offset`，保持集合和筛选条件不变。偏移量可能是 `"0"` 或 UUID，不要将其解析成浮点数。Qdrant 的 `next_page_offset=null` 表示遍历结束。续页元信息缺失或重复会导致操作失败。并发写入会影响结果，遍历不是快照。

## 限制与失败

每个载荷/筛选 JSON 对象最多 512 KiB。HTTP 请求最多 1 MiB，响应最多 8 MiB。向量或载荷较大时，请缩小批次或分页数量。操作截止时间为 60 秒，传输超时为 45 秒；点操作请求 30 秒服务器超时。Qdrant 自身的存储、内存、访问与查询限制仍然适用。

插件拒绝重定向，仅使用已配置服务，不会重试失败的 HTTP 响应或重放写请求正文，也不会回显上游原始错误正文、服务 URL 或凭证。HTTP 错误、非 OK 的 Qdrant 响应、不完整回执和无效分页会明确失败。超时后请先检查集合或点，再重复写入，因为请求可能已经生效。

本版本不删除集合，不管理别名、快照或分片，不更新载荷索引，不执行任意 API 请求，也不管理云账户。所选操作可用于自部署开源服务器。测试同时包含依据文档构建的合成请求 fixture，以及一次性本地 Qdrant 1.20.0 容器记录的真实响应，不涉及生产账户或数据。

## 官方参考

- [Qdrant 1.20.0 发布](https://github.com/qdrant/qdrant/releases/tag/v1.20.0)
- [Apache-2.0 许可证](https://github.com/qdrant/qdrant/blob/v1.20.0/LICENSE)
- [创建集合](https://api.qdrant.tech/api-reference/collections/create-collection)
- [集合详情](https://api.qdrant.tech/api-reference/collections/get-collection)
- [查询点](https://api.qdrant.tech/api-reference/search/query-points)
- [更新或插入点](https://api.qdrant.tech/api-reference/points/upsert-points)
- [读取点](https://api.qdrant.tech/api-reference/points/get-points)
- [遍历点](https://api.qdrant.tech/api-reference/points/scroll-points)
- [删除点](https://api.qdrant.tech/api-reference/points/delete-points)
