# Microsoft Outlook

通过 Microsoft Graph v1.0 读取和发送邮件、下载文件附件、查询日历实例并创建日程。

## 凭据与权限

在 Microsoft Entra ID 创建应用，通过你方已批准的授权流程取得 **Microsoft Graph 委托访问令牌**，将原始令牌保存到 `access_token`，不要加 `Bearer ` 前缀。令牌必须面向全球版 Microsoft Graph。本插件不实现 OAuth 授权或刷新，请在到期前更新令牌。不能使用 ID token、app-only 令牌、客户端密钥或刷新令牌。

所有操作首先用 `User.Read` 调用 `GET /me?$select=id`，验证委托身份，将邮箱请求绑定到返回的用户 ID。插件不接受其他邮箱所有者。`health_check` 仅执行该只读身份请求，返回 `ok` 和脱敏 `message`，不会验证邮箱许可或全部服务权限。

| 能力 | 委托权限 |
| --- | --- |
| 所有操作需要的身份检查 | `User.Read` |
| 邮件读取及附件列表/下载 | `Mail.Read` |
| 发送邮件 | `Mail.Send` |
| 日历视图及所选详情 | `Calendars.Read`（或 `Calendars.ReadWrite`） |
| 创建日程 | `Calendars.ReadWrite` |

只申请需要的权限。授权应用、租户同意策略、条件访问及邮箱权限仍然有效，管理员可能需要批准应用。Outlook 数据要求账号拥有可访问的邮箱。在 Graph 支持对应邮箱的前提下，可使用个人 Microsoft 账号或工作/学校账号。本版本不支持主权云或其他国家云端点。

## 邮件操作

- `list_messages` 默认读取整个邮箱的一页邮件。`folder_id` 可填 Graph 文件夹 ID 或 `inbox`、`sentitems` 等已知名称。`filter` 为 Graph 邮件 OData `$filter` 表达式，例如 `isRead eq false`；不支持全文 `$search` 或自定义排序。
- `get_message` 按 ID 读取邮件。输出包括主题、发件人和收件人地址、收取时间、正文及格式、正文预览、已读/附件标记、网页 URL、Internet message ID，不返回 CC/BCC 头或任意扩展属性。
- `send_mail` 使用 `to`、`subject`、`body` 发送新邮件，可选 `cc`、`bcc`、`body_type`（`text` 或 `html`，默认 `text`）。地址必须是纯邮箱，至少一个 To 地址，总收件人最多 100。沿用 Graph 默认行为保存到已发送项目。不支持附加文件、发送草稿、回复或转发。

```json
{"to":["recipient@example.com"],"subject":"Research summary","body":"The summary is ready.","body_type":"text"}
```

`accepted: true` 仅表示 Graph 返回 HTTP 202，处理和投递均异步完成，不是投递回执，也不返回已发邮件 ID。发送是外部操作，超时后不要盲目重试，应先检查已发送项目和目标收件箱。

邮件请求使用 `Prefer: IdType="ImmutableId"`。请使用本插件取得的 ID，避免混用其他客户端的 ID 格式。正文可能包含 HTML，下游渲染时应视为不可信内容。

## 附件

`list_attachments` 返回邮件附件类型、名称、MIME 类型、大小和内联标记。`download_attachment` 接受邮件 ID 和附件 ID，验证文件附件元数据后读取原始 `/$value` 字节，返回 Sokel 文件。二进制不会被当作文本解码，文件名中的路径分隔符会被移除。

仅支持下载 `#microsoft.graph.fileAttachment`。嵌入式 Outlook 项目及引用/链接附件可列出，但不能在此下载。下载上限 16 MiB，拒绝更大文件和需重定向的下载。邮件的 `has_attachments` 标记可能不计算仅内联附件；如需内联内容，应调用附件列表。

## 日历与时间

`calendar_view` 展开 `start`/`end` 时间范围内的重复事件实例、例外及单次事件。`calendar_id` 可选，默认当前用户日历。时间必须采用带明确时区偏移的 RFC3339，结束晚于开始。插件将查询时间转为 UTC，并请求 UTC 响应时间。输出包括 ID、主题、正文预览、带时区的起止时间、位置、URL、取消和全天标记，不返回重复规则或参与者列表。

`create_event` 创建单次定时事件，必填主题和起止时间，可选日历、正文、正文格式、位置、必选参与者邮箱及 `transaction_id`。时间转为 UTC 的 Graph `dateTimeTimeZone` 结构。Transaction ID 是供 Graph 处理重复请求的稳定客户端值，插件仍不会自动重试。不支持创建重复/全天事件、更新/删除事件或忙闲查询。

```json
{"subject":"Review","start":"2026-11-03T10:00:00+08:00","end":"2026-11-03T11:00:00+08:00","attendees":["colleague@example.com"],"transaction_id":"review-20261103"}
```

填写参与者会发送会议邀请；个人约会应省略参与者，最多支持 100 人。

## 分页与限制

邮件和日历列表接受 `limit` 1–50，默认 50。附件采用服务端页大小。将 `next_link` 原样传回同一操作，并保持资源输入不变，直到游标为空。下一页 URL 保留完整不透明查询参数，不再叠加初次的过滤、时间范围或页大小。下一页必须保持全球 Graph 的精确域名及相同规范资源路径，拒绝其他用户、文件夹、API 版本或域名。包括后续页在内，每次操作都会重新检查委托身份。

平台操作超时 60 秒，每次 HTTP 超时 55 秒。JSON 请求上限 4 MiB，响应及下载文件上限 16 MiB。拒绝重定向。错误仅说明状态类别，不回显令牌、URL 或上游正文。不自动重试，没有事件触发器。尚未进行真实账号验收或实际发送邮件，测试使用基于官方 API 结构的合成样例。

参考：[邮件列表](https://learn.microsoft.com/en-us/graph/api/user-list-messages?view=graph-rest-1.0)、[sendMail](https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0)、[附件](https://learn.microsoft.com/en-us/graph/api/attachment-get?view=graph-rest-1.0)、[日历视图](https://learn.microsoft.com/en-us/graph/api/calendar-list-calendarview?view=graph-rest-1.0)、[创建事件](https://learn.microsoft.com/en-us/graph/api/user-post-events?view=graph-rest-1.0)、[权限](https://learn.microsoft.com/en-us/graph/permissions-reference)。
