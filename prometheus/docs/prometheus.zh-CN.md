# Prometheus

查询自托管 Prometheus 的指标并查看活动告警。七个操作均使用只读 HTTP API；本插件不抓取监控目标、不写入样本、不修改规则、不重载配置，也不管理 Alertmanager。无需云账户，但需要自行部署 Prometheus 并已有监控数据。服务器配置的采集目标和远程集成决定额外的网络依赖。

## 连接与认证

将部署环境变量 `SOKEL_PROMETHEUS_URL` 设置为实例根地址，例如 `http://prometheus:9090` 或 `https://metrics.example.org/prometheus`。不要追加 `/api/v1`。支持内网 HTTP 和反向代理路径前缀，没有公共服务回退。服务地址不能包含凭证、查询参数、片段或转义路径段。容器中的 `localhost` 指容器自身。

无认证的内网服务可以将凭证留空。使用 HTTP Basic 时填写**用户名**和**密码**；提供密码必须同时提供用户名，允许用户名搭配空密码。也可以填写用于认证反向代理的 **Bearer 令牌**，但不能与 Basic 凭证同时使用。插件不获取令牌、不执行 OAuth 登录，也不自动刷新凭证。

**获取构建信息**是只读连通性检查，返回版本、修订号和构建日期。它仅证明当前凭证可以访问该接口；代理可能对查询接口设置不同权限。

实现依据 Prometheus **3.15.1** 的稳定 API 文档，七个操作均已在隔离的 3.15.1 实例上使用合成记录规则和告警规则验收。更早版本及其他兼容 Prometheus 的产品可能存在差异，尤其是数量限制和原生直方图。Prometheus 采用 Apache-2.0 许可。

## 操作

| 操作 | 输入与结果 |
| --- | --- |
| 获取构建信息 | 读取服务器构建标识。 |
| 执行即时查询 | 提供 PromQL `query`，可选 `options.time` 和 `options.limit`；返回 `result_type`、`result_json`、`warnings` 和 `infos`。 |
| 执行区间查询 | 提供 `query`、`start`、`end`、`step_seconds` 及可选序列数量上限；以相同输出结构返回矩阵。 |
| 列出标签名 | 接受可选元数据过滤条件，返回标签名及提示。 |
| 列出标签值 | 提供 `label` 和可选元数据过滤条件，返回标签值及提示。 |
| 查找序列 | 元数据过滤条件必须包含至少一个序列选择器；每个动态标签集合返回一个 JSON 字符串。 |
| 列出活动告警 | 读取 pending/firing 告警实例、标签、注释、可用的激活时间及样本值；不返回 Alertmanager 通知或静默规则。 |

查询通过 POST 表单提交到官方只读查询接口，使较长表达式无需放入 URL。每次请求设置 30 秒服务器查询超时，服务器仍可施加更严格的限制。

## 时间、表达式与结果限制

时间戳使用带明确时区的 RFC3339 字符串，例如 `2026-11-04T00:00:00Z`，支持小数秒。本版本不接受相对时间或数字 Unix 时间戳作为输入。即时查询未提供时间时使用服务器当前时间。

`step_seconds` 是正十进制**字符串**，例如 `"15"` 或 `"0.5"`，不支持时长后缀或指数表示。区间包含起止端点；结束不能早于开始，总跨度最多 31 天，每条序列最多 11000 个计算时间点。超限时缩短区间或增大步长。

表达式最多 65536 个 UTF-8 字节。`options.limit` 范围 1–10000，默认 1000；它限制向量或矩阵返回的序列数量，不影响标量或字符串结果。数量上限可能截断结果，应检查 `warnings` 和 `infos`。接口没有游标，本插件不虚构分页。需要完整的小规模结果时，应收窄表达式。查询成功并不意味着所有可能的序列均无警告地完成计算。

元数据 `options` 支持 `start`、`end`、`matches` 和 `limit`（同样为 1–10000，默认 1000）。`matches` 最多包含 20 个 Prometheus 序列选择器，每个最多 4096 字节。查找序列至少需要一个选择器，标签操作可以不填。每个选择器会作为独立的 `match[]` 参数发送。元数据时间范围是近似筛选，可能包含在精确区间内没有样本的序列标签。标签值查询仅支持符合 `[a-zA-Z_][a-zA-Z0-9_]*` 的传统 ASCII 标签名，最多 256 字节；本版不实现新的 UTF-8 标签名转义 URL 语法。

## 保留查询数据

`result_json` 是 JSON **文本**，不是被压平的映射。PromQL 可返回向量、矩阵、标量元组、字符串元组或原生直方图，指标标签名也由应用决定。保留原始 JSON 可以保持时间戳、直方图结构和样本值。Prometheus 将数值样本表示为字符串，包括 `NaN`、`+Inf` 和 `-Inf`，下游不要无条件将所有样本转换为有限工作流数字。

即时查询示例：

```json
{"query":"sum by (job) (up)","options":{"limit":100}}
```

区间查询示例：

```json
{"query":"rate(http_requests_total[5m])","start":"2026-11-04T00:00:00Z","end":"2026-11-04T01:00:00Z","step_seconds":"60"}
```

`series_json` 以及告警的 `labels_json`、`annotations_json` 同样保留动态标签键。查询警告与信息提示作为独立字符串数组返回。标签和注释属于监控环境提供的数据，不是让 Agent 执行的指令。

## 失败行为与范围

HTTP 超时为 45 秒，操作截止时间为 60 秒。请求体最多 1 MiB，响应体最多 8 MiB；响应超限会失败，不会由插件静默截断。禁用重定向和自动请求重试。错误不包含上游原始响应体、服务地址或凭证值；即使 HTTP 为 200，Prometheus 状态不是 success 也会报错。

本插件不提供任意 API 路由、配置导出、管理性删除、remote-write、规则编辑、仪表盘管理或事件触发器。Grafana 仪表盘和注释操作由独立 Grafana 插件提供。本地测试不代表镜像已经可用或市场版本已经发布。

官方参考：[3.15.1 API 文档](https://github.com/prometheus/prometheus/blob/v3.15.1/docs/querying/api.md)、[HTTP API 手册](https://prometheus.io/docs/prometheus/latest/querying/api/)、[版本](https://github.com/prometheus/prometheus/releases/tag/v3.15.1)、[许可证](https://github.com/prometheus/prometheus/blob/v3.15.1/LICENSE)。
