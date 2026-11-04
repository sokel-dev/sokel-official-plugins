# BookStack

在自己的 BookStack 实例上读取和维护知识内容。本插件面向稳定版 **BookStack 26.09.2**、其未带版本号的 `/api` REST 端点，以及使用 MIT 许可证的社区应用。无需 SaaS 账户或公共端点。

## 配置

将必填部署环境变量 `SOKEL_BOOKSTACK_URL` 设为**站点根地址**，例如 `http://bookstack:80` 或 `https://docs.example.org/knowledge`，不要附加 `/api`。支持内网 HTTP 和反向代理路径前缀；可信网络之外请使用 HTTPS。地址不能包含凭据、查询参数、片段、编码后的路径段或点路径段，不会回退到公共服务。

在 BookStack 用户资料中创建 API 令牌，并将其 **Token ID（令牌 ID）**和 **Token secret（令牌密钥）**填入插件凭据表单。用户必须拥有 **Access System API（访问系统 API）**角色权限，以及对应内容的查看或修改权限。插件仅向配置的实例发送 `Authorization: Token <token_id>:<token_secret>`，不创建令牌，不管理用户，不修改权限，也不实现 OAuth 或令牌刷新。`health_check` 读取最多一本书的列表；成功表示该请求的令牌访问有效，不代表具有所有页面的权限。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 经过鉴权读取书籍列表，返回 `ok` 和 `message`。 |
| `list_entities` | `kind` 为 `shelves`、`books`、`chapters` 或 `pages`。可选 `pagination.offset` 与 `count`；返回实体、十进制字符串 `total_count` 和 `next_offset`。 |
| `get_shelf` | 输入 `shelf_id`，返回书架详情及其中可见的书籍。 |
| `get_book` | 输入 `book_id`，返回书籍详情和按顺序展平的 `contents`：每个章节后紧跟其页面，同时包含直接位于书籍下的页面。 |
| `get_chapter` | 输入 `chapter_id`，返回章节详情及其中可见的页面。 |
| `get_page` | 输入 `page_id`，返回页面名称、父级 ID、渲染后的 HTML、存储的 HTML、可用的 Markdown、标签和修订次数。 |
| `search` | 非空 `query` 使用 BookStack 搜索语法。可选 `pagination.page` 与 `count`；返回带类型的实体摘要和 `next_page`。 |
| `create_page` | 输入 `parent_type`（`book` 或 `chapter`）、`parent_id`、`name`、`format`（`html` 或 `markdown`）及非空 `content`。可选 `options.priority`、`tags` 和 `changelog`，立即创建并发布页面。 |
| `update_page` | 输入 `page_id` 及非空 `changes`：可选 `name`、`html` 或 `markdown`、`priority`、`tags`、`changelog`。通过 PUT 仅发送已提供的属性。 |

实体结果类型为 `bookshelf`、`book`、`chapter` 和 `page`。实体 ID 必须结合类型使用，不同类型可能具有相同数字 ID。所有 ID、总数和修订次数均以十进制字符串返回，保留大整数精度。输入 ID 必须为无前导零的正 64 位十进制字符串；不存在的可空父级 ID 会省略。结果仅包含选定的内容字段，不包含用户记录、权限配置、附件或全部上游属性。

## 创建和更新页面

创建输入示例：

```json
{"parent_type":"book","parent_id":"12","name":"Release notes","format":"markdown","content":"# Release notes\n\nChanges for this release.","options":{"priority":0,"tags":[{"name":"Category","value":"Release"}],"changelog":"Initial version"}}
```

部分更新示例：

```json
{"page_id":"42","changes":{"html":"<p>Updated content</p>","priority":0,"tags":[],"changelog":"Refresh content"}}
```

HTML 与 Markdown 只能提供一种。省略的更新字段保持不变；明确提供的空内容字符串会发送给服务以清空内容，空标签数组会移除标签。显式优先级 `0` 会保留，JSON null 按省略处理。页面名称必须为 1–255 个 UTF-8 字节，内容最多 1 MiB，排序优先级范围为 -1000000 至 1000000。最多支持 100 个标签，名称非空，名称和取值分别最多 255 字节。修订说明必须为 1–180 字节。插件不移动页面，不创建书架、书籍或章节，也不删除内容。

页面写入会立即生效，可能创建修订或触发 BookStack 配置的通知和 Webhook。HTML 应使用与 BookStack 编辑器兼容的单层块级普通元素。写入内容时，BookStack 可能将 base64 data URI 图片提取到图片库中。`html` 为渲染后的页面内容，可能包含页面引用；`raw_html` 是编辑时使用的存储源内容。如果最后一次编辑使用了其他编辑器，Markdown 可能为空。返回的 HTML 是内容，不是可信应用模板；下游应用渲染时应进行适当净化。

## 分页

普通列表使用 **offset 偏移量**分页，默认和每次最大 count 为 100，offset 范围为 0–10000000。请求按 ID 升序排序。保持 kind/count 不变，使用 `next_offset` 继续，直至为 0。下一偏移量按实际返回数量推进，因此当 `total` 表示仍有更多结果时，不会将短页静默视为结束。尚未到达报告末尾却返回空页会报错。

搜索使用 **page 页码**分页，从 1 开始，默认 count 为 20，最大 count 为 100，最大 page 为 100000。保持 query/count 不变，使用 `next_page` 继续，直至为 0。插件不会向搜索端点发送普通列表的排序或筛选参数。搜索不返回完整页面内容；需要时使用 `get_page`。列表和搜索结果不是快照，并发变更时可能需要按实体类型和 ID 去重。每次操作仅获取一页。

## 传输与限制

插件仅通过固定 API 路径访问配置的实例，不跟随重定向，不复用 Cookie，不获取任意 URL，也不读取本地文件。错误消息不包含凭据或服务错误正文。JSON 请求上限为 2 MiB，响应上限为 8 MiB。操作允许 60 秒，HTTP 超时为 45 秒。

失败的 HTTP 响应没有自动重试循环，写请求正文也不能被自动重放。429 错误会根据 `Retry-After` 给出有界等待建议。写入超时后应先检查页面再重试；创建没有插件级幂等键，更新没有乐观修订版本前置条件，并发编辑者可能覆盖彼此修改。BookStack 服务器仍需自身的 PHP 和数据库部署；可选邮件或其他外部集成由你控制。

## 验证与来源

测试使用稳定版本的官方 API 请求和响应示例，以及明确标注的合成边界变体，覆盖生成的凭据、输入和输出分发代码、书籍嵌套内容、偏移量与搜索分页区别、精确 ID、空标签和空内容、零优先级、异常成功响应、脱敏与重定向拒绝。**本次实现未运行真实 BookStack 实例**，不声称已进行生产部署。

官方来源：[26.09.2 发布](https://github.com/BookStackApp/BookStack/releases/tag/v26.09.2)、[API 文档](https://demo.bookstackapp.com/api/docs)、[固定版本 API 示例](https://github.com/BookStackApp/BookStack/tree/v26.09.2/dev/api)、[页面 API 控制器](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Entities/Controllers/PageApiController.php)、[搜索 API 控制器](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Search/SearchApiController.php)及 [MIT 许可证](https://github.com/BookStackApp/BookStack/blob/v26.09.2/LICENSE)。
