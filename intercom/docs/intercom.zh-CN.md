# Intercom

通过 Intercom REST API 管理一组常用联系人与会话功能。请求明确使用 `Intercom-Version: 2.15`，这是已发布且受支持的 API 版本；插件不会悄悄选择最新版本。插件支持美国、欧洲和澳大利亚区域托管，每个区域使用固定地址。

## 凭证与权限

为 Intercom 工作区创建私有应用并获取访问令牌，在 Sokel 中将其保存为 **访问令牌**。请按需授予联系人和会话读写权限；以管理员身份回复还取决于该管理员的访问权限。令牌不会绕过工作区权限或套餐限制。令牌续期和轮换在插件外进行；本插件不提供 OAuth 授权或刷新流程。

将 **工作区区域** 设置为与托管区域对应的 `us`、`eu` 或 `au`。留空表示美国。插件分别使用 `api.intercom.io`、`api.eu.intercom.io` 或 `api.au.intercom.io`，拒绝任意 URL，也不会通过重定向发现其他区域。鉴权失败时，请先确认令牌和区域再重试。

运行 `health_check`。它执行只读 `GET /me` 请求验证当前管理员，返回 `ok` 和 `message`。凭证不可用时返回 `ok=false`，不抛出处理器错误。检查成功只能确认身份访问有效，不代表所有联系人或会话权限均可用。该操作不会发送消息或修改联系人。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 读取当前管理员以检查令牌。 |
| `list_contacts` | 可选 `page`；返回联系人 ID、角色、选定的可空资料字段、邮件订阅状态和分页字段。 |
| `get_contact` | 输入 `contact_id`；返回选定的联系人字段。 |
| `create_contact` | 输入纯 `email` 地址，可选 `role`（默认为 `user`，也可为 `lead`）和 `fields`。返回创建的联系人。 |
| `update_contact` | 输入 `contact_id` 和非空 `fields` 对象。返回更新后的联系人。 |
| `search_contacts` | 输入纯 `email` 地址，可选 `page`；执行精确 `email = value` 查询并返回联系人和分页字段。不开放任意搜索表达式。 |
| `list_conversations` | 可选 `page`；返回会话 ID、标题、状态、打开/已读标记、创建/更新时间和分页字段。 |
| `get_conversation` | 输入 `conversation_id`；请求 `display_as=plaintext`，返回会话元信息、首条消息正文、选定的会话片段、上游报告的片段数和截断标记。 |
| `reply_conversation` | 输入 `conversation_id`、`admin_id` 和 `body`，可选 `message_type`（默认为 `comment`，也可为 `note`）。返回会话元信息作为写入回执。 |

联系人 ID 是不透明的 ID 字符串，不是邮箱或外部 ID。会话 ID 和回复管理员 ID 为正十进制字符串，可保留超过 JavaScript 精确整数范围的值。请从 Intercom 管理员 API 或自己的配置中获取有权回复的管理员 ID；它不是联系人 ID。健康检查只报告状态，不输出管理员 ID。时间戳单位为 Unix 秒。

## 联系人字段与示例

支持写入的 `fields` 为 `name`、`phone` 和 `unsubscribed_from_emails`。省略字段表示不指定。显式 `false` 会原样发送；空姓名或电话会作为空字符串发送。创建后，本插件不提供修改联系人邮箱、角色或外部 ID 的能力。

创建联系人：

```json
{
  "email": "alex@example.com",
  "role": "user",
  "fields": {"name": "Alex Example", "unsubscribed_from_emails": false}
}
```

仅更新邮件订阅标记，不修改其他字段：

```json
{"contact_id": "6630b37a0000000000000001", "fields": {"unsubscribed_from_emails": false}}
```

该订阅标记控制 Intercom 的邮件订阅状态。修改它不能替代在你自己的工作流中取得适当同意。可空资料字段可能在输出中被省略；null 不会替换成虚构的值。

## 会话回复与历史

`comment` 发送会话参与者可见的回复；`note` 添加内部备注。执行前请检查目标会话和管理员。正文遵循 Intercom 格式规则，插件限制为 64 KiB。不支持附件或代表联系人回复。

```json
{
  "conversation_id": "123456789",
  "admin_id": "12345",
  "message_type": "note",
  "body": "The support team is reviewing this request."
}
```

`get_conversation` 单独以 `source.body` 提供首条消息；首条消息正文或片段正文可能不存在。API 最多返回 500 条会话片段。当返回 500 条片段，或返回数量少于报告的 `parts_total` 时，`parts_may_be_truncated` 为 true。该标记为 false 只描述本次响应，不保证获得完整历史归档。本版本不分页获取更早片段、不下载附件，也不提供导出端点。`list_conversations` 返回元信息，不返回消息历史。

## 游标分页

列表和搜索操作接受 `page.limit`（默认 50，范围 1–150）及可选 `page.cursor`。首次不传游标；在 `has_more` 为 true 时，将 `next_cursor` 原样复制到 `page.cursor`。保持相同操作、区域和搜索邮箱。列表游标作为 `starting_after` 查询参数发送，搜索游标放入 JSON 的 `pagination` 对象。插件不会访问响应中的 URL。

每次调用只获取一页。续页元信息缺失或重复会报错，不会悄悄返回不完整结果。Intercom 分页是无状态的；遍历期间的数据变化可能影响顺序和集合成员。请按资源 ID 去重，不要把游标当作恰好一次的检查点或快照令牌。

## 限制、失败与功能范围

姓名和电话字符串最多 1024 字节，联系人 ID 最多 128 个字符，请求上限为 1 MiB，响应上限为 8 MiB。调用的传输超时为 45 秒，同时受操作截止时间约束。Intercom 的 API 速率限制、账户权限和套餐限制仍然适用。响应超过大小限制时请减小每页数量。

HTTP 错误、Intercom 错误对象、身份字段缺失和无效写入回执都会明确失败。错误信息不包含上游响应正文或凭证。请求拒绝重定向，仅使用选定的官方区域，从不自动重试。创建或回复超时后，请先在 Intercom 中检查结果再重复操作；写入可能已经成功。

本版本不删除联系人，不关闭、重新打开或分配会话，不管理工单，不发送外呼营销消息，不注册 webhook，不开放自定义属性或任意搜索筛选。测试使用依据官方 v2.15 示例适配的合成 fixture，覆盖 null、false、大 ID、游标和截断变体，尚未进行真实账户验收，也未发送真实回复。

## 官方参考

- [鉴权、区域和 REST API](https://developers.intercom.com/docs/build-an-integration/learn-more/rest-apis)
- [分页](https://developers.intercom.com/docs/build-an-integration/learn-more/rest-apis/pagination)
- [当前管理员](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/admins/identifyadmin)
- [联系人列表](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/listcontacts)
- [创建联系人](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/createcontact)
- [更新联系人](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/updatecontact)
- [搜索联系人](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/contacts/searchcontacts)
- [会话列表](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/conversations/listconversations)
- [读取会话](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/conversations/retrieveconversation)
- [回复会话](https://developers.intercom.com/docs/references/2.15/rest-api/api.intercom.io/conversations/replyconversation)
