# Meilisearch

使用你自己的 Meilisearch 社区实例创建索引、导入文档、搜索内容并检查异步索引任务。不需要 Meilisearch Cloud 账户或付费功能。本版本已在自部署 Meilisearch 1.56.1 上完成测试，使用其标准 REST 端点，不配置外部 AI 供应商。

## 部署与凭证

将必填部署环境变量 `SOKEL_MEILISEARCH_URL` 设置为服务 URL，例如 `http://meilisearch:7700` 或 `https://search.example.internal/meili`。插件没有公共默认地址或回退地址，支持内网 HTTP 和反向代理路径前缀。URL 不得包含内嵌凭证、查询参数、片段或点路径段。流量经过不可信网络时请使用 HTTPS。

容器中的 `localhost` 代表当前容器，不是宿主机或其他服务容器。请使用容器网络上可达的服务名，或符合部署情况的宿主机地址。插件不会安装或启动 Meilisearch。

实例启用鉴权时，请填写 **API 密钥**，它将作为 `Authorization: Bearer ...` 发送。只有明确以无鉴权方式部署的实例才支持空密钥。请使用具有所需索引范围和操作权限的普通 API 密钥，本插件不要求主密钥。常用操作权限为 `indexes.get`、`indexes.create`、`search`、`documents.get`、`documents.add`、`documents.delete` 和 `tasks.get`。密钥配置和轮换在插件外完成。

`health_check` 读取一页索引列表，返回 `ok` 与 `message`。它使用需要鉴权的路由，而不是公开的 `/health` 端点，因此公开存活检查成功不会掩盖无效密钥。失败时返回 `ok=false`，不创建数据。仅有搜索权限的密钥可能无法通过该检查，但仍可执行搜索；健康检查需要 `indexes.get`，也不保证写入或任务权限有效。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 检查服务配置与索引列表访问权限。 |
| `list_indexes` | 可选 `page`；返回索引元信息、`total`、`next_offset` 与 `has_more`。 |
| `get_index` | 输入 `index_uid`；返回 UID、可选主键以及创建/更新时间。 |
| `create_index` | 输入 `index_uid`，可选 `primary_key`；返回已入队任务回执。 |
| `search` | 输入 `index_uid`，可选 `query` 与 `options`；返回文档 JSON 命中结果、估计命中总数、处理时间、`next_offset` 和 `may_have_more`。空查询执行占位搜索。 |
| `get_document` | 输入 `index_uid` 和 `document_id`；以精确 JSON 对象字符串返回文档。 |
| `add_documents` | 输入 `index_uid`、`documents_json`，可选 `primary_key`；提交添加或替换操作，返回任务回执。 |
| `delete_document` | 输入 `index_uid` 和一个 `document_id`；提交删除操作，返回任务回执。 |
| `get_task` | 输入字符串 `task_uid`；返回任务元信息、`finished` 和 `succeeded`。只请求一次，不轮询。 |

索引和文档标识符接受 1–512 个 ASCII 字母、数字、连字符或下划线。请使用资源标识符，不要使用 URL。任务 ID 是标准非负十进制字符串，包括 `"0"`，插件保持其数字精度。服务返回 null 时，`primary_key`、任务索引 UID 和任务时间戳可能在输出中缺失。

## 索引写入与文档 JSON

文档字段由应用定义，因此 `documents_json` 是包含非空 JSON 对象数组的字符串，可保留嵌套值及超过 JavaScript 精确整数范围的大整数。插件不强加固定业务文档结构。例如：

```json
{
  "index_uid": "books",
  "primary_key": "id",
  "documents_json": "[{\"id\":\"book-1\",\"title\":\"An Example\",\"available\":false}]"
}
```

`add_documents` 使用**添加或替换**，不是部分更新：同主键的已有文档会被替换，省略的字段会被移除。权限允许时，Meilisearch 可在导入文档过程中创建索引。如果既未提供主键，也没有已配置主键，Meilisearch 会尝试推断。为获得可预测的导入行为，建议明确指定主键。插件验证 JSON 数组，Meilisearch 异步验证主键值与文档语义。因此，语法正确的批次仍可能产生失败任务。

任务成功后，使用 `get_document` 或 `search` 读取已索引内容。文档和命中结果输出为 JSON 字符串，保留 false、null、嵌套值和大整数。插件不会把文档自身的 `error` 或 `code` 字段当成 API 失败。搜索输出受索引展示属性设置和请求投影影响，不一定是完整源文档。

## 任务提交不代表完成

创建、导入和删除通常返回 HTTP 202，状态为 `status=enqueued`。这只说明任务已被接受。保存回执中的 `task.uid`，在后续工作流步骤或定时检查中调用 `get_task`。插件不会等待任务完成，也不会自动重试写入。

