# Jira Cloud

使用 Atlassian 账号邮箱和 API 令牌读取、修改 Jira Cloud issue。当前为源码实现，尚未在市场发布。不支持 Jira Server 或 Data Center。

## 凭据与路由

将 `email` 和 `api_token` 保存到 Sokel 凭据。必须使用 API 令牌，不能使用账号密码。无作用域令牌配置 `site` 为 `https://your-site.atlassian.net` 或裸主机名。仅允许单一 HTTPS Atlassian Cloud 站点，不接受自定义域名、端口、URL 用户信息、路径或查询参数。

使用带作用域令牌时，改为填写租户的 `cloud_id` UUID。它优先于 `site`，按 Atlassian 要求通过 `https://api.atlassian.com/ex/jira/{cloud_id}` 访问。Cloud ID 可从站点的 `/_edge/tenant_info` 或组织 Atlassian 管理员处获得。两种路由均使用邮箱和 API 令牌的 Basic 认证，拒绝重定向。令牌会过期，需要手动替换；本插件没有 OAuth 或自动刷新。

授予计划使用操作所需的作用域及对应项目权限。身份检查还要求当前用户接口的权限/作用域。`health_check` 只读请求 `GET /rest/api/3/myself`，返回 `ok` 和脱敏的 `message`。身份检查通过不代表拥有项目的创建、编辑或状态流转权限。

## 操作

| 操作 | 行为 |
| --- | --- |
| `search_issues` | 使用 `POST /rest/api/3/search/jql` 增强版 JQL 查询。 |
| `get_issue` | 根据 key 或数字 ID 读取核心字段。 |
| `create_issue` | 使用项目 key、类型 ID、摘要、可选详情和自定义字段创建。 |
| `update_issue` | 更新给定详情和自定义字段，省略字段保持不变。 |
| `list_transitions` | 列出可用状态流转及必填字段元数据。 |
| `transition_issue` | 根据流转 ID 执行，并可传入必填字段。 |
| `list_comments` | 读取一页评论，正文为原始 ADF JSON。 |
| `add_comment` | 将纯文本转为 Atlassian Document Format 后添加评论。 |
| `health_check` | 读取当前账号身份。 |

查询要求有界 JQL，如 `project = DEMO ORDER BY updated DESC`。接受 `limit`（1–100，默认 50）和 `next_page_token`。持续使用返回的 token，直到 `is_last` 为真，期间保持 JQL 不变。写入后搜索可能短暂延迟，不使用已移除的旧搜索路径。

评论分页使用 `start_at`（默认 0），页大小范围相同，返回 `next_start_at`、`has_more`、`total`。每次列表调用只返回一页。时间戳原样返回。Issue 输出包含 ID、key、摘要、ADF 描述 JSON、状态、负责人账号 ID、标签、更新时间、截止日期和优先级 ID，不包含任意自定义字段。

## 写入与自定义字段

```json
{"project_key":"DEMO","issue_type_id":"10001","summary":"Review earnings report","details":{"description":"Prepare the summary.","labels":["research"]},"custom_fields":{"customfield_10020":"Example"}}
```

类型、优先级、自定义字段、账号 ID 均需从实际 Jira 配置获取，示例 ID 仅为占位符。`details`/`changes` 支持 `summary`、`description`、`assignee_account_id`、`labels`、`due_date`、`priority_id`。创建时只使用顶层 summary。描述和评论文本转为 ADF 段落，换行拆成独立段落，不渲染 Markdown。

更新时省略字段表示保留。空描述清空描述，空负责人取消分配，`labels: []` 移除标签，空截止日期清空日期；其他日期值采用 `YYYY-MM-DD`。摘要和优先级不能清空。类型化可选字段的 JSON null 视为省略，清空请使用文档所述空字符串或数组。

`custom_fields` 只允许 `customfield_<数字>` 键，值采用租户配置要求的 JSON。这里会保留 JSON null，可在 Jira 允许时用于清空。SDK 将动态 JSON 数字绑定为 float64；大整数标识应传字符串，避免精度丢失。该对象不能覆盖内置字段。Jira 会校验字段上下文、必填要求和编辑界面，本版本不提供元数据发现。

先调用 `list_transitions`，其中 `fields_json` 是上游字段元数据 JSON。使用 Jira 要求的结构填写流转必填字段，例如 `{"resolution":{"id":"10000"}}`。动态 `fields` 对象会按原结构传入，也受 float64 数字限制。`update_issue` 不修改状态，应使用 `transition_issue`。Jira 接受更新或流转后返回 `ok: true`。

## 限制与错误

平台操作超时 60 秒，HTTP 超时 55 秒，请求上限 4 MiB，响应上限 8 MiB。错误仅说明状态类别，不回显上游正文、令牌、邮箱或请求 URL。空结果数组合法，缺失列表字段会报错。不自动重试，包括限流。写入超时而结果不确定时，请先检查 Jira 再重试。

本版本不删除 issue、不编辑/删除评论、不管理附件、不发现创建/编辑元数据，也不提供事件触发器。合成样例覆盖请求契约、分页、ADF、凭据隔离和错误处理，尚未进行真实账号验收。
