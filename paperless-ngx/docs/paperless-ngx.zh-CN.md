# Paperless-ngx

在工作流中使用自己的 Paperless-ngx 文档库：搜索提取的文本，读取与更新文档元数据，上传文件进行入库，检查入库任务，下载文档并管理标签。本插件面向稳定版 Paperless-ngx **3.4.1**，明确请求 **API 版本 10**。Paperless-ngx 使用 GPLv3 许可证，无需托管服务账户。

## 配置服务

将必填部署环境变量 `SOKEL_PAPERLESS_URL` 设为**站点根地址**，例如 `http://paperless:8000` 或 `https://documents.example.org/archive`，不要附加 `/api`。支持反向代理路径前缀。地址必须为 HTTP 或 HTTPS，不能包含嵌入凭据、查询参数、片段、编码后的路径段或 `.` / `..` 路径段。可信内网可使用 HTTP；经过不可信网络时请使用 HTTPS。不会回退到公共端点。

在插件的 **API 令牌**凭据字段中填写 Paperless API 令牌。插件发送 `Authorization: Token <token>` 和 `Accept: application/json; version=10`。请通过实例支持的令牌管理或 API 认证流程获取令牌；本插件不收集账户密码，不创建令牌，不实现 OAuth，也不刷新令牌。读取需要查看权限；上传、标签和元数据修改需要相应的新增或修改权限，同时还需要对象级访问权限。

`health_check` 读取最多一条文档的单页列表，返回 `ok` 与 `message`。它检查鉴权、文档可见性及 API 10 响应结构，并不能证明入库 worker、OCR 或写入权限正常。如果服务返回的 `X-Api-Version` 不是 10，请求会失败；不接受旧版 API 9 的任务响应。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 检查经过鉴权的 API 10 文档列表请求。 |
| `list_documents` | 可选 `query` 使用 Paperless 全文搜索语法；可选 `pagination.page` 与 `pagination.page_size`；返回文档、`total_count` 和 `next_page`。 |
| `get_document` | 输入十进制字符串 `document_id`；返回当前或最新版本的元数据与提取文本。 |
| `upload_document` | 输入平台 `file` 及可选 `options.title`、`tags`、`correspondent_id`、`document_type_id`。返回 `accepted` 和入库 `task_id`，不是文档 ID。 |
| `get_ingestion_task` | 输入上传返回的 UUID；返回 `found` 及任务状态、完成标记和 `related_document_ids`。 |
| `download_document` | 输入 `document_id`，可选 `options.original` 与 `version_id`；返回平台文件。 |
| `update_document` | 输入 `document_id` 及 `metadata`，后者至少提供一个支持的修改字段：`title`、`tags`、`correspondent_id` 或 `document_type_id`。 |
| `list_tags` | 返回一页标签，包括 ID、名称、颜色和收件箱标记，以及 `total_count` 与 `next_page`。 |
| `create_tag` | 输入 `name` 和可选 `options.color`（`#RRGGBB`）或 `is_inbox_tag`，创建一个标签。 |

所有文档、版本、标签、往来单位和文档类型 ID 都是无前导零的正 64 位十进制**字符串**。输出 ID 与总数也使用字符串，保留大整数精度。日期按 Paperless 原值返回，包括只有日期部分的创建值，不会转换为本地时间戳。插件仅提供选定的稳定字段，不覆盖 Paperless 的全部字段或管理端点。

## 上传并跟踪入库

提供带有 ID 或内联数据、普通文件名及可选媒体类型的工作流平台文件对象。文件大小必须为 1 字节至 32 MiB，支持中文文件名。不接受本地文件路径或只有 URL 的文件对象。读取平台文件通过 SDK 的平台文件机制进行；插件不会获取任意远程 URL。声明大小和实际读取大小都会检查。

