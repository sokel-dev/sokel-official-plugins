# ClickUp

通过固定的 ClickUp v2 API 域名发现工作区、空间、文件夹和列表，读取、新建、修改任务，以及添加任务评论。

## 凭据

在 ClickUp 设置中生成个人 API Token，填入 `access_token`。插件按照个人令牌要求，将原始 token 放入 `Authorization`，不实现 OAuth 授权或刷新流程。令牌继承用户访问权限，仍受工作区权限约束。`health_check` 请求 `/user`，验证当前身份，不保证每个列表都有权限。

## 操作和资源发现

按 `list_workspaces` → `list_spaces` → `list_folders` → `list_lists` 查找任务列表。`list_lists` 的 `parent_type=space` 只列出不属于文件夹的列表；文件夹内的列表需使用 `parent_type=folder`。文件夹发现返回 ID 和名称的扁平集合，不返回嵌套树。资源发现采用接口默认的活动资源范围。

`list_tasks` 接收 `list_id` 和可选 `options`：`page` 从零开始，`archived`、`include_closed`、`include_timl`、`subtasks` 均保留显式 false。每页最多 100 条。只有 `has_more=true` 时才继续使用 `next_page`，并保留全部过滤条件。上游未返回 `last_page` 时，满 100 条表示可能还有下一页，因此可能需要一次额外的空页请求。分页不是快照。默认只返回主列表为指定列表的任务；`include_timl=true` 可包含从其他主列表添加的任务。

`get_task`、`create_task`、`update_task` 和 `add_comment` 使用原生任务 ID，不支持自定义 ID。新建任务必填列表和顶层名称，不要重复填写 `details.name`。`details.archived` 仅适用于更新，新建时会被拒绝。

更新必须提供至少一个字段，省略字段保持不变。支持名称、描述、状态、优先级、`due_date_ms`、`due_date_time` 和归档状态。优先级只接受 1–4，截止时间必须为正 Unix 毫秒时间戳。上游没有明确说明用零清除优先级或日期，因此插件会拒绝。若需保留具体时间，请随截止时间一起填写 `due_date_time=true`；false 或省略表示仅日期，ClickUp 会按用户时区规范为当天凌晨 4 点。空描述会按 ClickUp 要求转换为单个空格。显式 `archived=false` 会发送。本版不包含自定义字段和负责人修改。输出中的可选截止时间及优先级可能为空或 null。

```json
{"task_id":"abc123","changes":{"description":"","priority":3,"archived":false}}
```

`add_comment` 必填任务 ID 和文本。`notify_all` 默认 false，true 会额外通知评论作者。false 时负责人和关注者仍可能收到通知。评论 ID 返回字符串，避免大整数失真。

## 可靠性

不自动重试或跟随重定向，JSON 上限 8 MiB，错误不直接回显上游正文或凭据。写入超时或回执异常后，应先查看任务再重试；新建任务和评论不是幂等操作。限流应由工作流延迟后续调用。本插件没有触发器或 webhook。

测试采用构造的公开协议数据，并覆盖生成代码的输入输出绑定，不是真实账户流量。真实账户验收尚未进行。

参考：[鉴权](https://developer.clickup.com/docs/authentication)、[任务列表](https://developer.clickup.com/reference/gettasks)、[更新任务](https://developer.clickup.com/reference/updatetask)、[添加评论](https://developer.clickup.com/reference/createtaskcomment)、[官方 OpenAPI](https://developer.clickup.com/openapi/clickup-api-v2-reference.json)。
