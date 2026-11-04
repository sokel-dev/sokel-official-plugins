# Microsoft Teams

通过 Microsoft Graph v1.0 浏览团队、频道和已有聊天，读取消息及回复，发送频道消息、频道回复和聊天消息。0.1.0 当前仅为源码，尚未发布到市场。

## 委托凭据与租户同意

使用 Microsoft Entra ID 注册的应用和你方已批准的委托授权流程，取得全球版 Microsoft Graph 访问令牌。将原始令牌保存到 `access_token`，不要添加 `Bearer ` 前缀。本插件没有 OAuth provider 或自动刷新，令牌过期时需从外部更新。不能在此填写客户端密钥、ID token 或刷新令牌。

必须使用已登录的工作或学校用户，不支持个人 Microsoft 账号或 app-only 令牌。每个操作先通过 `User.Read` 只读调用 `GET /me?$select=id`，app-only 令牌会在尝试写入消息前失败。本插件不实现 application-only 迁移/导入 API。发送以委托用户身份执行，用户必须能访问目标会话。

| 操作 | 额外委托权限 |
| --- | --- |
| `health_check` | 除 `User.Read` 外无需其他权限 |
| `list_teams` | `Team.ReadBasic.All` |
| `list_channels` | `Channel.ReadBasic.All` |
| `list_chats` | `Chat.ReadBasic`（或 `Chat.Read`） |
| `list_channel_messages`、`list_replies` | `ChannelMessage.Read.All` |
| `list_chat_messages` | `Chat.Read` |
| `send_channel_message`，含回复 | `ChannelMessage.Send` |
| `send_chat_message` | `ChatMessage.Send` |

频道消息读取权限 `ChannelMessage.Read.All` 需要管理员同意。租户策略也可能要求管理员批准其他权限、应用同意、成员关系或条件访问。同意不会使用户自动加入所有团队、频道或聊天，请只申请所需能力。健康检查返回 `ok` 和脱敏 `message`，通过不代表具备所有 Teams 权限或目标会话访问权。

## 发现与读取

`list_teams` 返回用户直接加入的团队 ID、名称及描述，不包含用户仅通过共享频道访问但未直接加入的宿主团队。`list_channels` 返回选定团队拥有且当前用户可见的频道；不是 allChannels/associated-teams 接口，传入的共享频道可能缺失。私有/共享频道可见性遵循成员关系。

`list_chats` 返回当前用户已经参与的聊天，包含 ID、主题、类型、URL 和时间戳。单聊主题可能为空。本插件不创建聊天、不根据显示名称解析 ID，请使用列表操作获得的 ID。

`list_channel_messages` 读取频道根消息，回复需使用 `list_replies` 并传入团队、频道及根消息 ID。`list_chat_messages` 读取已有聊天。输出消息 ID、正文及格式、上游提供的作者 ID/名称、创建/更新/删除时间、父消息 ID、网页 URL 和消息类型。系统消息或已删除消息的作者/正文可能为空。HTML 原样作为内容返回，插件不渲染或净化。

不返回附件、提及、表情回应、托管内容、成员展开或完整原始消息 JSON。频道排序采用 Graph 回复链修改时间规则，聊天消息采用 Graph 默认排序；本版本不提供自定义过滤或排序。

## 发送与回复

`send_channel_message` 要求 `team_id`、`channel_id`、`content`。填写 `reply_to_id` 会回复对应频道根消息，省略则发新根消息。`send_chat_message` 要求已有 `chat_id` 和 `content`。

```json
{"team_id":"<team-id>","channel_id":"<channel-id>","reply_to_id":"<message-id>","content":"The analysis is ready.","content_type":"text"}
```

`content_type` 支持 `text`（默认）或 `html`。Teams 客户端限制 HTML，不支持任意样式。不提供 Markdown 转换、提及、附件或自适应卡片。插件正文上限 64 KiB，Microsoft 可能根据编码和会话策略实施更低的实际限制。

发送成功要求 HTTP 201 且返回消息 ID。不自动重试，没有幂等键。超时而结果不确定时，先检查会话再重试，避免重复消息。只发送面向读者的消息，不用于日志灌入或批量迁移。

## 分页与运行限制

聊天和消息列表每页 1–50，默认 50。团队及频道列表采用服务端页大小，因为接口不支持 `$top`。所有列表返回 `next_link`，将其原样传回同一操作并保持资源 ID 不变，直到为空。插件保留完整不透明查询，校验 HTTPS、精确 `graph.microsoft.com` 域名、API 版本及资源路径，拒绝其他用户、频道、聊天或外部链接。

每次调用重新验证委托用户身份。用户列表使用经验证的 `/users/{id}` 路径，将分页绑定到同一身份；其他操作保持精确团队/频道/聊天路径。不接受主权云、其他国家云或用户自定义 base URL。

平台操作超时 60 秒，每次 HTTP 超时 55 秒。JSON 请求上限 4 MiB，响应上限 16 MiB。拒绝重定向，错误不包含上游正文、URL 或凭据。限流后需等待再尝试。不提供自动触发器、订阅、编辑或删除。测试使用官方 API 结构的合成样例，尚未进行真实账号验收或实际发送消息。

参考：[已加入团队](https://learn.microsoft.com/en-us/graph/api/user-list-joinedteams?view=graph-rest-1.0)、[频道](https://learn.microsoft.com/en-us/graph/api/channel-list?view=graph-rest-1.0)、[聊天](https://learn.microsoft.com/en-us/graph/api/chat-list?view=graph-rest-1.0)、[频道消息](https://learn.microsoft.com/en-us/graph/api/channel-list-messages?view=graph-rest-1.0)、[回复](https://learn.microsoft.com/en-us/graph/api/chatmessage-list-replies?view=graph-rest-1.0)、[发送频道消息](https://learn.microsoft.com/en-us/graph/api/channel-post-messages?view=graph-rest-1.0)、[发送聊天消息](https://learn.microsoft.com/en-us/graph/api/chat-post-messages?view=graph-rest-1.0)、[权限](https://learn.microsoft.com/en-us/graph/permissions-reference)。
