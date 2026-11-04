# Vikunja

使用你自己的 Vikunja 服务器发现项目、读取和创建任务、更新指定任务字段并添加评论。不需要云账户或付费功能。插件使用 Vikunja 2.4.0 引入的 API v2，已在独立的 Vikunja 2.7.1 社区版容器中测试，不回退到旧 API v1。

## 部署与认证

将必填部署环境变量 `SOKEL_VIKUNJA_URL` 设置为服务器根地址，例如 `http://vikunja:3456` 或 `https://tasks.example.internal/vikunja`。不要追加 `/api/v2`，插件会自动添加。支持内网 HTTP 和反向代理路径前缀，没有默认服务器或公共服务回退。URL 不能包含内嵌凭证、查询参数、片段或点路径段。跨越不可信网络时请使用 HTTPS。

容器内的 `localhost` 指该容器自身。请使用可达的容器网络服务名或正确的宿主机地址。插件不会安装或启动 Vikunja 及其数据库。

在 Vikunja 的设置 → API 令牌中创建有范围限制的令牌，填入 **API 令牌**。请求使用 `Authorization: Bearer ...`。授予所需的项目、任务和任务评论权限；读取、创建和更新是不同能力。局部更新需要 Vikunja PATCH 实现使用的权限，包括目标任务的读取和更新权限。本地验收使用具有相关 `projects`、`tasks` 和 `tasks_comments` 权限的 `tk_` 令牌。

令牌轮换和过期管理在插件外完成。插件不提供 OAuth 供应商、自动登录、账户创建或令牌刷新。部署可能接受手动传入的 Bearer JWT，但你仍需管理其有效期；推荐使用有范围限制的 API 令牌。

`health_check` 读取一页项目，返回 `ok` 和 `message`。配置、传输或认证失败时返回 `ok=false`。它仅确认项目列表权限，不保证所有写入权限，也不创建数据。仅有任务权限的令牌可能无法通过此检查，但仍能执行特定任务操作。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 验证已配置服务及令牌的项目列表权限。 |
| `list_projects` | 可选 `page`；返回项目元信息、字符串 `total`、`next_page` 和 `has_more`。 |
| `get_project` | 输入 `project_id`；返回标题、HTML 描述和归档标记。 |
| `create_project` | 输入 `title` 和可选 `description`；创建普通项目并返回。Vikunja 会创建默认视图和任务分组。 |
| `list_tasks` | 输入 `project_id` 和可选 `page`；返回一页平铺任务及分页信息。 |
| `get_task` | 输入 `task_id`；返回任务实际所属项目 ID、标题、HTML 描述、完成状态、优先级和截止时间。 |
| `create_task` | 输入正数 `project_id`、`title` 和可选 `fields`；在该普通项目中创建任务。 |
| `update_task` | 输入 `task_id` 和非空 `fields`；应用 JSON Merge Patch 并返回结果任务。 |
| `add_comment` | 输入 `task_id` 和 HTML `comment`；创建评论并返回 ID、正文和创建时间。 |

项目和任务 ID 在 Sokel 中是规范的十进制**字符串**，保留有符号 64 位精度。不要将 ID 转换为浮点数。任务 ID 和用于创建任务的项目 ID 必须为正数。本版本不提供项目或任务删除操作。

## 普通项目与虚拟项目

Vikunja 将保存的筛选器和收藏夹作为负 ID 的虚拟项目提供。`-1` 表示收藏夹，保存的筛选器 N 表示为 `-N-1`。项目列表和 `get_project` 保留这些 ID，`list_tasks` 接受负 ID 进行读取。虚拟项目返回的任务仍报告实际的正数 `project_id`。

不能通过插件在虚拟项目中创建任务，请选择真实的正数项目 ID。修改从虚拟项目获得的任务会修改真实任务，也可能使任务不再匹配该筛选器。

真实 2.7.1 服务器会将虚拟项目追加到普通项目分页结果之后，而 `total` 和页数只计算普通项目。因此，一页项目的条数可能超过请求大小，同一个虚拟项目也可能出现在多个页面。按需保留虚拟条目，按 ID 对所有项目结果去重，并使用 `has_more`/`next_page` 判断是否继续，不要根据返回条数判断。总数为零时仍可能有虚拟条目。

## 任务字段与 HTML

