# 天眼查

通过天眼查商业数据 API 查询企业工商信息和工商变更记录。

## 凭证与接口权限

从[天眼查数据平台](https://open.tianyancha.com/)获取令牌，开通企业信息 **API 365** 和工商变更 **API 822**。在“商业 API 令牌”中填写令牌。插件向 `https://open.api.tianyancha.com` 的 `Authorization` 请求头直接写入原始令牌；凭证值不要加 `Bearer` 前缀。

网站会员资格本身不代表拥有这些商业 API 的调用权限。接口权限、账户余额、剩余次数和 IP 白名单由天眼查管理，不同接口的授权可能不同。运行工作流前，请在账户中确认当前商业条款。

两个操作都可能消耗付费调用次数，因此插件不注册自动 `health_check`，也不另设付费探测操作。验证凭证时请有意识地执行企业查询；API 365 成功不代表 API 822 已开通。平台可能显示不支持自动凭证测试。插件不提供刷新流程，需要时请在外部获取并更换令牌。

## 操作

| 操作 | 输入 | 结果 |
| --- | --- | --- |
| `get_company` | `keyword`：API 365 支持的企业名称、企业 ID 或注册代码 | `company`：企业 ID、名称、登记状态、信用代码、法定代表人、注册资本、行业、注册地址、经营范围、成立时间和内嵌主要人员 |
| `list_company_changes` | `keyword`：企业名称或 ID；可选 `page.page` 和 `page.page_size` | `changes`、`total`、`has_more` 和 `next_page` |

`get_company` 使用 `/services/v4/open/baseinfoV3`。所有 ID 均输出字符串，包括服务端以 JSON 数字返回的 ID，从而保留超过 JavaScript 安全整数范围的精度。`established_at_ms` 为原始 Unix 毫秒时间戳字符串；负数表示 1970 年之前。缺失的可选字段转为空字符串。注册资本保留上游原始格式和单位。

`company.staff` 是基础信息响应内嵌的主要人员子集。应将数组长度与 `staff_total` 对比：该接口可能只返回部分人员。此操作不会额外翻页获取全部人员。首版不提供通用企业搜索、股东或风险接口。

`list_company_changes` 使用 `/services/open/ic/changeinfo/2.0`，查询参数为 `keyword`、`pageNum` 和 `pageSize`。默认第 1 页、每页 20 条。插件允许页码 1..10000、每页 1..20 条，这是实现层限制，不代表服务端的全部上限。仅在 `has_more` 为 true 时使用 `next_page`，同时保持查询词和每页大小不变。分页数据缺失或矛盾时会报错，不会静默返回截断的成功结果。超过插件页数上限仍有数据时，操作返回错误。

变更记录包含 `item`、`before`、`after`、`changed_at` 和 `created_at`。文本、标记和日期保留上游表示。业务码 `300000` 表示没有结果：变更列表返回空数组和总数 0，企业详情查询返回明确的未匹配错误。即使 HTTP 状态为 200，非零业务码仍会报错。错误区分权限、过期、限流、余额、次数和 IP 限制，不回显上游 reason 或令牌。

变更查询示例：

```json
{"keyword":"Example company","page":{"page":1,"page_size":20}}
```

## 限制与验证

请求仅发往固定 HTTPS 域名，不跟随重定向，不自动重试，包括付费读取。响应上限为 8 MiB。界面标签、说明和错误默认使用英文，并提供中文翻译。

测试采用按官方示例构造的模拟数据，验证原始令牌认证、ID 和时间戳精度、分页参数、空结果、业务失败、平台生成输出、凭证脱敏和响应大小限制。模拟测试未执行付费请求或真实账号验收。发布前需使用已授权账户验证两项 API 权限及代表性响应。

官方参考：[API 365：企业基本信息](https://open.tianyancha.com/open/365)和 [API 822：工商变更](https://open.tianyancha.com/open/822)。
