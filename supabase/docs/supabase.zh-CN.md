# Supabase

通过 Data REST API 操作数据库记录和函数，并通过 Storage 列出对象、生成签名下载链接。在插件进程配置 `SUPABASE_URL`，例如 `https://your-project.supabase.co`。只填写项目源地址，不包含路径或查询参数。每个项目单独部署实例。必须使用 HTTPS；本地开发可对 localhost 或回环 IP 使用 HTTP。

## 凭据

将**项目 API 密钥**设为 publishable/secret 密钥，或旧版 anon/service-role JWT。新 `sb_publishable_` 和 `sb_secret_` 密钥只通过 `apikey` 请求头发送；旧密钥同时作为 bearer token。可选的**用户访问令牌**会覆盖 bearer token，不会自动刷新。使用服务端 secret 密钥且不需要终端用户身份时，可留空。

secret/service-role 密钥可绕过行级安全策略（RLS），只应提供给需要该权限的工作流。publishable/anon 密钥和用户令牌受项目授权与 RLS 策略控制。拥有 API 密钥不代表拥有所有表的权限。请启用 Data API 并开放目标 schema。

## 数据库操作

`select_rows`、`insert_rows`、`upsert_rows`、`update_rows` 和 `delete_rows` 接受表或视图名，以及已开放的 schema（默认 `public`）。表、schema、函数和筛选列名仅支持字母、数字、下划线，且以字母或下划线开头。不支持带引号或含 schema 前缀的名称。行对象由项目自己的数据库列和 JSON 值组成，显式 false 和 null 均会保留。超过 JavaScript 安全整数范围（±9,007,199,254,740,991）的输入请用字符串传递，平台或 SDK 的 JSON 绑定可能舍入数字输入。响应解码保留原始数字位数；下游需要时应使用字符串或任意精度解析。

多个筛选条件使用 AND。每项包含 `column`、`operator` 和字符串 `value`，例如 `{"column":"id","operator":"eq","value":"42"}`。支持 `eq`、`neq`、`gt`、`gte`、`lt`、`lte`、`like`、`ilike`、`in`、`is`、`cs`、`cd`、`ov`；值遵循 PostgREST 语法，例如 `in` 的 `(1,2)`，或 `is` 的 `null`。`select`、`limit` 等查询控制名称不能作为筛选列。更新和删除至少需要一个筛选条件，所有匹配且可访问的行都可能被修改；写操作没有自动影响行数上限。

读取支持 PostgREST `select` 表达式、`order`、1–1000 的页大小（默认 100）和 offset。建议使用 `id.asc` 等稳定唯一排序。持续使用 `next_offset`，直到返回空页；服务器可能返回少于请求上限的行数。服务器提供精确总数时，会包含在 `content_range` 中。其他客户端同时写入时，offset 分页不保证快照一致性。

插入和 upsert 接受 1–1000 个非空行对象。Upsert 使用 merge-duplicates，可通过逗号分隔的 `on_conflict` 唯一列指定冲突目标，默认使用主键。同一批行应使用兼容的列集合。写入请求返回行数据，因此可能还需要 SELECT 权限。写入不会自动重试。

`call_function` 向数据库函数提交命名 JSON 参数，函数本身可能修改数据。`result_json` 字符串保留标量、对象、数组或 null 结果，需要结构化字段时可在工作流中解析。

## 存储与凭据检查

`list_objects` 按名称排序，使用 offset 分页列出 bucket 的文件夹前缀。结果可能包含对象 ID 为空的文件夹条目。`create_signed_url` 为已有对象创建临时下载链接，默认有效 3600 秒，可设为 1–604800 秒。持有链接的人在有效期内可以读取对象。这些操作不会上传或删除对象。

`health_check` 需要表名，可选 schema，通过只读 HEAD 请求返回 `ok` 和 `message`。缺少表名时会报告尚未验证，包括平台凭据检查未传入参数的情况。成功代表身份与表访问检查通过，但 RLS 仍可能隐藏所有行。存储权限由对应操作单独验证。

JSON 请求与响应限制为 16 MiB。拒绝重定向，错误不包含上游响应正文。此版本不提供 Realtime 触发器。自动化检查采用官方 API 结构与模拟响应，真实项目验收需单独完成。

参考：[Data REST API](https://supabase.com/docs/guides/api)、[API 密钥](https://supabase.com/docs/guides/getting-started/api-keys)、[PostgREST 表与筛选](https://docs.postgrest.org/en/stable/references/api/tables_views.html)、[Storage API](https://supabase.com/docs/reference/self-hosting-storage)。
