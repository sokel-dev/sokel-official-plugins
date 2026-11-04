# Baserow

将 Sokel 连接到你自己的 Baserow 开源版，发现可访问的表和字段，读取行，并创建、更新或删除单行。不需要 Baserow Cloud 账户、付费功能或外部 AI 服务。本版本依据 Baserow 2.4.1 官方 API 规范和对应标签源码实现。协议测试使用据此改编的合成样例，尚未在运行中的 Baserow 实例上验收。

## 部署与认证

设置必填部署环境变量 `SOKEL_BASEROW_URL`，例如 `http://baserow:80` 或 `https://data.example.internal/baserow`。请填写 `/api` 之前的服务器根地址，插件会追加 API 路径。支持内网 HTTP 和反向代理路径前缀，没有默认地址或公共服务回退。包含内嵌凭证、查询参数、片段或点路径段的地址会被拒绝。

容器内的 `localhost` 指插件容器自身。请使用容器网络上可达的服务名或正确配置的宿主机地址。插件不会安装 Baserow、PostgreSQL 或其他服务器依赖。跨越不可信网络时请使用 HTTPS。

在凭证表单中输入**数据库令牌**。请求使用 `Authorization: Token ...`。插件不接受登录 JWT，不执行用户登录，也不刷新账户会话。请在 Baserow 中创建和轮换数据库令牌，仅授予目标表所需权限。不同表的读取、创建、更新和删除权限可能不同。

`health_check` 调用数据库令牌专用的 `/api/database/tables/all-tables/` 接口，返回 `ok` 和 `message`；配置、传输或认证失败时返回 `ok=false`。可访问表列表为空仍可能表示令牌有效。该接口会列出令牌拥有任一种受支持权限的表，因此健康检查不能保证对每张表都具有读取行或写入权限。检查没有写入副作用。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 验证配置和数据库令牌认证。 |
| `list_tables` | 返回可访问表的 ID、名称和数据库 ID。接口一次返回不分页列表，受响应大小限制。 |
| `list_fields` | 输入 `table_id`；返回字段 ID、名称、类型、主字段标记和完整 `definition_json`。 |
| `list_rows` | 输入 `table_id` 和可选 `options`；返回行记录、十进制字符串 `total`、`next_page` 和 `has_more`。 |
| `get_row` | 输入 `table_id`、`row_id` 和可选 `user_field_names`；返回单行。 |
| `create_row` | 输入 `table_id`、`data_json` 和可选 `user_field_names`；创建行并返回实际行响应。 |
| `update_row` | 输入 `table_id`、`row_id`、`data_json` 和可选 `user_field_names`；只修改传入字段并返回结果行。 |
| `delete_row` | 输入 `table_id` 和 `row_id`；仅收到规范中的 HTTP 204 后返回 `deleted=true`。不提供永久清空回收站的功能。 |

表、字段、数据库和行 ID 在 Sokel 中均为十进制**字符串**。输入的表和行 ID 必须是规范的正有符号 64 位整数，不接受前导零、小数或指数形式。`"9007199254740993"` 这样的字符串可以保持精确值。读取和更新响应必须匹配请求的行 ID。

## 动态字段值

行包含由应用定义的字段。每个返回行都有 `id` 和 `json`，其中 `json` 是完整 JSON 对象字符串。字段定义也包含完整 JSON 字符串，可保留不同字段类型的专有选项。这样不会将任意字段强行展开，也不会把大整数转换成浮点数。false、零、null、嵌套值和 Baserow 的十进制字符串行顺序均会保留。

默认使用 `field_123` 等稳定的内部字段标识作为数据键。若要使用易读名称，请在读写中一致设置 `user_field_names=true`；此时重命名列会改变键名。`list_fields` 始终报告真实 ID 和名称。构造选择、关联、文件等特殊字段值前，请查看字段定义及实例自动生成的 API 文档。

使用稳定字段 ID 创建行的示例：

```json
{
  "table_id": "8",
  "data_json": "{\"field_123\":\"Example\",\"field_124\":false,\"field_125\":0}",
  "user_field_names": false
}
```

`data_json` 必须是 JSON 对象字符串，不能是数组或 null。创建允许 `{}`，让 Baserow 应用字段默认值。更新至少需要一个字段，并使用 PATCH：省略字段保持不变，显式 null 会交给 Baserow 按字段规则验证。不要传入由服务器管理的顶层保留字段 `id` 或 `order`。插件验证 JSON 结构，Baserow 验证字段名、类型和权限。创建和更新必须返回规范中的 HTTP 200 行回执，空响应不算成功。

## 搜索与分页

`list_rows.options` 支持：

- `page`：1–1000000，默认 1。
- `size`：1–200，默认 50；服务器可能有更严格限制。
- `search`：传给 Baserow 行搜索的文本。
- `order_by`：Baserow 的逗号分隔排序字段，例如 `-field_123`；使用字段名时可用 `-Name`。
- `filters_json`：包含 Baserow 筛选树的 JSON 对象字符串。
- `user_field_names`：行键和筛选、排序中的字段引用是否使用名称。

例如可使用筛选树 `{"filter_type":"AND","filters":[{"field":124,"type":"boolean","value":"false"}]}`。字段特定的筛选语义由服务器验证。搜索、排序和筛选字符串分别限制为 16 KiB。

每次调用只读取一页。当 `has_more=true` 时，将 `next_page` 传入 `options.page`，保持表、页大小、筛选和命名模式不变。结束时 `next_page=0`。总数以字符串返回以保持精度。插件检查总数和续页的一致性，拒绝重复、缺失或不匹配的续页信息。插件不会跟随响应中的 URL，而是用已配置服务和验证过的数字页码构建下一次请求。返回的续页 URL 必须匹配已配置源站、路径和查询；若检查失败，请统一配置反向代理和 Baserow 公共地址。

分页不是快照。并发修改可能改变页内容或总数；跨运行汇总时请按行 ID 去重。

## 限制与错误

请求上限为 1 MiB，响应上限为 8 MiB。行包含大型字段时请减小页大小。HTTP 调用超时为 45 秒，操作总时限为 60 秒。插件拒绝重定向，不重试失败的 HTTP 响应，并禁用写请求正文重放。错误不会回显服务 URL、凭证或上游原始错误正文。

遇到 401/403 时检查表权限，遇到验证错误时检查字段定义，超时后先检查实际行再重试写入，因为请求可能已经生效。插件不创建或删除表，不修改结构，不上传文件，不配置 webhook，不提供任意 API 路由，也不使用高级版或企业版接口。引用的外部文件 URL 及 Baserow 中另行配置的集成仍可能存在网络依赖。

## 官方参考

- [Baserow 2.4.1](https://github.com/baserow/baserow/releases/tag/2.4.1)
- [开源版及其他部分的许可范围](https://github.com/baserow/baserow/blob/2.4.1/LICENSE)
- [数据库 API](https://baserow.io/user-docs/database-api)
- [数据库令牌](https://baserow.io/user-docs/personal-api-tokens)
- [官方 API 规范](https://api.baserow.io/api/schema.json)
- [对应标签中的令牌表发现实现](https://github.com/baserow/baserow/blob/2.4.1/backend/src/baserow/contrib/database/api/tables/views.py)

社区版使用 MIT 许可。高级版、企业版、文档和第三方组件分别适用其声明的许可，不应将整个仓库描述成统一只使用 MIT。
