# 高德地图

通过高德 Web 服务 API 进行地址解析、地点搜索、路线规划、实时天气查询和行政区划查询。

## 凭证与配额

在[高德控制台](https://console.amap.com/)创建应用，获取 **Web 服务**类型的 API Key。浏览器、Android 和 iOS 密钥属于其他产品。为工作流所需接口开通权限，并检查账号配额及商业使用要求。在“Web 服务 API 密钥”中填写密钥。仅在此密钥开启数字签名时填写“可选签名密钥”，否则留空。签名密钥只用于计算签名，不会作为独立参数发送。

所有请求发往 `https://restapi.amap.com`。高德要求在查询参数中携带密钥，因此记录出站 URL 的基础设施应脱敏 `key` 和 `sig`。插件错误不会包含上游响应正文或含凭证的 URL；不跟随重定向，也不自动重试请求。

插件没有自动 `health_check`：验证本身会消耗配额。主动运行 `verify_credential` 并提供 `address`，将执行一次地址解析。成功仅表示地址解析可用，不代表其他 API 权限或剩余配额充足。

## 操作

| 操作 | 输入与行为 |
| --- | --- |
| `geocode_address` | 必填 `address`，可选 `city`；通过 `/v3/geocode/geo` 返回匹配地址和坐标。 |
| `reverse_geocode` | 必填 `location`；可选 `options.radius` 为 0 至 3000 米，默认 1000；返回格式化地址和行政区划信息。 |
| `search_places` | 必填不超过 80 个字符的 `query`；可选 `options.region`、`city_limit`、分类 `types`、`page` 和 `page_size`；使用 V5 关键字搜索。 |
| `search_nearby` | 必填 `location`、可选不超过 80 个字符的 `query`；支持同样的筛选项，以及 `radius`（0..50000 米，默认 5000）和 `sort`（`distance` 或 `weight`，默认 `distance`）。 |
| `get_place` | 必填 `place_id`；以数组返回 V5 地点基础信息。不存在的 ID 可返回空数组。 |
| `plan_route` | `mode` 为 `driving` 或 `walking`；提供 `origin` 和 `destination`。驾车支持 `options.strategy` 0 至 20，默认 0；步行拒绝该选项。 |
| `get_weather` | 必填六位 `adcode`；返回实时观测，不提供天气预报。 |
| `list_districts` | `query` 为必填区划名称或代码；可选 `options.depth` 0..3（默认 1）和 `options.page` 1..10000（默认 1）；返回带父级行政代码的扁平区划列表。 |
| `verify_credential` | 必填探测 `address`；执行一次消耗配额的地址解析。 |

## 坐标、分页与输出

坐标使用高德 GCJ-02 坐标系，顺序为 `经度,纬度`，最多六位小数。WGS-84/GPS 坐标需要在插件外转换。经度范围 -180..180，纬度范围 -90..90。路线距离单位为米、时长为秒、过路费为元，均以字符串保留上游格式。步行路线受高德 100 公里服务限制。首版不提供公交路线或坐标转换。

地点搜索默认第 1 页、每页 10 条；每页允许 1..25 条，页码为 1..200，同时受 200 条召回窗口约束。`returned_count` 是实际返回数量。满页时设置 `has_more=true` 并给出下一页 `next_page`，因此最后一次请求可能返回空数组。插件不将高德返回的 count 当作可靠总量。累计返回行达到 200 条召回上限时返回 `page_limit_reached=true`、`has_more=false`、`next_page=0`：结果受接口上限截断，不一定完整。起始位置超过前 200 条的请求会在本地拒绝。每页大小不能整除 200 时，最后一页可以是带截断标记的不满页。缩小查询词或区域后可继续检索。

行政区划每页最多 20 个根匹配项。下级区划会展开并携带 `parent_adcode`，不计入根匹配项页大小。根匹配项满页表示可能存在下一页。插件限制单次响应最多展开 10000 个区划，分页最多 10000 页。街道级行政代码可能与上级相同，因此不能在所有层级仅凭 `adcode` 唯一标识区划。

插件将文档约定的缺失文本值（`[]` 或 `null`）转为空字符串；文本字段出现非空数组会报错。空结果数组有效；缺少集合或业务失败会报错。响应上限为 8 MiB。天气时间和行政名称保留上游格式与语言。界面标签和错误默认使用英文，另提供中文翻译。

地点搜索示例：

```json
{"query":"coffee","options":{"region":"110000","city_limit":false,"page":1,"page_size":10}}
```

## 验证与参考

测试使用根据官方响应结构编写的模拟数据，覆盖请求参数、显式 0/false、分页限制、签名编码、平台生成绑定、超大响应和错误脱敏。测试不代表真实密钥权限、配额或线上服务已验收。

官方文档：[地址解析](https://lbs.amap.com/api/webservice/guide/api/georegeo)、[V5 地点搜索](https://lbs.amap.com/api/webservice/guide/api-advanced/newpoisearch)、[路线规划](https://lbs.amap.com/api/webservice/guide/api/direction)、[天气](https://lbs.amap.com/api/webservice/guide/api/weatherinfo)、[行政区划](https://lbs.amap.com/api/webservice/guide/api/district)和[数字签名](https://lbs.amap.com/faq/quota-key/key/41181/)。
