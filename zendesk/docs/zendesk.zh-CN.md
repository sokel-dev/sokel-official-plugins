# Zendesk

通过面向客服坐席的 Ticketing API v2 读取和管理 Zendesk Support 工单。

## 凭据与访问权限

将 `subdomain` 设置为 `.zendesk.com` 前的账户名称，不要包含完整网址或后缀。`email` 为坐席或管理员邮箱，`api_token` 为在 Zendesk Admin Center 创建的 API 令牌。账户必须启用 API 令牌访问。鉴权使用 Basic auth，用户名为 `email/token`，密码为 API 令牌。

令牌以所填用户的权限、角色和工单可见范围执行操作。本插件不提供 OAuth、令牌刷新、密码鉴权、终端用户 Requests API、自定义代理域名或 Zendesk Help Center 文章功能。`health_check` 调用 `/users/me.json`，返回 `ok` 和脱敏的 `message`；终端用户账户不视为可执行这些坐席操作的有效账户。

## 读取工单和评论

`list_tickets` 读取一页游标分页结果。`list_comments` 还需 `ticket_id`。两者都支持 `limit`（1–100，默认 50）和可选 `cursor`。保持工单 ID 和操作不变，在 `has_more=true` 时传入 `next_cursor` 继续读取。插件使用不透明游标，不访问服务返回的分页网址。末页为空是合法情况。

`get_ticket` 读取单张工单。工单输出包括主题、描述、状态、优先级、请求人与处理人 ID、标签和时间戳。ID 使用字符串保留大整数精度。缺失的可选字符串为空字符串。评论包含正文、作者 ID、是否公开和创建时间，不返回附件或 HTML 正文。

`search_tickets` 的 `query` 使用 Zendesk 搜索语法，`page` 为 1 至 10 的页码，默认 1。插件添加 `type:ticket`，请勿再指定冲突的资源类型。每页请求 100 条，`has_more=true` 时使用 `next_page` 继续。搜索最多提供前 1,000 条匹配结果；`total_count` 为服务端完整匹配数，`truncated=true` 表示需缩小查询范围才能取全。新改动进入搜索索引可能延迟数分钟。

## 写入工单和评论

`create_ticket` 需要 `subject`、初始评论 `body`、已有的 `requester_id` 和明确的 `public` 布尔值。`public=false` 创建内部备注，`true` 创建公开回复。账户触发规则和通知规则仍会生效。可选 `details` 支持状态、优先级、处理人 ID 和标签。初始评论通过 `ticket.comment` 发送，不写只读的 description 字段。

`update_ticket` 需要 `ticket_id` 和非空 `changes`。支持主题、状态、优先级、处理人 ID 和标签，省略字段表示保持不变。空优先级或处理人 ID 会发送 JSON null；空标签数组会清空全部标签，非空标签数组也会替换原有完整列表。状态限于 `new`、`open`、`pending`、`hold`、`solved`，不包含关闭工单或自定义状态 ID。

`add_comment` 需要工单 ID、正文和明确的公开标记。它通过 Tickets API 更新 `ticket.comment`，返回更新后的工单，不伪造评论 ID。该操作不能修改已有评论。创建工单和评论不具备幂等性。

更新和添加评论都可提供 `updated_stamp`，其值为最近一次读取工单获得的完整 RFC3339 `updated_at`。提供后会发送 `safe_update=true`，并发修改会导致失败；省略则为无条件更新。处理冲突前应先重新读取最新工单。

## 限制与失败处理

每个操作的平台超时为 60 秒，HTTP 超时为 55 秒。JSON 请求和响应上限为 8 MiB。请求只发送到所配置账户的 HTTPS `.zendesk.com` 域名，拒绝重定向，不自动重试写入。HTTP 错误和异常回执会明确失败，不展示敏感上游响应。创建或评论结果不确定时，重试前先检查工单，避免重复记录或消息。分页不是事务快照。

开发期间未向真实账户写入，也未发布到市场。测试使用合成协议夹具，不是真实流量抓取。本版本不提供触发器、Webhook、自定义字段编辑、上传、批量任务或增量导出。

## 官方参考

- [Tickets API](https://developer.zendesk.com/api-reference/ticketing/tickets/tickets/)
- [工单评论](https://developer.zendesk.com/api-reference/ticketing/tickets/ticket_comments/)
- [搜索及 1,000 条结果限制](https://developer.zendesk.com/api-reference/ticketing/ticket-management/search/)
- [游标分页](https://developer.zendesk.com/documentation/api-basics/pagination/paginating-through-lists-using-cursor-pagination/)