上传将排队执行入库，可能触发 Paperless 工作流、OCR 和已配置的自动化。HTTP 200 和 `accepted: true` 表示请求已被队列接受，**不表示**文档已创建或建立索引。使用返回的 UUID 调用 `get_ingestion_task`。API 10 状态为 `pending`、`started`、`success`、`failure` 和 `revoked`；后三种状态的 `complete` 为 true，只有 `success` 的 `succeeded` 为 true。请从 `related_document_ids` 获取结果文档。ID 列表为空时不能据此推断或构造文档 ID。`found: false` 表示没有返回可见的匹配任务，此时附带的任务字段没有实际含义。任务可能延迟出现、被删除或因权限不可见。插件不会自动轮询或发出事件。

上传选项示例：

```json
{"title":"Quarterly report","tags":["12","18"],"correspondent_id":"3","document_type_id":"5"}
```

上传的 `tags` 按 Paperless 要求以重复的 multipart 表单值发送。已有的往来单位、文档类型和标签 ID 必须对令牌所属用户可见。

## 元数据与下载

元数据更新使用 PATCH。省略的字段保持不变。明确传入空数组 `metadata.tags: []` 会移除全部标签；空标题会作为空字符串发送。往来单位和文档类型 ID 必须为正整数字符串；首版不支持通过 null 清空这些关系，JSON null 按省略处理。最多接受 1000 个不重复标签 ID。标题上限为 255 个 UTF-8 字节，标签名称为 128 字节，搜索查询为 4096 字节。明确提供的 `is_inbox_tag: false` 和 `original: false` 会保留。

Paperless 默认下载归档版本（如存在），否则下载原件。设置 `options.original: true` 可请求原件，可选 `version_id` 用于指定文件版本。响应必须为带普通文件名的有效附件，大小上限为 32 MiB。插件验证响应长度并将字节保存为平台文件；平台上传失败会明确报错。不返回临时外部下载链接，也不通过重定向转发 API 令牌。

## 分页与可靠性

页码从 1 开始，默认每页 50 条，最多 100 条；页码范围为 1–100000。保持查询和每页数量不变，将 `next_page` 作为 `pagination.page` 继续调用，直至为 0。即使当前为短页，也会依据服务端的 `next` 字段继续。下一页必须保持配置的来源、端点、筛选条件和每页数量，并恰好推进一页。插件仅返回验证后的页码，不会直接请求服务端提供的 URL。每次调用返回一页。列表不是快照，并发修改时可能需要按文档或标签 ID 去重。

JSON 响应上限为 8 MiB。上传与下载的平台操作超时为 300 秒，HTTP 超时为 240 秒；其他操作为 60 秒。实例仍需自身的入库 worker、存储及所启用的 OCR 组件。可选的外部邮件、AI 或工作流集成由你的 Paperless 部署控制，不由本插件管理。

插件拒绝重定向，不复用 Cookie，不重试失败的 HTTP 响应，并禁止自动重放写请求正文。错误消息不包含服务响应正文或传输 URL。429 响应会给出有界的 `Retry-After` 等待建议，不会自动重试。上传、创建标签及元数据更新不提供插件级幂等键；发生超时后，应检查任务或当前状态再决定是否再次写入。重复文件检测属于 Paperless 的行为，不能代替恰好一次投递保证。

## 验证与参考

测试覆盖请求字节、API 10 任务状态、multipart 元数据及中文文件名、精确 ID、空标签替换、可选 false、平台文件失败、分页、生成的分发代码、HTTP 错误和重定向拒绝。这些是依据稳定版本源码编写的合成响应协议测试；**本次实现未使用真实运行的 Paperless 服务**。不声称已部署到生产环境。

官方来源：[Paperless-ngx 3.4.1 发布](https://github.com/paperless-ngx/paperless-ngx/releases/tag/v3.4.1)、[稳定版 API 指南](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/docs/api.md)、[API 视图](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/views.py)、[序列化器](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/serialisers.py)、[任务状态定义](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/models.py)及 [GPLv3 许可证](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/LICENSE)。
