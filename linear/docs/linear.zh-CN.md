# Linear

使用个人 API 密钥连接 Linear 工作区。当前为源码实现，尚未在市场发布。

## 凭据与权限

在 Linear 账号的 API 设置中创建个人 API 密钥，保存至 Sokel 凭据的 `api_key`。仅授予工作流需要的团队及读写权限。本插件使用个人密钥的 Authorization 格式，不支持 OAuth 访问令牌。组织策略可能限制个人密钥。

`health_check` 只读查询 GraphQL viewer，返回 `ok` 和已脱敏的 `message`。身份检查通过不代表可以访问所有 issue 或团队。密钥仅发送到 `https://api.linear.app/graphql`，所有重定向均被拒绝。

## 操作

| 操作 | 行为 |
| --- | --- |
| `list_teams` | 读取有权限访问的团队 ID、标识和名称。 |
| `list_issues` | 读取一页 issue，可按团队、项目、负责人、状态或忽略大小写的标题子串过滤。 |
| `get_issue` | 根据 UUID 或 `ENG-123` 等标识读取 issue。 |
| `list_projects` | 读取一页项目，可按忽略大小写的名称子串过滤。 |
| `create_issue` | 使用 `team_id`、`title` 和可选的 `details` 创建 issue。 |
| `update_issue` | 仅修改 `changes` 中提供的字段。 |
| `health_check` | 只读检查当前身份。 |

列表接受 `limit`（1–100，默认 50）和 `cursor`。当 `has_more` 为真时，将 `next_cursor` 传给下一次调用，并保持过滤条件不变。每次只返回一页，沿用 Linear 默认排除归档资源的行为。这是过滤查询，不是全局全文搜索。

Issue 输出包含 ID、标识、标题、Markdown 描述、URL、优先级、截止日期、更新时间、团队、状态、负责人和项目。项目输出包含 ID、名称、描述、URL、更新时间和目标日期。时间戳保留 Linear 原始值，日期输入采用 `YYYY-MM-DD`。

## 创建与更新

先调用 `list_teams` 获取团队 UUID。创建示例：

```json
{"team_id":"<team-uuid>","title":"Review earnings report","details":{"description":"Prepare the summary.","priority":2}}
```

`details` 和 `changes` 支持 `title`、`description`、`state_id`、`assignee_id`、`project_id`、`priority`、`due_date`。创建时 title 只能放在顶层。状态、负责人和项目必须使用 API ID，而不是显示名称。本版本不提供状态列表，可从现有 issue 或 Linear API 获取有效状态 ID。

更新时省略字段表示不修改。优先级 `0` 明确取消优先级，1–4 分别表示紧急、高、普通、低。空描述会清空描述；负责人、项目或截止日期的空字符串会清空对应值。标题和状态不能清空。类型化契约将 JSON `null` 视为省略；清空请使用文档所述空字符串。

```json
{"issue_id":"ENG-123","changes":{"priority":0,"assignee_id":"","description":""}}
```

## 限制与错误

平台操作超时 60 秒，HTTP 超时 55 秒，请求上限 4 MiB，响应上限 8 MiB。HTTP 和 GraphQL 错误返回脱敏英文信息，不包含上游错误正文或凭据。带 errors 的部分成功 GraphQL 响应会被拒绝。缺失列表字段视为错误，明确的空列表合法。

不自动重试。超时或无法确认写入结果时，先检查 Linear 再重试，避免重复创建。限流后需等待。本版本不创建项目、不上传附件、不管理评论，也没有事件触发器。目前尚未进行真实账号验收，测试使用合成协议样例。
