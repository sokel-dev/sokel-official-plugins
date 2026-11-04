# Brevo

通过 Brevo API v3 管理联系人和联系人列表，并显式发送事务邮件。本插件包含 8 个操作，使用 API 密钥，不提供自动事件源、定时发送器、营销活动发送器或 OAuth provider 声明。

## 凭证与就绪状态

创建 Brevo API v3 密钥并保存到 API 密钥；SMTP 密码不是 API 密钥。请求只发送到 `https://api.brevo.com/v3`，密钥位于 `api-key` 请求头。工作流不能覆盖 API 地址。密钥轮换由插件外部负责。

`health_check` 执行只读 `GET /account`，返回 `ok` 和 `message`。它丢弃账户的 relay 与营销自动化数据，包括其中的密钥。检查成功只证明账户认证通过，不证明发件人已审核、余额充足、收件人可接收或邮件已送达。显式调用 `send_email` 前，先在 Brevo 配置发件域名和发件人。

## 操作

| 操作 | 用途 |
|---|---|
| `health_check` | 验证账户认证，不发送邮件。 |
| `list_contacts` | 列出联系人，可按 RFC 3339 `modified_since` 时间筛选修改后的记录。 |
| `get_contact` | 根据以字符串表示的数字 `contact_id` 读取联系人。 |
| `create_contact` | 创建邮箱联系人，必须明确选择初始邮件许可状态，返回联系人 ID。 |
| `update_contact` | 修改属性、加入已有列表或显式改变邮件屏蔽状态。 |
| `list_lists` | 分页读取联系人列表。 |
| `get_list` | 根据数字 `list_id` 字符串读取列表。 |
| `send_email` | 显式向 1–99 个收件人提交一封纯文本或 HTML 事务邮件。 |

联系人、文件夹和列表 ID 为有符号 64 位范围内的精确正十进制字符串。联系人输出包含邮件及短信屏蔽标志、列表 ID、账户自定义属性、创建及修改时间。通过纯手机号方式创建的联系人可能没有邮箱。列表输出 `unique_subscribers` 对应提供方 `uniqueSubscribers`，不输出正在废弃为固定零的 `totalSubscribers` 或 `totalBlacklisted`。这些统计反映提供方响应，不代表已确认能够投递的收件人数。

## 联系人变更

创建时必须将 `email_status` 设为 `blocked` 或 `allowed`，显式映射到 Brevo 的 `emailBlacklisted`，不根据未勾选的选项推断。创建不会启用冲突时更新或强制合并。若联系人已存在，应先读取，再明确选择更新。

更新接受可选 `options.email_blacklisted`。省略时保留原状态；`false` 显式允许邮件，`true` 屏蔽邮件。`list_ids` 将联系人加入这些已有列表，不替换全部归属，也不从其他列表移除。每次最多接受 150 个列表 ID，拒绝重复 ID。

`attributes` 使用账户中已有的大写属性名及其配置类型对应的值。插件接受字符串、数字、布尔值和字符串数组，最多 200 个属性。输入数字限于 JSON 精确整数范围（绝对值不超过 9007199254740991）；更大的标识应使用文本属性。Brevo 可能忽略不符合属性类型的值，因此更新被接受并不证明每个字段都发生了变化；需要验证时重新读取联系人。插件明确拒绝 `EMAIL`：Brevo 文档指出，修改已屏蔽联系人的邮箱可能取消屏蔽并重新订阅。本版不支持邮箱重命名、强制合并、DOI 创建、删除、发送短信或创建及移除列表。

联系人变更可能激活 Brevo 账户中已有的自动化。插件自身仅在调用 `send_email` 时发送事务邮件，不控制这些外部自动化。

## 显式发送事务邮件

提供纯邮箱 `sender_email`、可选 `sender_name`、由 `email` 及可选 `name` 对象组成的 `to` 数组、`subject`、`content_type`（`text` 或 `html`）及 `body`。一次只发送一种正文格式。主题为 1–998 字节且不能含换行；正文为 1–524288 字节。拒绝重复收件人以及包含显示名称语法的邮箱。所有收件人都放在 To 中，可能看见其他地址；需要私密投递时分别调用。

以下入参示例仅供审阅；执行会发送邮件，并可能产生费用：

```json
{
  "sender_email": "sender@example.com",
  "sender_name": "Example service",
  "to": [{"email": "person@example.com", "name": "Alex"}],
  "subject": "Your requested update",
  "content_type": "text",
  "body": "Your request has been processed."
}
```

只有有效的 HTTP 201 响应及 `messageId` 才会返回 `accepted=true` 和 `message_id`。这表示 API 已受理，不表示送达或已读。投递问题应查看 Brevo 日志。本版不暴露模板、模板参数、附件、CC/BCC、消息版本、定时、定制请求头或幂等键，不提供恰好一次发送保证。

## 分页

使用嵌套的 `page.limit` 和 `page.offset`。联系人每页 1–1000 条，列表每页 1–50 条，均默认 50。偏移默认 0，最多 10000000。省略的数字参数保持未设置，直到插件应用文档中的默认值；显式零偏移被保留，但分页大小为零会被拒绝。

列表按创建顺序升序请求。仅当 `has_more=true` 时继续，用相同筛选条件及 `next_offset` 请求下一页。短页按实际条数推进；总数不一致或在总数尚未读完时返回空页，会明确失败。普通偏移分页不是快照，并发变更可能造成重复或遗漏。定时联系人同步应采用重叠的 `modified_since` 时间窗，并按联系人 ID 去重。插件不提供内置轮询检查点或交付保证。

## 失败行为

HTTP 超时 45 秒，操作超时 60 秒。请求上限 1 MiB，响应上限 4 MiB，读取过大时缩小分页。拒绝重定向，不跟随响应中的分页 URL。只有固定 API 路径与本地编码参数的请求会携带密钥。错误不回显上游错误正文或账户密钥。

插件不执行应用层重试，HTTP 客户端无法重放写请求正文。遇到 429 时，如有经过限制的数字 Retry-After 提示，按提示等待；Brevo 还可能按账户应用额外限制。写请求超时或回执异常可能意味着提供方已经受理。重试前先查看联系人或投递日志，尤其发送邮件时可能造成重复。

验证使用官方公开响应示例和合成 HTTP 边界用例。开发中未读取真实凭据、未调用真实账户、未发送邮件。发件人配置和投递仍需另行真实验收。

## 官方参考

- [账户详情](https://developers.brevo.com/reference/get-account)
- [联系人列表](https://developers.brevo.com/reference/get-contacts)与[联系人详情](https://developers.brevo.com/reference/get-contact-info)
- [创建联系人](https://developers.brevo.com/reference/create-contact)与[更新联系人](https://developers.brevo.com/reference/update-contact)
- [联系人列表集合](https://developers.brevo.com/reference/get-lists)与[单个列表详情](https://developers.brevo.com/reference/get-list)
- [发送事务邮件](https://developers.brevo.com/reference/send-transac-email)与[发送指南](https://developers.brevo.com/docs/send-a-transactional-email)
- [分页概念](https://developers.brevo.com/docs/how-it-works)与[速率限制](https://developers.brevo.com/docs/api-limits)
