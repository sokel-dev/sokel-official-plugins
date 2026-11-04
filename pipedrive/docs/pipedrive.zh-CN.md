# Pipedrive

读取和创建 CRM 联系人与组织，并读取、创建及更新交易。业务操作使用 Pipedrive API v2；当前用户健康检查使用仍受支持的 v1 `users/me` 端点。插件通过官方 `x-api-token` 请求头使用个人 API 令牌鉴权。

## 凭证

在 Pipedrive 账户的 API 设置中获取个人 API 令牌，并填入 Sokel 的 **API 令牌**。**公司子域名**仅填写子域名部分：例如 `https://acme.pipedrive.com` 应填 `acme`。完整 URL、端口、包含点的主机名或自定义域名均会被拒绝。插件自行构造 HTTPS 地址，从不把令牌放进查询字符串。

令牌使用所属用户的账户权限和可见性规则。请确认套餐与账户允许使用 API。续期和轮换在插件外进行；轮换个人令牌也会影响使用旧令牌的其他集成。本版本不提供 OAuth 授权或刷新流程。

运行 `health_check`。它读取 `/api/v1/users/me` 并返回 `ok` 和 `message`。凭证不可用时返回 `ok=false`，不抛出处理器错误，也不执行写入。身份访问成功不代表可以修改每条 CRM 记录。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 读取当前用户以检查凭证。 |
| `list_persons` | 可选 `page`；返回联系人和分页字段。 |
| `get_person` | 输入 `person_id`；返回 ID、姓名、可选组织 ID、电子邮箱地址和电话号码。 |
| `create_person` | 输入 `name`；可选 `fields.org_id`、`fields.emails` 和 `fields.phones`。返回创建的联系人。 |
| `list_organizations` | 可选 `page`；返回组织 ID、名称和分页字段。 |
| `create_organization` | 输入 `name`；返回创建的组织。 |
| `list_deals` | 可选 `page`；返回交易 ID/标题/状态/金额/币种、可选联系人/组织 ID，以及分页字段。使用上游默认的可见性和归档筛选规则。 |
| `create_deal` | 输入 `title`；可选 `fields.value`、`currency`、`person_id`、`org_id` 和 `status`。返回交易。 |
| `update_deal` | 输入 `deal_id`；可选 `title` 和与创建相同的 `fields`。至少提供一项修改。返回更新后的交易。 |

Sokel 接口中的所有资源 ID 都使用正十进制字符串。解析 Pipedrive JSON 数字时不会转成浮点数；关联 ID 以精确 JSON 整数发送。插件保留可为空的关联 ID，不会将其变为零。

## 创建与更新示例

创建关联组织的联系人：

```json
{
  "name": "Alex Example",
  "fields": {
    "org_id": "123",
    "emails": [{"value": "alex@example.com", "primary": false, "label": "work"}]
  }
}
```

邮箱和电话条目包含 `value`、可选 `primary` 和可选 `label`。显式传入的 `primary=false` 会被保留。省略数组表示未指定，显式空数组会按空数组发送。每个数组最多 100 个条目，且最多一个主条目。邮箱必须是纯地址，不能包含显示名称。

将交易金额更新为零，不修改其他字段：

```json
{"deal_id": "456", "fields": {"value": "0"}}
```

接口中的金额使用十进制**字符串**，实际请求使用精确 JSON 数字。输入金额必须为非负普通十进制形式，最多 32 个字符；写入不接受科学计数法。返回金额保留上游数字的精确文本，包括上游使用的指数形式。币种代码必须是三个大写字母，例如 `USD`；账户接受哪些币种由 Pipedrive 决定。状态必须为 `open`、`won` 或 `lost`。更新时省略的字段保持不变。本版本不提供用 null 清空关联 ID 或用空标题清空交易标题的能力。

## 游标分页

三个列表操作均接受 `page.limit`（默认 100，范围 1–500）和可选 `page.cursor`。首次请求省略游标。在 `has_more` 为 true 时，将 `next_cursor` 原样复制到下一次调用的 `page.cursor`，并保持相同操作和账户。上游的 `additional_data.next_cursor=null` 表示完成；此时插件返回空 `next_cursor` 和 `has_more=false`。

每次操作只获取一页，从不访问上游返回的 URL。游标缺失、重复或格式错误会报错，不会悄悄提前结束。分页不是快照或恰好一次的事件流；扫描期间记录变化时请按记录 ID 去重。列表提供所选的当前记录，不是已删除记录归档或完整自定义字段导出。

## 限制、失败与功能范围

姓名、组织名称和交易标题限制为 1024 字节。请求上限为 1 MiB，响应上限为 8 MiB。调用的传输超时为 45 秒，同时受操作截止时间约束。较大的联系人集合请使用较小分页。Pipedrive 的令牌预算、速率限制、记录可见性和套餐限制仍然适用。

HTTP 错误、`success=false`、数据缺失和不完整回执都会报错。错误不会回显上游响应正文、含密钥的 URL 或凭证。插件仅接受固定的 Pipedrive 公司地址，拒绝重定向，从不自动重试请求。写入结果不确定时，请先检查 CRM 记录再重复操作，避免重复创建联系人、组织或交易。

本版本不更新联系人或组织，不删除记录，不管理活动或销售管道，不上传文件，不开放任意筛选或自定义字段，也不注册 webhook。测试使用根据官方 v2 OpenAPI 及鉴权/分页示例适配的合成 fixture；大 ID、精确金额、可选 false 和错误是特意加入的测试变体，尚未进行真实账户验收。

## 官方参考

- [API 令牌鉴权](https://pipedrive.readme.io/docs/core-api-concepts-authentication)
- [公司域名](https://pipedrive.readme.io/docs/how-to-get-the-company-domain)
- [联系人](https://developers.pipedrive.com/docs/api/v1/Persons)
- [组织](https://developers.pipedrive.com/docs/api/v1/Organizations)
- [交易](https://developers.pipedrive.com/docs/api/v1/Deals)
- [游标分页](https://pipedrive.readme.io/docs/core-api-concepts-pagination)
- [官方 v2 OpenAPI](https://developers.pipedrive.com/docs/api/v1/openapi-v2.yaml)

文档站点的 `/docs/api/v1/` URL 同时承载 v2 端点参考；实际业务请求使用 `/api/v2/`。
