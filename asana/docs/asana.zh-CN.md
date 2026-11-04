# Asana

通过 `https://app.asana.com/api/1.0` 浏览工作区和项目、读取项目任务、新建或修改任务，以及添加评论。

## 凭据和权限

在 Asana 开发者控制台创建个人访问令牌，填入 `access_token`，插件以 Bearer 方式发送。插件不实现 OAuth 授权或刷新流程，过期或撤销的令牌需要在外部更换。资源访问受令牌所属用户权限限制。`health_check` 读取 `/users/me`，验证身份成功不表示有权访问私有项目。

对应 API 权限包括健康检查的 `users:read`，以及 `workspaces:read`、`projects:read`、`tasks:read`、`tasks:write`、评论的 `stories:write`。个人令牌仍需有权访问目标工作区和项目。

## 资源发现和分页

先用 `list_workspaces`，再以 `workspace_gid` 调用 `list_projects`，最后以 `project_gid` 调用 `list_tasks`。这些操作每次返回一页。`page.limit` 默认 50，范围 1–100；`page.offset` 是不透明分页令牌。`has_more=true` 时，将 `next_offset` 传给相同操作，并保持资源和过滤条件不变。令牌可能过期，此时需重新遍历。插件只使用 offset，不访问上游返回的续页 URL。并发修改时分页不是一致性快照。

资源 GID 均为数字字符串。`get_task` 读取单个任务，输出包含备注、完成状态、截止日期、负责人、创建和更新时间、URL。缺失的可选日期返回空字符串；无负责人时返回 null。

## 写入

`create_task` 必填 `workspace_gid` 和顶层 `name`，可选 `project_gids` 将任务加入同工作区的项目。可选详情支持备注、完成状态、`assignee_gid` 和 `due_on`。不要在详情中重复填写名称。`assignee_gid` 可以是数字 GID 或 `me`。

`update_task` 必填 `task_gid`，且 `changes` 至少包含一个字段。省略字段保持不变。显式 `completed=false` 会重新打开任务。空 `assignee_gid` 或 `due_on` 会发送 JSON null 以清除字段，空备注会清除备注。日期必须为 `YYYY-MM-DD`，不允许空任务名称。

```json
{"task_gid":"123456789","changes":{"completed":false,"assignee_gid":"","due_on":"","notes":""}}
```

`add_comment` 创建任务的顶层文本评论，返回 story GID、文本、创建时间和子类型。本版本不包含评论线程回复、附件、自定义字段、删除和自动触发器。

## 限制和验证

JSON 请求和响应上限 8 MiB，使用固定域名，不跟随重定向，不自动重试写操作。HTTP 和限流错误不回显敏感的上游正文。写入结果不确定时，应先检查任务和评论，再决定是否重试，避免重复。成功响应结构或分页令牌异常会明确报错。

测试覆盖构造的请求响应数据、字段清除、分页和平台生成绑定，并非真实账户验收。

参考：[个人访问令牌](https://developers.asana.com/docs/personal-access-token)、[任务列表](https://developers.asana.com/reference/gettasks)、[任务更新](https://developers.asana.com/reference/updatetask)、[评论](https://developers.asana.com/reference/createstoryfortask)。
