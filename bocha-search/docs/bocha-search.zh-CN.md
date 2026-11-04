# 博查搜索

使用博查 API Key 搜索网页。插件使用博查官方 provider 实现中的 `https://api.bocha.cn/v1/web-search`，不跟随重定向，不自动重试付费请求。

## 凭据

在[博查控制台](https://open.bochaai.com/)创建 API Key，填入 `access_token`，并确保账户有足够余额。密钥申请和更换在插件外完成。

`verify_credential` 必填 `probe_query`，执行一次付费搜索，只取一个结果且不生成摘要。缺少查询时会在本地拒绝，不发送请求。验证成功只表示当时可访问，不保证后续余额充足。本插件不声明 `health_check`：平台无参数的凭据测试按钮无法执行此项显式付费验证，会提示不支持自动健康检查。请调用验证操作或正常搜索检查权限。

## 操作

- `web_search`：必填 `query`，可选 `freshness` 和 `options`。
- `verify_credential`：必填付费验证查询 `probe_query`，行为如上。

`options.count` 默认为 10，范围 1–50。`options.summary` 默认为 true，显式 false 会被保留。时效默认 `noLimit`，也可用 `oneDay`、`oneWeek`、`oneMonth`、`oneYear`、单个日期（如 `2026-01-15`），或升序的包含起止日的区间（如 `2026-01-01..2026-01-31`）。

结果包含 `id`、`name`、`url`、`snippet`、`summary`、`site_name`、`published_at`。上游缺失的可选文本返回空字符串，并附带 `query`、`total_estimated_matches`、`log_id`；缺失的估算数量或日志信息返回零或空字符串。当前版本不包含分页、爬取、AI 问答接口或域名过滤。数量限制表示请求结果数，不代表完整搜索结果。

```json
{"query":"agent workflow integrations","freshness":"oneMonth","options":{"count":5,"summary":false}}
```

## 限制和错误

JSON 请求和响应上限为 8 MiB。鉴权、权限、限流和业务错误不会直接回显上游响应正文。超时后计费状态可能不确定，请先检查账户用量再重试。空结果数组合法；上游明确返回成功码 200 时，也允许 data 或网页结果缺失/null，与官方 provider 行为保持一致。如果既无结果集合，也无明确成功码，则验证失败。

请求样例和生成绑定测试覆盖公开协议，使用的是构造数据，并非真实账户验收。开发期间未进行真实搜索或市场发布。本插件没有触发器或 webhook。

参考：[博查官方 provider](https://github.com/bocha-ai/dsh-web-search-bocha)、[博查控制台和 API 示例](https://open.bochaai.com/)。
