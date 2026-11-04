# Sokel for Todoist

通过当前 Todoist `/api/v1` API 管理项目和活动任务。本集成并非由 Todoist 创建、关联或支持。

## 凭据

从 Todoist 集成设置复制个人 API 令牌，填写到 **个人 API 令牌**。插件仅向 `https://api.todoist.com/api/v1` 发送 Bearer 鉴权。插件不使用已退役的 REST v2 或 Sync v9 接口，不实现 OAuth 提供方，也不刷新令牌。令牌撤销后需在外部更新。账号套餐和共享项目权限仍然适用。

查询项目、任务及用户身份需要读取权限；创建、更新和完成任务需要写入权限。如果使用在外部获得的带权限范围 OAuth 令牌，请核对该令牌适用的上游 `data:read`/`user:read`、`data:read_write` 和任务完成权限；令牌获取和权限协商不由本插件负责。`health_check` 只读调用 `GET /user`，返回 `ok` 和 `message`，不会修改账号数据。该检查只验证身份，不验证所有项目权限。

## 操作

| 操作 | 行为 |
| --- | --- |
| `health_check` | 检查当前用户身份。 |
| `list_projects` | 返回一页活动项目。 |
| `get_project` | 根据服务器已确认的 ID 读取项目。 |
| `create_project` | 创建项目，填写名称，可选描述、父项目、收藏状态和视图样式。 |
| `list_tasks` | 返回一页活动任务，可按项目、分区、父任务、标签筛选。 |
| `get_task` | 读取未完成的活动任务。 |
| `create_task` | 根据必填内容和可选详情创建任务。 |
| `update_task` | 仅将已提供字段 POST 到现有任务。 |
| `close_task` | 完成普通任务，或将循环任务推进到下一次。 |

列表接受 1–200 的 `limit`（省略或 0 使用 50）和不透明 `cursor`。下一次调用将 `next_cursor` 作为游标，并保持其他筛选条件不变。`has_more=false` 表示结束。每次执行仅发送一次请求，不会跟随任何返回 URL。请使用真实服务器 ID，不要传入完整 URL 或以 `tmp-` 开头的临时 ID。本版本不支持已完成任务、删除、提醒、附件、评论、工作区管理或 Sync 命令。

## 创建与修改任务

例如创建 `content: Buy milk`，将 `details.project_id` 设为项目 ID，并填写 `details.due_date: 2030-01-02`。省略项目时使用收件箱。创建支持的可选字段为 `description`、`project_id`、`section_id`、`parent_id`、`order`、`labels`、`priority`、`due_date`、`due_datetime`。

更新字段为 `content`、`description`、`labels`、`priority`、`due_date`、`due_datetime`、`child_order`、`is_collapsed`。必须至少提供一个字段。显式 `false`、排序值 `0`、空描述和 `labels: []` 均保留；省略字段保持现值。标签会整体替换，空数组清空所有标签。本版本不提供通过 null 清空受让人、时长、最后期限的功能，也不提供移除截止日期。

截止时间只能选择 `YYYY-MM-DD` 日期或带时区偏移的 RFC 3339 时间戳，不能同时提供。时间戳统一转换为 UTC。本版本不支持自然语言解析日期。数字优先级 1–4 原样发送。当前官方规范存在矛盾：请求描述称 1 为最高，任务响应描述却称 1 为普通、4 为紧急。插件不会反转数值；自动化前请在自己的账号中确认优先级效果。

排序值使用有符号 32 位整数。内容最多 10,000 字符；最多 100 个标签，每个最多 512 字符；项目名称最多 120 字符。这些是插件限制，账号实际限制可能更低。项目视图支持 `list`、`board`、`calendar`、`table`，是否可用取决于账号。显式 `is_favorite: false` 会发送。

`close_task` 调用 `POST /tasks/{task_id}/close`，要求 HTTP 200 和 JSON null，返回 `accepted=true`。对循环任务，该操作推进一次，不会永久关闭整个系列。普通任务及其子任务会进入历史记录。再次完成可能影响下一次循环，因此结果不确定时应先检查状态。

## 限制与验收

操作时限为 60 秒，HTTP 超时 50 秒，请求上限 1 MiB，响应上限 8 MiB。插件阻止重定向、验证游标、隐藏错误中的敏感信息，并且从不自动重试。插件没有为创建操作提供幂等保证。写入成功后仍可能发生超时，重试前应先检查账号。Todoist 配额和套餐限制仍然有效。

源码尚未发布。测试数据根据官方 API 结构和示例合成、适配，不是真实账号响应记录。开发期间未访问真实 Todoist 账号，也未创建或完成任务。生产验收应检查实际令牌权限、分页、优先级映射，以及受控的创建、更新、完成流程。

## 官方参考

- [Todoist API v1：项目、任务、用户、分页和迁移参考](https://developer.todoist.com/api/v1/)

参考文档核对日期为 2026-11-04。SDK v0.7.7 运行时通过 `SetDoc` 上传英文指南，不上传语言字典。通过注册表安装时，会打包 `locales/zh-CN.json` 中完整的中文翻译；仓库同时提供独立中文指南。