| 任务状态 | `finished` | `succeeded` | 含义 |
| --- | --- | --- | --- |
| `enqueued` 或 `processing` | false | false | 仍在等待或处理中。 |
| `succeeded` | true | true | 任务成功完成。 |
| `failed` | true | false | 检查 `task.error_code` 及实例中的任务详情。 |
| `canceled` | true | false | 任务已取消。 |

任务原始错误消息可能包含文档数据或其他敏感细节，因此不返回原文，只返回稳定错误代码。`get_task` 将失败和取消状态作为正常类型化结果提供，供工作流分支处理。必须同时检查完成和成功标记，才能将变更视为已生效。删除原本不存在的 ID 也可能成功完成，因此它不能替代存在性检查。

## 搜索与分页

搜索选项包括 `limit`（默认 20，范围 1–1000）、`offset`（默认 0，范围 0–1000000）、`filter` 表达式、例如 `year:desc` 的 `sort` 字符串列表，以及 `attributes_to_retrieve`。显式零偏移量和空属性数组会被保留。筛选和排序字段必须事先在 Meilisearch 中配置为可筛选或可排序，本插件不修改索引设置。插件支持字符串筛选表达式，不支持完整的嵌套筛选数组语法。

```json
{
  "index_uid": "books",
  "query": "example",
  "options": {
    "limit": 20,
    "offset": 0,
    "filter": "available = false",
    "sort": ["year:desc"],
    "attributes_to_retrieve": ["id", "title"]
  }
}
```

每次搜索获取一页。满页时设置 `may_have_more=true` 并返回下一偏移量，这只表示可能还有一页，不保证总数。保持相同查询和选项，使用返回偏移量继续，直到收到不足一页或空页。`estimated_total_hits` 是估计值，不用于提前终止遍历。Meilisearch 的 `maxTotalHits` 设置限制可搜索窗口，因此搜索不是完整数据库导出。

`list_indexes.page` 使用相同的数量与偏移量范围。在 `has_more=true` 时继续，将 `next_offset` 复制到 `page.offset`；完成时返回零。插件检查索引列表的总数和偏移量一致性。搜索与索引分页都不是快照，并发索引写入或索引变更会影响结果。

## 限制、错误与功能范围

每个导入批次包含 1–1000 个非空文档对象，并须符合 1 MiB HTTP 请求上限。响应最多 8 MiB。查询和筛选字符串最多 16 KiB，主键最多 512 字节，排序/投影列表最多 100 项。操作截止时间为 60 秒，传输超时为 45 秒。Meilisearch 自身的内存、索引、密钥和搜索限制仍然适用。

请求仅使用已配置服务并拒绝重定向。插件不会重试失败的 HTTP 响应或重放写请求正文。HTTP 失败或不完整元信息/回执会明确报错，不回显服务 URL、凭证或上游错误正文。超时可能使写入结果不确定；再次提交前，请检查任务列表或目标文档。失败任务应先修正文档内容，再重试。

本版本不删除索引，不修改设置，不创建 API 密钥，不管理转储或快照，不调用任意 API 路由，不启用企业功能，也不配置 AI 嵌入模型。测试包含依据文档构建的合成请求 fixture，以及一次性本地 Meilisearch 1.56.1 社区容器记录的真实响应。本地验收验证了成功和失败两种异步任务，不涉及生产账户或数据。

## 官方参考与许可证

- [Meilisearch 1.56.1 发布](https://github.com/meilisearch/meilisearch/releases/tag/v1.56.1)
- [仓库许可证划分](https://github.com/meilisearch/meilisearch/blob/v1.56.1/LICENSE) 与 [MIT 社区许可证](https://github.com/meilisearch/meilisearch/blob/v1.56.1/LICENSE-MIT)
- [鉴权](https://www.meilisearch.com/docs/reference/api/authorization)
- [索引列表](https://www.meilisearch.com/docs/reference/api/indexes/list-all-indexes)
- [创建索引](https://www.meilisearch.com/docs/reference/api/indexes/create-index)
- [搜索](https://www.meilisearch.com/docs/reference/api/search/search-with-post)
- [添加或替换文档](https://www.meilisearch.com/docs/reference/api/documents/add-or-replace-documents)
- [删除文档](https://www.meilisearch.com/docs/reference/api/documents/delete-document)
- [读取任务](https://www.meilisearch.com/docs/reference/api/async-task-management/get-task)

上游仓库同时包含 MIT 社区代码和 BUSL-1.1 企业代码。本插件使用社区 REST 能力，不要求企业版；不应将整个上游仓库描述为仅采用 MIT 许可证。
