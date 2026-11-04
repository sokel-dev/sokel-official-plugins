# Salesforce

使用固定为 **v65.0** 的 REST API 查询 Salesforce 记录和元数据，以及创建、更新或按外部 ID 写入记录。明确固定稳定版本，不跟随 `latest` 别名。

## 凭据与权限

将 `instance_url` 设置为 Salesforce OAuth 流程返回的 HTTPS 实例网址，例如 `https://example.my.salesforce.com`。支持生产 My Domain、沙箱或开发者 My Domain 子域，以及 `https://na123.salesforce.com` 这类传统实例域名。不接受登录或测试登录域名、自定义代理域名、端口、用户信息、查询参数和路径。

将 `access_token` 设置为外部获取的 OAuth 令牌。应用需要 `api` scope，用户需要 API Enabled 及相应对象、记录和字段权限。组织版本、会话策略、连接应用或外部客户端应用策略也会生效。本插件不提供 OAuth provider、令牌刷新或用户名密码登录，过期令牌需在外部更新。

`health_check` 读取需要鉴权的 API 配额，返回 `ok` 和脱敏的 `message`。它验证 API 访问能力，不代表有权操作所有 Salesforce 对象。

## 元数据与记录读取

`describe_object` 需要 `Account` 或 `MyObject__c` 这样的对象 API 名称。输出对象名称、显示标签、创建和更新标记，以及字段 API 名称、类型、是否可为空、写权限、外部 ID 标记和引用目标等元数据。可据此确定组织中的可写字段。

`get_record` 需要 `object_name` 和 15 或 18 位 `record_id`。可选 `field_names` 为简单 API 字段名，不支持 SOQL 表达式。选择字段时插件始终同时请求 Id。记录输出是动态对象，具体字段由租户结构和字段权限决定。

## 只读 SOQL 与分页

`query` 的 `soql` 接收最多 16,000 个字符的 SELECT 表达式，例如 `SELECT Id, Name FROM Account LIMIT 100`。查询通过 URL 编码传递，不拼入接口路径。为保证只读，拒绝 DML、注释、分号及 `FOR UPDATE`、`FOR VIEW`、`FOR REFERENCE`。这种保守校验也可能拒绝字符串字面量中出现的对应字符或词组。

输出包括 `records`、`total_size`、`done` 和 `next_records_url`。聚合查询和所选关系子查询保留在记录对象中。`done=false` 时，将完整的相对续页路径传给 `query_more`。仅接受 `/services/data/v65.0/query/<locator>-<offset>`，拒绝绝对网址和其他接口路径。每次读取一页，实际返回可能少于 2,000 条。查询定位器会过期，不能作为持久同步机制。嵌套关系结果集不会自动继续分页。

## 写入与数字精度

`create_record` 需要对象 API 名称和非空 `fields` 对象，`update_record` 还需要记录 ID。省略字段表示保持不变，JSON null 清空可为空的字段。更新仅在收到文档规定的空 HTTP 204 响应时视为成功。对象和字段须使用 API 名称，而非显示标签。插件禁止向 `Id` 和 `attributes` 写入，权限、校验规则及其他业务规则由 Salesforce 执行。

`upsert_record` 需要对象名、`external_id_field`、`external_id` 和字段值。请在 Salesforce 配置适用的外部 ID 字段。匹配到一条记录则更新，无匹配则创建，多条匹配会失败。本版本不支持特殊的 Id/null 插入形式或 `updateOnly`。外部 ID 值最多 255 个字符，不得包含正反斜杠、百分号、查询或片段标记及换行；该字段也不能重复出现在 `fields` 中。新版 Salesforce upsert 更新返回 HTTP 200 和 `created=false`，创建返回 HTTP 201 和 `created=true`，插件会验证两种回执。

由于组织自定义结构和 SOQL 投影不同，记录字段必须是不透明动态对象。响应 JSON 数字保留完整十进制表示。平台 SDK 将不透明输入数字经 float64 绑定，因此大整数或需精确的小数字段值，应在 Salesforce 允许时使用字符串传入。超出精确整数范围 ±9,007,199,254,740,991 的数值输入会被拒绝；这不等于保证任意小数输入都保持精度。

## 可靠性与限制

每个操作的平台超时为 60 秒，HTTP 超时为 55 秒。JSON 请求和响应上限为 8 MiB。请求仅发往经过验证的 Salesforce HTTPS 域名，拒绝重定向。错误不会回显敏感响应。所有操作均不自动重试，包括写入。创建、更新或 upsert 结果不确定时，重试前先读取或查询记录。Upsert 需要稳定且最好唯一的外部 ID，不保证 Salesforce 自动化只执行一次。写入可能触发流程、校验规则及其他组织自动化。更新不包含并发保护条件。

开发期间未调用真实账户，也未发布到市场。合成协议测试和生成入口测试都有明确标注。本版本不包含 Bulk API、Composite API、删除、文件传输、触发器、Change Data Capture 或订阅。

## 官方参考

- [REST 资源和请求](https://developer.salesforce.com/docs/platform/api-rest/guide/intro-rest-resources.html)
- [查询](https://developer.salesforce.com/docs/platform/api-rest/guide/resources-query.html)
- [对象元数据](https://developer.salesforce.com/docs/platform/api-rest/guide/resources-sobject-describe.html)
- [Upsert 与新版响应状态](https://developer.salesforce.com/docs/platform/api-rest/guide/dome-upsert.html)