创建时可选 `fields` 包含 `description`、`done`、`priority` 和 `due_date`；更新还支持 `title`。PATCH 中省略的字段保持不变，至少应提供一个字段。显式 `done=false`、`priority=0` 和空描述会被保留。

```json
{
  "task_id": "123",
  "fields": {
    "done": false,
    "priority": 0,
    "description": "<p>Updated details</p>",
    "due_date": "2027-01-01T12:00:00Z"
  }
}
```

插件将标题限制为 250 个 UTF-8 字节且不能为空白；描述和评论限制为 256 KiB。优先级接受常用界面范围 0–5，其中零表示无优先级；上游的整数范围更宽，插件在此有意限制。截止时间必须是带时区的 RFC3339 时间戳。使用 `"0001-01-01T00:00:00Z"` 清空截止时间，不接受空字符串。

描述和评论以 HTML 交换，不是 Markdown。本版本不启用 Markdown 转换模式。请将返回内容视为 HTML，在下游渲染器中适当清理。服务器可能规范化提交的 HTML。评论提及和任务变更可能按服务器设置触发 Vikunja 通知。循环任务标记完成时可能重新打开并调整日期，应使用返回的任务状态，不要假定请求的完成标记一定保持。

创建必须返回规范中的 HTTP 201 资源响应。更新必须返回 HTTP 200 且任务 ID 匹配。缺少元信息、ID 不匹配或响应格式错误会明确失败，空响应不算成功。PATCH 只改变插件公开的字段，不会由插件替换受派人、标签、提醒、循环规则等其他任务属性。插件不提供乐观并发条件，因此并发写入同一字段时仍可能互相覆盖。

## 分页与范围

`page.number` 范围为 1–1000000，默认 1；`page.size` 范围为 1–1000，默认 50。服务器可能限制实际页大小；续页使用响应元信息并接受这种限制。可选 `page.query` 为最长 16 KiB 的搜索字符串，通过 API v2 的 `q` 参数发送。

每次调用只读取一页。当 `has_more=true` 时，将 `next_page` 复制到 `page.number`，其他输入保持不变；结束时返回零。总数是十进制字符串。分页信息不一致、页内资源 ID 重复或无法安全推进的续页会明确失败。并发修改可能改变结果，分页不是快照。

项目列表使用服务器默认行为，排除归档项目。任务列表返回接口的平铺结果，不模拟某个看板或甘特视图，也不应用视图筛选。本版本不管理视图、标签、受派人、附件、循环规则、提醒、订阅、webhook 或账户，也不执行任意 API 请求。

## 限制与错误

请求上限为 1 MiB，响应上限为 8 MiB。HTTP 调用超时为 45 秒，操作总时限为 60 秒。插件仅使用部署 URL，拒绝重定向，不重试失败的 HTTP 响应，并禁用写请求正文重放。错误不包含上游原始响应正文、服务 URL 或凭证。

遇到 401/403 时检查令牌范围和项目、任务访问权限，遇到 422 时检查字段值。超时后先检查目标资源再重复写入，因为原操作可能已生效。这些自托管操作不需要第三方服务，但 Vikunja 中另行配置的通知或集成可能引入外部依赖。

测试包含依据文档改编的合成请求样例，以及独立 2.7.1 实例的真实请求和响应记录。真实验收覆盖全部九个操作、包含虚拟项目的分页，以及 false、零和日期清空。验收使用全新的本地测试账户并删除了临时项目，没有访问生产账户或数据。

## 官方参考

- [Vikunja 2.7.1 发布](https://github.com/go-vikunja/vikunja/releases/tag/v2.7.1)
- [AGPL-3.0 许可](https://github.com/go-vikunja/vikunja/blob/v2.7.1/LICENSE)
- [API v2 与迁移语义](https://vikunja.io/docs/api-v2/)
- [API 文档和令牌](https://vikunja.io/docs/api-documentation/)
- [对应标签中的项目 API 路由](https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/routes/api/v2/projects.go)
- [对应标签中的普通和虚拟项目分页](https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/models/project.go)
- [保存的筛选器](https://vikunja.io/help/saved-filters/)

精确接口定义请查阅自己实例的 `/api/v2/docs` 或 `/api/v2/openapi.json`。公共演示站可能运行开发版本；本插件真实验收使用已发布的 2.7.1 规范。
