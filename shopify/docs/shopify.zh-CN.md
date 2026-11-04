# Shopify

使用 Shopify GraphQL Admin API **2026-10** 版本读取店铺、商品、订单和库存项数据，并更新商品标题或状态。

## 凭据与权限

将 `shop` 设置为 `.myshopify.com` 前的子域，例如 `example-shop`。不要输入完整网址、自定义店铺域名或 `.myshopify.com` 后缀。将 `access_token` 设置为该店铺签发的 Admin API 令牌。插件不会申请或刷新令牌；即将过期的令牌需在外部更新。不支持 Storefront 令牌。

应用和授权用户必须有权访问目标资源。读取商品需要 `read_products`；更新商品需要 `write_products` 以及更新商品权限。读取库存项需要 `read_inventory` 或 `read_products`。读取订单需要 `read_orders`，Shopify 通常只开放最近 60 天的订单；更早订单需另行获批 `read_all_orders`。受保护客户数据规则还可能限制应用。本版本不请求客户联系方式或地址字段。

`health_check` 执行真实的只读店铺查询，返回 `ok` 和脱敏的 `message`，不代表每个操作都有权限。`get_shop` 返回 ID、店铺名称、myshopify 域名和币种。

## 商品与库存项

`list_products` 接收 `limit`（1–100，默认 50）、可选 Shopify 搜索表达式 `query` 和分页 `cursor`。`get_product` 要求完整的 `gid://shopify/Product/...` ID。商品输出包括 ID、标题、handle、状态、供应商、商品类型、标签和更新时间。

`list_inventory_items` 使用同样的分页和搜索参数，返回库存项 ID、SKU、是否跟踪库存、是否需要运输及更新时间。它**不返回**各地点库存数量、价格或变体详情。

`update_product` 需要商品 GID 和非空的 `changes` 对象。支持 `title` 和 `status`，省略字段表示保持不变。状态可为 `ACTIVE`、`DRAFT` 或 `ARCHIVED`。不接受空标题。修改可能影响店铺可见性；仅设置 ACTIVE 不会自动向销售渠道发布商品。本插件不修改价格、变体、支付或退款。

## 订单

`list_orders` 接收同样的 `limit`、`query` 和 `cursor`。`get_order` 要求 `gid://shopify/Order/...` ID。输出包括订单名称、创建和更新时间、财务与履约状态、以店铺币种计价的当前总额。金额使用十进制字符串，不使用二进制浮点数。本版本不包含订单行、客户身份、履约操作或订单写入。

## 分页与失败处理

将 `next_cursor` 传入下一次请求时，保持查询和操作不变。仅在 `has_more=true` 时继续。每次调用读取一页；分页期间的数据变化可能影响结果，游标不是快照或持久同步检查点。受 Shopify GraphQL 查询成本限制。插件不会自动重试限流请求或写操作。

每个操作的平台超时为 60 秒，HTTP 超时为 55 秒。请求和响应上限均为 8 MiB。调用仅发送到所配置店铺的官方 HTTPS Admin API 地址，并拒绝重定向。GraphQL 错误、变更 userErrors、资源缺失及异常成功响应均明确失败。上游错误文本可能含敏感输入，因此不会原样展示。若响应头表明使用了其他 API 版本，插件会明确失败，避免默默接受版本回退。

超时或写入回执异常后，重试前先读取商品确认状态。开发期间未调用真实店铺，也未发布到市场。测试使用明确标注的合成协议夹具。不包含触发器、Webhook、批量任务或库存写入。

## 官方参考

- [GraphQL Admin 商品查询](https://shopify.dev/docs/api/admin-graphql/2026-10/queries/products)
- [更新商品](https://shopify.dev/docs/api/admin-graphql/2026-10/mutations/productUpdate)
- [订单及访问限制](https://shopify.dev/docs/api/admin-graphql/2026-10/objects/Order)
- [库存项](https://shopify.dev/docs/api/admin-graphql/2026-10/queries/inventoryItems)
