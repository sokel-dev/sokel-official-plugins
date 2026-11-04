# Mailchimp

通过 Marketing API 读取受众、成员、成员标签和营销活动，并显式新增或更新成员。本插件包含 8 个操作，不创建、定时发送或发送营销活动，也不提供自动事件源。

## 凭证

在 Mailchimp 账户中创建 Marketing API key，将包含数据中心后缀的完整值保存到 API 密钥。例如密钥以 `-us6` 结尾时，插件只向 `https://us6.api.mailchimp.com/3.0` 发送请求。支持的后缀为 `us` 加最多四位的正整数。地址由密钥推导，不能由工作流指定。不支持 OAuth token 或 Mailchimp Transactional/Mandrill 密钥。

API 密钥继承创建者的账户访问权限与角色。检查凭证会执行只读认证请求 `GET /ping`；成功不代表有权更新指定受众。密钥轮换或替换由插件外部负责。

## 操作

| 操作 | 用途 |
|---|---|
| `health_check` | 认证 ping，返回 `ok` 和 `message`。 |
| `list_audiences` | 列出受众 ID、名称、创建时间及双重订阅确认设置。 |
| `get_audience` | 根据 `list_id` 读取受众。 |
| `list_members` | 读取受众成员，可按状态、`since_last_changed` 和 `options.vip_only` 筛选。 |
| `get_member` | 根据受众 ID 和纯邮箱地址读取成员。 |
| `upsert_member` | 显式创建或更新该邮箱对应的成员。 |
| `list_member_tags` | 分页读取成员的全部标签，数字标签 ID 以精确十进制字符串返回。 |
| `list_campaigns` | 读取营销活动标识、状态、标题、主题、受众和时间，可按受众或活动状态筛选。 |

成员邮箱必须是纯邮箱地址，不包含显示名称。成员 URL 使用小写邮箱的 MD5 哈希，不将原始邮箱放入 URL 路径或查询参数。upsert 请求正文按 Mailchimp 要求包含邮箱。

## 显式选择订阅状态

每次调用 `upsert_member` 都必须提供 `status_if_new`。请明确选择 `subscribed`、`unsubscribed`、`cleaned`、`pending` 或 `transactional`。`subscribed` 表示你已取得相应同意；`pending` 请求双重订阅确认，可能使 Mailchimp 发送确认邮件。插件不会根据列表归属、标签或现有 CRM 记录推断同意。

对于已有成员，插件绝不发送 `status` 字段，因此不会在更新资料时顺带重新订阅已退订成员。`status_if_new` 仅在 Mailchimp 创建成员时生效。账户中已有的自动化可能响应联系人变更；本插件不关闭或控制这些自动化。

可选 `merge_fields` 将受众合并标签映射到值。使用该受众已配置的标签；Mailchimp 仍会执行必填合并字段校验。省略的标签保持不变。输入合并字段的数字必须在 JSON 精确整数范围内（绝对值不超过 9007199254740991）；更大的标识应使用文本合并字段。嵌套最多 16 层。`options.vip` 可选：省略时保留原状态，传 `false` 时显式取消 VIP。本版不支持修改邮箱、归档或删除成员、写标签或修改已有订阅状态。

以下入参示例仅供审阅，真正调用会写入联系人：

```json
{
  "list_id": "your_audience_id",
  "email_address": "person@example.com",
  "status_if_new": "transactional",
  "merge_fields": {"FNAME": "Alex"},
  "options": {"vip": false}
}
```

## 分页与同步

列表接受 `page.limit` 1–1000，默认 50；`page.offset` 0–10000000，默认 0。显式零偏移会被保留，但分页大小不能为零。检查 `has_more`，并保持相同筛选条件、用 `next_offset` 请求下一页。下一偏移按实际返回数量推进。缺少总数、数量矛盾或在总数尚未读完时返回空页，会明确报错。

受众按创建时间升序，营销活动按创建时间升序，成员按最后修改时间升序，标签沿用提供方排序。这是普通偏移分页，不是一致性快照；分页期间的增删改和同值排序可能造成重复或遗漏。定时同步应使用有重叠的 RFC 3339 `since_last_changed` 时间窗，并按受众与成员 ID 去重；处理成功后再保存检查点。插件不实现持久轮询、交付保证或后台同步。

`options.vip_only=false` 会显式发送并返回所有成员；省略时使用提供方默认值。合并字段输出采用账户自定义 JSON，解码时保留 JSON 整数精度。空集合返回数组。读取营销活动不代表邮件已送达收件人。

## 错误与限制

HTTP 超时 45 秒，操作超时 60 秒；请求上限 1 MiB，响应上限 4 MiB。响应过大时缩小分页。拒绝重定向，不跟随响应 `_links`。错误消息不包含 API 密钥、任意上游错误正文或响应链接 URL。

插件不执行应用层自动重试，写请求没有可重放的请求正文。遇到 429 时，如有经过限制的数字 Retry-After 提示，按提示等待。写请求超时或回执异常后先读取成员，因为上游写入可能已成功；不能把失败响应当成未发生变更的证明。

本版使用官方文档示例和另行标注的合成 HTTP fixture 验证。开发过程中未访问真实账户、未变更联系人、未发送邮件。真实验收需针对目标账户和受众另行执行。

## 官方参考

- [认证与数据中心](https://mailchimp.com/developer/marketing/docs/fundamentals/)
- [认证 ping 示例](https://mailchimp.com/developer/marketing/build/start-developing/send-your-first-api-request)
- [列出受众](https://mailchimp.com/developer/marketing/api/lists/list)与[读取受众](https://mailchimp.com/developer/marketing/api/lists/get)
- [列出成员](https://mailchimp.com/developer/marketing/api/lists/members/list-members)与[新增或更新成员](https://mailchimp.com/developer/marketing/api/lists/members/upsert-member)
- [导入导出联系人及成员哈希](https://mailchimp.com/developer/marketing/build/quickstarts/import-and-export-contacts)
- [成员标签列表](https://mailchimp.com/developer/marketing/api/lists/list-member-tags)与[标签示例](https://mailchimp.com/developer/marketing/build/quickstarts/organize-contacts-with-tags)
- [营销活动列表](https://mailchimp.com/developer/marketing/api/campaigns/list)
