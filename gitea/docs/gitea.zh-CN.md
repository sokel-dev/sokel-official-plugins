# Gitea

将工作流连接到自己的 Gitea 实例，读取仓库、跟踪议题并创建拉取请求。本插件面向 Gitea 28.3.0 及其 `/api/v1` REST API。Gitea 使用 MIT 许可证，无需托管服务账户或公共 Gitea 端点。

## 配置服务

将部署环境变量 `SOKEL_GITEA_URL` 设为 Gitea **站点根地址**，例如 `http://gitea:3000` 或 `https://git.example.org/team`，不要附加 `/api/v1`。可信内网支持 HTTP；流量经过不可信网络时请使用 HTTPS。反向代理的路径前缀会保留。地址不能含凭据、查询参数、片段、编码后的路径段或 `.` / `..` 路径段，不会回退到公共服务。

在插件凭据表单中填写 **API 令牌**。可在 Gitea 的用户设置 → 应用 → 管理访问令牌中生成。插件仅向配置的服务发送 `Authorization: token <token>`，不实现 OAuth 登录、令牌创建或刷新。仅选择工作流所需的权限：健康检查需要 `read:user`，仓库操作需要仓库访问权限，议题及拉取请求操作需要相应的议题权限。写操作需要对应的写入 scope 和账户本身的仓库权限；`write:repository` 与 `write:issue` 可覆盖本插件的仓库协作功能。令牌 scope 不会授予账户原本没有的权限。健康检查读取 `/api/v1/user`；成功表示令牌有效，不代表能访问每一个仓库。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 读取已认证用户后返回 `ok` 和 `message`。 |
| `list_repositories` | 列出令牌所属用户可访问的仓库。可选 `pagination.page` 和 `pagination.limit`；返回 `repositories` 与 `next_page`。 |
| `get_repository` | 输入 `owner` 和 `repository`；返回仓库 ID、完整名称、可见性、默认分支和网页地址。 |
| `list_issues` | 输入仓库、可选 `state`（`open`、`closed`、`all`）及分页参数。默认列出开放议题，并明确排除拉取请求。 |
| `get_issue` | 输入仓库和十进制 `number`。Gitea 的议题与拉取请求共享编号，请检查 `is_pull_request`。 |
| `create_issue` | 输入仓库和 `title`。可选 `options.body`、`closed`、`assignees`、`label_ids` 仅在提供时发送。 |
| `add_comment` | 输入仓库、议题或拉取请求的 `number` 及非空 `body`，创建一条评论。 |
| `list_pull_requests` | 输入仓库、可选 `state` 和分页参数。默认列出开放的拉取请求。 |
| `create_pull_request` | 输入仓库、`title`、`head`、`base` 和可选 `body`。`head` 可为分支名，或 fork 仓库的 `owner:branch`。分支必须已存在，并有 Gitea 可比较的差异。 |

仓库名称和所有者均为单独的名称，不能填写 URL 或 `owner/repository` 路径。支持 Unicode 字母、数字以及 `_`、`-`、`.`。ID、标签 ID、议题和拉取请求编号使用十进制**字符串**，因此也能准确传递超出 JavaScript 安全整数范围的数值；必须是无前导零的正 64 位整数。

创建议题的输入示例：

```json
{"owner":"team","repository":"project","title":"Review report","options":{"body":"Review the attached report.","closed":false,"assignees":["reviewer"],"label_ids":["12"]}}
```

明确提供的 `closed: false` 会原样发送。省略的选项不会被补成 false 或零。标题最多 255 个 UTF-8 字节；描述和评论最多 1 MiB。每个议题最多接受 100 名受理人和 100 个标签 ID。创建议题、评论或拉取请求会立即写入，并可能触发 Gitea 实例自身的通知、Webhook 或自动化。本插件不合并拉取请求，不创建仓库，不管理用户，也不执行管理操作。

## 继续获取列表

页码从 1 开始，默认及最大每页数量为 50，页码范围为 1–100000。保持仓库、状态和每页数量不变，将返回的 `next_page` 作为 `pagination.page` 继续调用，直至为 0。插件依据 Gitea 的 `Link` 响应头并检查 `X-Total-Count`，不会仅凭短页判断结束。下一页 URL 必须保持配置的来源、端点、筛选条件和每页数量，并恰好推进一页；对外只返回下一页页码。缺失必要分页响应头会报错，请配置反向代理保留这些响应头。

列表不是快照。并发变更可能使结果在页间移动；需要时按返回的对象 ID 去重。每次调用仅返回一页，不会自动获取整个集合。

## 错误与部署限制

插件拒绝重定向，不复用 Cookie，也不向其他主机转发凭据。错误消息不包含服务错误响应正文或传输 URL。请求没有自动重试循环，不重试失败的 HTTP 响应，写请求正文也不会被自动重放。写入后发生超时时，应先检查 Gitea 再决定是否重试；这些创建操作不提供插件级幂等键。429 错误会根据 `Retry-After` 给出有界等待建议，但不会安排再次请求。

JSON 响应上限为 8 MiB，操作超时为 60 秒。插件不下载仓库文件，不克隆 Git 仓库，也不获取任意 URL。自建 Gitea 服务自身可能配置了外部集成，这些连接仍由你的部署控制。

## 验证与参考

本集成已在隔离的 Gitea 28.3.0 服务中使用合成仓库、议题、评论和拉取请求完成验证，包括议题的两页读取。离线测试还覆盖真实捕获响应、生成的分发代码、Unicode 名称、精确 ID、可选 false、异常分页、拒绝重定向和错误脱敏。这属于本地服务验收，不代表已部署到生产平台。

官方来源：[Gitea 28.3.0 发布](https://github.com/go-gitea/gitea/releases/tag/v28.3.0)、[固定版本 API 定义](https://github.com/go-gitea/gitea/blob/v28.3.0/templates/swagger/v1-swagger.generated.json)、[API 鉴权与分页](https://docs.gitea.com/development/api-usage/)及 [MIT 许可证](https://github.com/go-gitea/gitea/blob/v28.3.0/LICENSE)。
