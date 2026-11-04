# Figma

读取文件元数据和节点摘要、获取渲染图片链接、浏览目录，并读取或发布评论。

## 凭据与权限

在本插件之外创建 Figma 个人访问令牌，填写到 **个人访问令牌**。请求仅向 `https://api.figma.com` 发送，使用 `X-Figma-Token` 请求头。插件不会交换或刷新令牌，到期后需在外部更新。本版本不支持 OAuth 令牌或套餐访问令牌。

只读 `health_check` 需要 `current_user:read`；文件元数据需要 `file_metadata:read`；节点和图片渲染需要 `file_content:read`；浏览目录需要 `folders:read`；读取评论需要 `file_comments:read`；发布评论需要 `file_comments:write`。令牌所属用户也必须有资源访问权限。Figma 套餐、席位、审批和限流规则仍然适用。需要 `folders:read` 的目录 API 不对公共 OAuth 应用开放；本插件使用个人令牌。健康检查成功只确认 `/v1/me` 可用，不代表所有文件和团队都可访问。

## 操作

| 操作 | 行为 |
| --- | --- |
| `health_check` | 读取当前用户，返回 `ok` 和 `message`。 |
| `get_file_metadata` | 读取 `/v1/files/{file_key}/meta`，包含名称、编辑器类型、创建者以及可用的版本与链接。 |
| `get_nodes` | 使用固定 `depth=1`，以结构化摘要返回指定节点及其直接子节点。 |
| `render_images` | 返回指定节点的渲染图片链接元数据，不下载图片。 |
| `list_team_folders` | 使用 `/v2/teams/{team_id}/folders` 列出可见的顶层目录。 |
| `list_subfolders` | 使用 `/v2/folders/{folder_id}/folders` 列出直接子目录。 |
| `list_folder_files` | 使用 `/v2/folders/{folder_id}/files` 列出目录直接包含的文件。 |
| `list_comments` | 返回文件上的评论。 |
| `create_comment` | 发布纯文本评论，或回复顶层评论。 |

插件不使用已弃用的项目接口。团队 ID 可从 Figma 团队页面 URL 获取。目录和评论列表接口当前没有游标参数；插件通过一次请求返回 API 给出的完整直接列表，但受响应大小上限约束。嵌套目录需显式调用 `list_subfolders` 遍历，插件不会自动递归。

## 输入与结果

从 Figma 文件 URL 获取 `file_key`，仅传入文件键，不要传完整 URL。节点 ID 来自节点链接；URL 中的 `1-2` 常对应 API ID `1:2`。每次提供 1–100 个不重复的 API 节点 ID。`get_nodes` 可选传入已保存的 `version`。输出保留请求顺序；官方定义的 null 节点返回 `found=false`。找到的节点提供 ID、名称、类型、可选可见性或文本以及直接子节点摘要。本版本不输出完整递归设计文档、向量几何、组件或样式映射及插件数据。

`render_images` 支持 `png`（默认）、`jpg`、`svg`、`pdf`；`options.scale` 范围为 0.01–4。可选 `contents_only` 和 `use_absolute_bounds` 保留显式 `false`。每个结果包含 `node_id`、`rendered` 和可选 URL；null URL 表示该节点未能渲染。Figma 当前文档说明图片 URL 有效期为 30 天，导出上限为 3,200 万像素，较大图片可能被缩小。请将 URL 作为临时访问链接处理。插件不会获取返回 URL 的内容，也不会向这些链接发送凭据。

发布评论需要 `file_key` 和 `message`（1–10,000 字符，为插件设定的上限）。回复时将 `reply_to` 设为顶层评论 ID；Figma 不允许继续回复一条回复。本版本不支持指定画布位置、删除评论、修改已解决状态、编辑设计或复制文件。

## 限制与验收

操作时限为 60 秒，HTTP 超时 50 秒，请求上限 1 MiB，响应上限 8 MiB。大型文件或评论列表可能超过限制。插件禁用重定向和自动重试；错误消息不会包含上游原始响应正文或令牌。评论请求超时时，评论可能已经存在，应先检查文件再重新发布。API 限流取决于接口级别、令牌用户、套餐和席位。

测试数据根据官方 REST 文档与 OpenAPI 规范合成、适配，包含 null 节点和渲染失败情形。开发期间未访问真实 Figma 账号、文件或评论。生产使用前，请验收实际令牌权限、团队可见性、图片渲染和受控评论。

## 官方参考

- [文件接口](https://developers.figma.com/docs/rest-api/file-endpoints/)
- [目录接口](https://developers.figma.com/docs/rest-api/folders-endpoints/)
- [评论接口](https://developers.figma.com/docs/rest-api/comments-endpoints/)
- [用户接口](https://developers.figma.com/docs/rest-api/users-endpoints/)
- [官方 OpenAPI 规范](https://github.com/figma/rest-api-spec/blob/main/openapi/openapi.yaml)

参考文档核对日期为 2026-11-04。SDK v0.7.8 运行时通过 `SetDoc` 上传英文指南，不会上传语言字典。通过注册表安装时，会打包 `locales/zh-CN.json` 中以完整英文指南为精确键的中文翻译；仓库同时提供独立中文指南供阅读。
