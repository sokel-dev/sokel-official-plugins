# Crawl4AI

通过自有 Crawl4AI Docker 服务器的 REST API 抓取网页并提取 Markdown。本插件面向自托管 0.9.5 协议，提供五项操作，不使用 Crawl4AI Cloud，也不要求 LLM 服务账户。浏览器及服务器自身依赖需要由管理员预先安装，插件不会下载这些组件。

## 部署与凭证

将必填的部署环境变量 `SOKEL_CRAWL4AI_URL` 设置为服务器根地址，例如 `http://crawl4ai:11235` 或 `https://internal.example/crawler`。不要添加 `/crawl`，也不要包含 URL 凭证、查询参数或片段。支持内网 HTTP 地址和反向代理路径前缀，没有默认公共端点。插件容器内的 `localhost` 指向插件容器自身，而非宿主机或爬虫容器；请使用插件能够访问的服务名称或地址。

在插件凭证中，将 **访问令牌** 设置为自托管服务器配置的 API 令牌或已有且被服务器接受的 JWT，不要添加 `Bearer` 前缀。插件会在 `Authorization` 请求头中自动添加该前缀。请在插件外获取和续期凭证。Crawl4AI 0.9.x 需要在服务器端配置 `CRAWL4AI_API_TOKEN`：未配置时，安全默认设置只绑定服务器容器内部回环地址，发布到宿主机的端口可能仍不可达。此服务器端设置与插件端的 `SOKEL_CRAWL4AI_URL` 是两个独立配置。

**检查服务可用性** 读取公开的 `/health`。`ok=true` 只确认服务响应符合预期，不证明令牌有效、浏览器已就绪或某个网站可以抓取。请抓取一个获准访问的页面来验证完整流程。健康检查失败时，返回 `ok=false` 和简洁说明。

## 操作

| 操作 | 行为 |
| --- | --- |
| `health_check` | 读取服务器健康状态，返回 `ok` 和 `message`。 |
| `crawl` | 向 `/crawl` 提交 1–5 个不同页面 URL，并等待一次有时间上限的同步响应；返回强类型页面结果和 `all_succeeded`。 |
| `submit_crawl` | 使用相同 URL 和选项结构向 `/crawl/job` 提交任务；HTTP 202 返回 `task_id` 与 `accepted=true`，不代表抓取已完成。 |
| `get_task` | 向 `/crawl/job/{task_id}` 发起一次请求；返回状态、完成标志、逐页结果和安全提示，不在内部循环轮询。 |
| `markdown` | 通过 `/md` 使用固定 `raw` 过滤方式抓取一个 URL；返回页面 URL 和原始 Markdown。`use_cache` 默认为 false。 |

对于 `crawl` 和 `submit_crawl`，`urls` 必须包含 1–5 个不同的绝对 HTTP 或 HTTPS URL，每个不超过 8192 字节，且不能嵌入凭证。`markdown` 的单个 `url` 使用相同限制。不接受原始 HTML、文件 URL 或 JavaScript URL。目标页面可以在爬虫内部遵循自身重定向，但须满足服务器出站策略；插件会拒绝配置的 REST 服务本身返回的重定向。

部署服务可以位于内网，而目标页面仍受 Crawl4AI 安全策略约束。当前服务器会校验页面目的地址，并可能拒绝私有网络、回环地址或 DNS 变化。插件不会关闭这些防护。自托管爬虫并不会让外部网站离线可用；网站也可能要求认证、阻止自动化或拒绝访问。

## 抓取选项与输出

可选的 `options` 对象只包含以下字段：

- `page_timeout_ms`：页面导航超时，默认 30000，接受范围为 100–60000 毫秒。
- `word_count_threshold`：最小词数，范围为 0–10000；省略时使用服务器默认值，显式零值会保留。
- `only_text`：是否只提取文本；省略时使用服务器默认值，显式 false 会保留。
- `css_selector`：可选内容选择器，单行且不超过 2048 字节。

请求始终关闭流式输出。该接口不开放任意浏览器选项、自定义请求头、Cookie、代理、Python 或 JavaScript 钩子、代码执行、LLM 设置、Webhook、截图或 PDF 产物。已支持的路径无需 LLM API 密钥。`markdown` 固定使用 `f=raw`；fit、BM25 和 LLM 过滤方式均不在本版本范围内。

页面结果暴露 `url`、`success`、可选的 `status_code`、可选的 `title`、可选的 `markdown` 以及 `message`。Markdown 来自上游结构化结果的 `markdown.raw_markdown`，不是 HTML 或 LLM 回答。空 Markdown 可能表示有效的空页面。失败页面使用安全提示替代原始服务器错误，并省略标题和 Markdown。上游失败记录中无效的 URL 会替换为空字符串。不暴露内部堆栈、HTML、响应头、提取数据或服务器链接。

HTTP 请求成功，或上游批次的 `success=true`，并不代表每个页面都成功。请检查 `all_succeeded` 和各页面的 `success`。即便上游抓取标志为 true，页面 HTTP 状态达到 400 或以上时，仍会被视为页面失败。

## 异步任务

保存 `submit_crawl` 返回的 `task_id`，在工作流适当等待后调用 `get_task`。支持的 ID 使用服务器的 `crawl_` 前缀，后接八个小写十六进制字符。插件自行构造规范任务路径，不跟随服务器返回的链接。

| 状态 | 含义 |
| --- | --- |
| `processing` | `completed=false`、`all_succeeded=false`，暂时没有页面结果；稍后查询同一 ID。 |
| `completed` | `completed=true`；仍需检查 `all_succeeded` 和页面结果，因为个别页面可能失败。 |
| `failed` | `completed=false`、`all_succeeded=false`，并提供安全失败提示；这是终止的失败任务，不表示应无限轮询。 |

任务属于提交时的身份。请使用同一身份查询；对于其他身份的任务、过期任务或未知 ID，服务器可能返回 404。服务器 Redis TTL 和清理策略决定结果保留时间，读取较旧的终止任务也可能触发上游清理。请在工作流中保存已完成结果，不要把任务存储视为永久档案。插件不会自动重新提交失败或过期任务。

## 限制与错误

请求限制为 1 MiB，完整上游响应限制为 8 MiB，包括插件稍后丢弃的字段。即使需要的 Markdown 很小，大量 HTML 仍可能导致超限；此时请减少每次抓取页面数或缩小选择器范围。HTTP 请求最长 150 秒；同步抓取和 Markdown 操作声明的平台超时为 180 秒。其他操作声明 60 秒超时，同时遵循调用方取消。耗时较长的工作请使用异步提交并在稍后读取状态。

所有操作都不会进行应用层自动重试，POST 正文也不会标记为可重放。提交或同步请求超时后，服务器可能仍在工作；手动重复请求前，请检查已知任务。HTTP 错误只暴露状态和安全提示，不把原始上游错误、令牌或内部 URL 复制到错误中。HTTP 401 或 403 表示需要检查服务器令牌或访问策略；HTTP 429 会给出有界的重试等待时间。

Crawl4AI 采用 Apache-2.0 许可证，并附有额外署名要求。其要求的署名原文如下：“This product includes software developed by UncleCode (https://x.com/unclecode) as part of the Crawl4AI project (https://github.com/unclecode/crawl4ai).”
