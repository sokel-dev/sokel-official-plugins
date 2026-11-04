# Grafana

在自建 Grafana 实例上查看仪表盘、发现安全的数据源摘要并记录注释。基线为采用 AGPLv3 许可证的 **Grafana OSS 13.3.0**，无需 Grafana Cloud 或 Enterprise 许可证。

首版使用现有 `/api` HTTP 路由。Grafana 13 将这些路由标记为弃用并推荐 `/apis`，但官方文档明确说明旧路由仍可使用。插件已在 13.3.0 验证，不声称兼容所有未来版本，也不实现较新的仪表盘资源模型。

## 配置

将必填部署环境变量 `SOKEL_GRAFANA_URL` 设为**站点根地址**，例如 `http://grafana:3000` 或 `https://monitor.example.org/grafana`，不要附加 `/api`。支持内网 HTTP 和反向代理前缀；可信网络之外请使用 HTTPS。地址不能包含凭据、查询参数、片段、编码后的路径段或点路径段，不会回退到公共服务。

在实例中创建**服务账户令牌**并填入插件凭据表单。插件仅向配置的 Grafana 服务发送 `Authorization: Bearer <token>`，不创建账户或令牌，不刷新令牌，不管理权限，也不调用服务器管理 API。读取需要相应的仪表盘、数据源和注释访问权限；创建注释需要写入或创建权限，在 OSS 部署中通常使用可访问目标仪表盘的 Editor 服务账户。Enterprise 安装可能要求更具体的 RBAC 操作权限。请使用目标组织的令牌，仅授予必要访问权限。

`health_check` 使用 limit 1 进行经过鉴权的仪表盘搜索，返回 `ok` 和 `message`。它验证此读取操作的访问能力，不代表具有所有数据源或写入权限，不会依赖公开的 `/api/health` 端点宣称令牌有效。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| `health_check` | 经过鉴权的仪表盘搜索检查。 |
| `list_data_sources` | 返回摘要：UID、名称、类型、默认标记和只读标记。 |
| `get_data_source` | 输入 `uid`，返回对应数据源的同类安全摘要。 |
| `search_dashboards` | 可选 `query` 及 `options.page`、`limit`、`tags`、`folder_uids`、`starred`。返回仪表盘摘要和 `next_page`，只搜索仪表盘，不搜索文件夹。 |
| `get_dashboard` | 输入 `uid`，返回 UID、标题、版本及 `model_json`。 |
| `list_annotations` | 可选 `filter.from`、`to`、`limit`、`dashboard_uid`、`panel_id`、`tags`。返回用户创建的注释和 `limit_reached`，排除告警状态注释。 |
| `create_annotation` | 输入非空 `text` 和明确的纪元毫秒 `time`。可选 `options.time_end`、`dashboard_uid`、`panel_id`、`tags`。创建一条注释并返回十进制字符串 ID。 |

数据源输出明确省略连接 URL、用户名、密码、`jsonData`、安全字段标记和任意配置。插件不创建或更新数据源，不向其代理请求，不执行 Prometheus 查询或任意插件查询，也不暴露其凭据。列表端点**不支持分页**；Grafana 可配置的服务器上限默认为 5000。返回列表不保证包含全部已配置数据源；必要时使用 `get_data_source` 按已知 UID 获取。

仪表盘模型包含面板和插件特定结构，因此 `model_json` 为 JSON **文本**，保留原始模型和精确整数字面量。它包含令牌可读取的用户编写内容、查询和链接；向预期受众之外分享前请检查这些内容。插件不会读取数据源密钥来补充模型。版本以字符串返回。搜索结果的仪表盘 URL 是服务提供的导航信息，插件不会请求这些 URL。

## 仪表盘搜索

