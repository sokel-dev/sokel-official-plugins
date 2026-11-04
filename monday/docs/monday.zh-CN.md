# Monday

使用本插件发现看板及其分组和列、读取项目、创建项目、修改列值并添加动态。插件调用 Monday 官方 GraphQL 端点，并明确发送 `API-Version: 2026-10` 请求头。本版本使用该 API 版本支持的嵌套 `boards { items_page }`，不依赖 2027-01 才引入的根级 `items_page`。

## 凭证与访问权限

在 Monday 开发者设置中创建个人 API 令牌，并填入 Sokel 凭证表单的 **API 令牌**。令牌继承你的账户访问权限：向其他用户共享看板不会为此令牌授予访问权限。请在插件外续期或更换令牌。本插件不提供 OAuth 授权流程或自动刷新。

`health_check` 执行只读 `me` 查询，返回 `ok` 和 `message`。凭证检查失败时返回 `ok=false`，不会创建或修改任何内容。检查成功只能确认用户访问有效，不代表对每个看板都有写入权限。如果提供应用令牌，其权限范围和看板权限必须允许目标操作；相关权限包括 `me:read`、`boards:read`、`boards:write` 和 `updates:write`。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 检查当前令牌，不执行写入。 |
| `list_boards` | 可选 `page.number`（默认 1，最大 10000）和 `page.limit`（默认 100，范围 1–100）。返回看板 ID、名称、状态、URL 以及 `next_page`。上游默认列表针对经典看板，不是所有看板层级类型的完整清单。 |
| `get_board` | 输入 `board_id`；返回看板、分组 ID/标题以及列 ID/标题/类型。后续写入请使用这些 ID。 |
| `list_items` | 输入 `board_id`，可选 `page.limit`（默认 100，范围 1–500）和 `page.cursor`。返回项目、`next_cursor` 与 `has_more`。 |
| `get_item` | 输入 `item_id`；返回项目、看板/分组引用和列值。 |
| `create_item` | 输入 `board_id` 和 `name`；可选 `group_id`、`column_values_json` 及 `options.create_labels`。返回创建的项目。 |
| `change_columns` | 输入 `board_id`、`item_id` 和非空的 `column_values_json` 对象字符串；可选 `options.create_labels`。返回更新后的项目。 |
| `add_update` | 输入 `item_id` 和 `body`；返回动态 ID 与正文。正文遵循 Monday 的动态格式规则。 |

所有数字资源 ID 都使用十进制**字符串**，包括超过 JavaScript 精确整数范围的 ID。不要用看板 URL 代替 ID。分组 ID、列 ID 与界面上显示的标题不同。

## 列值与示例

`column_values_json` 是 JSON **字符串**，其内容为以列 ID 为键的对象。插件按照 Monday 要求，将该字符串作为 GraphQL `JSON` 变量传递，不把值拼入查询文本，也不猜测列格式。请使用各列类型官方文档规定的格式。`get_board` 提供 ID 和类型，但不包含标签定义或列设置。

创建项目的输入示例：

```json
{
  "board_id": "1234567890",
  "name": "Prepare launch",
  "group_id": "topics",
  "column_values_json": "{\"numbers\":0,\"status\":{\"label\":\"Working on it\"}}",
  "options": {"create_labels": false}
}
```

示例中的列 ID 和状态标签必须已存在于你的看板。省略 `options.create_labels` 时使用上游默认值；显式传入的 `false` 会被保留。启用该选项可能创建标签，且需要相应权限。修改列值时，使用相同的字符串格式，并提供 `board_id` 和 `item_id`。返回的列值 `value` 保留为 JSON 编码字符串；Monday 返回 null 时，`text` 可能不存在。

计算列或汇总列可能拒绝修改或保持原值，包括多层级看板中的计算型父级列。收到变更回执不代表每个目标列都已变为预期值。对结果不确定的写入，请先检查返回值和看板，再决定是否重试。

## 分页与限制

每次操作只获取一页。看板列表使用 `next_page` 继续，直到其为零。满页只说明可能还有下一页，最后一次请求可能返回空页。项目列表在 `has_more` 为 true 时，将 `next_cursor` 原样复制到 `page.cursor`，并保持同一看板。首次请求应省略游标。游标是不透明值，会在上游过期；Monday 拒绝游标时请重新开始列表查询。分页不是快照，因此看板在扫描期间变化时，工作流应按项目 ID 去重。

插件允许项目名称最多 1024 字节、动态正文最多 64 KiB、列 JSON 最多 256 KiB。HTTP 请求上限为 1 MiB，响应上限为 8 MiB；看板包含较大的列值时请减小每页数量。调用的传输超时为 45 秒，同时受操作截止时间约束。Monday 的复杂度、速率和账户限制仍然适用。

## 错误与功能范围

HTTP 失败、GraphQL `errors`（包括同时返回部分数据的响应）、资源缺失和不完整写入回执都会使操作失败。错误不会包含上游响应正文或凭证。请求使用固定官方地址，拒绝重定向，且从不自动重试。写入超时或回执校验失败后，请先在 Monday 中检查结果，再重复创建或更新，避免重复数据。

本版本不创建看板或分组，不上传文件，不删除或归档项目，不开放任意 GraphQL，不订阅 webhook，也不是完整的列编辑器。输出仅包含声明的类型化字段。测试使用依据官方文档适配的合成 fixture，覆盖大 ID、null 字段、false 值和错误变体，尚未进行真实账户验收。

## 官方参考

- [API 版本](https://developer.monday.com/api-reference/docs/api-versioning)
- [看板与分组/列](https://developer.monday.com/api-reference/reference/boards)
- [项目](https://developer.monday.com/api-reference/reference/items)
- [项目分页](https://developer.monday.com/api-reference/reference/items-page)
- [修改列值](https://developer.monday.com/api-reference/docs/change-column-values)
- [列类型](https://developer.monday.com/api-reference/reference/columns)
- [动态](https://developer.monday.com/api-reference/reference/updates)
- [当前用户](https://developer.monday.com/api-reference/reference/me)