页码从 1 开始，默认和最大每页数量为 100，最大页码为 100000。标签和文件夹 UID 以重复查询参数发送，各最多 100 项。翻页时保持所有搜索选项不变。满页返回下一页页码，短页或空页返回 0。Grafana 搜索端点没有总数或继续令牌，因此最后一页刚好满页时，可能需要额外请求一次空页。分页不是快照；扫描期间内容变化时请按仪表盘 UID 去重。

提供 `starred: false` 时会明确发送；省略时保留服务默认行为。星标状态属于服务账户身份，不是交互式人类用户账户。传入的 UID 必须由 1–128 个 ASCII 字母、数字、`_` 或 `-` 组成，拒绝 URL 或路径语法。搜索查询上限为 4096 字节，每个标签最多 255 字节。

## 注释读写

创建输入示例：

```json
{"text":"Release deployed","time":"1507037197339","options":{"time_end":"1507037198340","dashboard_uid":"production","panel_id":"0","tags":["deploy"]}}
```

输入和输出时间均为十进制字符串表示的纪元毫秒。创建时间必须大于零，支持范围截至公元 9999 年，区域结束时间不能早于开始时间。筛选允许零值，根据 Grafana 语义表示无边界或默认下界。面板 ID 为非负 64 位十进制字符串；指定面板时必须同时提供仪表盘 UID。`panel_id: "0"` 会保留，但 Grafana 将零视为未指定，而非选中编号面板。不提供仪表盘 UID 时，创建的是组织级注释。读取时标签主要用于筛选组织级注释，不能替代仪表盘筛选；多个标签按 Grafana 的 AND 语义匹配。

注释读取默认及最多返回 100 条，此 API 没有通用分页游标。若 `limit_reached` 为 true，结果可能不完整，应缩小时间范围或其他筛选条件。切分时间窗口并按 ID 去重可能有帮助，但同一毫秒的大量注释仍可能超过限制；插件不保证完整导出或恰好一次事件投递，也不会轮询或发出事件。

创建注释立即写入，并可影响显示匹配组织标签或指定仪表盘、面板注释的仪表盘。正文上限为 1 MiB，最多支持 100 个标签。这些操作不更新或删除注释，不修改仪表盘，不执行告警，也不管理用户。

## 传输与可靠性

仅调用配置服务上的固定 API 路径，禁用重定向和 Cookie 复用，不提供任意 HTTP URL 输入。错误消息不含服务错误正文或传输 URL。JSON 请求上限为 2 MiB，响应上限为 8 MiB。操作允许 60 秒，HTTP 超时为 45 秒。

失败的 HTTP 响应不会重试，写请求正文不能被自动重放。429 响应会给出有界的 `Retry-After` 建议，不会自动重试。创建注释没有插件级幂等键；发生超时后应检查已有注释再决定是否重复写入。Grafana 服务自身可能访问已配置的数据源、插件或外部集成，这些依赖仍由你的部署控制。

## 验证与来源

全部七项操作已在隔离的 Grafana OSS 13.3.0 上通过启用竞态检测的验收，使用合成 Editor 服务账户、仪表盘、无凭据数据源和注释。离线测试使用脱敏后的真实捕获响应及合成边界变体，覆盖精确整数、显式 false 和零、生成的分发代码、省略数据源配置、异常成功回执、错误和重定向拒绝。这属于本地服务验收，不代表已部署或发布到生产平台。

官方来源：[13.3.0 发布](https://github.com/grafana/grafana/releases/tag/v13.3.0)、[旧版 API 状态](https://grafana.com/docs/grafana/latest/developer-resources/api-reference/http-api/api-legacy/)、[固定版本数据源 API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/data_source.md)、[仪表盘搜索 API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/folder_dashboard_search.md)、[注释 API](https://github.com/grafana/grafana/blob/v13.3.0/docs/sources/developer-resources/api-reference/http-api/api-legacy/annotations.md)、[仪表盘处理器](https://github.com/grafana/grafana/blob/v13.3.0/pkg/api/dashboard.go)及 [AGPLv3 许可证](https://github.com/grafana/grafana/blob/v13.3.0/LICENSE)。
