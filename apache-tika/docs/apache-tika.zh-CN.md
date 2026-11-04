# Apache Tika

通过自建 Apache Tika Server 提取文档文本、元数据和嵌入文档内容，或检测文件媒体类型，无需 SaaS 账户。本版本面向 **Apache Tika Server 4.2.0 兼容端点与元数据**。Apache Tika 采用 Apache-2.0 许可证，随附依赖另有通知。Tika 3.x 的端点和元数据键不同，本版本不支持。

## 部署与凭证

将必填部署环境变量 `SOKEL_TIKA_URL` 设置为服务基础地址，例如 `http://tika:9998` 或 `https://documents.example.org/tika`。插件支持内网 HTTP、HTTPS 和反向代理路径前缀。服务必须已运行，且能从插件访问。没有公共服务回退，也不会自动部署。URL 不允许包含用户信息、查询、片段、百分号编码路径或点路径段。

无需认证的内网服务可将两个凭证字段都留空。反向代理使用 HTTP Basic 认证时，请同时填写 **Basic 认证用户名** 和 **Basic 认证密码**。这不会启用 Tika 原生用户管理，只会向配置的端点发送标准 Authorization 请求头。不可信网络应使用 HTTPS。不开放自定义请求头、Bearer 令牌或 OAuth。插件拒绝所有重定向，确保文档和凭证只发往配置的地址。

Tika 4.x 在派生的 JVM 工作进程中解析文档，服务端需要可用的 Pipes 配置。请安装文档所需的解析器、字体、OCR 程序及语言数据，并在 Tika 中配置资源限制。本插件不会安装解析器、启用 `/pipes`、提交外部下载 URL 或修改单次请求的解析器设置。提取结果为空不能证明 OCR 可用。

## 操作

| 操作 | 请求与输出 |
| --- | --- |
| `extract_text` | 将平台 `file` 上传至 `PUT /tika/json/text`。返回纯文本 `text` 和文档 `metadata`；该模式下 Tika 会拼接提取到的嵌入内容。 |
| `extract_metadata` | 将 `file` 上传至 `PUT /meta`，请求 JSON。返回容器文档的 `metadata`，不包含正文文本或递归文档列表。 |
| `extract_recursive` | 将 `file` 上传至 `PUT /rmeta/text`。返回 `documents`：容器在前，嵌入文档按上游顺序排列，每项包含 `text` 和 `metadata`。 |
| `detect_type` | 将 `file` 上传至 `PUT /detect`。返回 `media_type`，例如 `application/pdf`。检测成功不保证完整解析器可用。 |

本版本不提供健康检查捷径。可用一个已知的小文件调用 `detect_type`，真实验证配置的服务，而不修改业务资源。解析和检测会消耗服务资源，并可能按 Tika 配置创建服务端临时文件。

每次输入使用平台文件引用，例如：

```json
{"file":{"id":"<platform-file-id>","name":"report.pdf","mime":"application/pdf","size":1234}}
```

请使用真实前序文件操作输出的引用；上例中的 ID 和大小仅为占位值。必须提供平台文件 ID 和纯文件名，也接受 SDK 小型内联文件数据。本地文件系统路径和只有 URL 的引用会被拒绝。插件通过平台文件服务读取字节，将原始字节作为 HTTP 正文发送，不使用 multipart 封装。文件名作为检测提示；文件 MIME 非空时作为 Content-Type 发送。本插件不会下载任意文件或资源 URL。

## 稳定的元数据输出

元数据随文档格式而变化，因此使用按键排序的类型化条目列表返回：

```json
[
  {"name":"Content-Type","values":["application/pdf"]},
  {"name":"dc:creator","values":["Alex","Sam"]}
]
```

每项包含 `name` 和字符串数组 `values`。上游单值转换为单元素数组，上游数组的每个成员各转换为一个字符串。字符串保留原文；数字、布尔值和结构化 JSON 等非字符串值转为紧凑 JSON 字符串，大整数文本不会经过浮点数转换。这样既提供稳定的工作流字段，也保留未知解析器专属元数据键的值。这种规范化不保留原始 JSON 标量类型。

Tika 4 的特殊字段 `tk:content` 转为 `text` 输出，并从元数据列表中移除。缺失文本转为空字符串；没有 OCR 的图片、空白文档或没有可提取文本的文档都可能合法返回空文本。其他元数据均保留。嵌入资源路径只作为元数据，不会解释为本地文件路径。`extract_recursive` 不返回嵌入文件字节，也不下载附件。

## 部分结果与错误

Tika 可能在 HTTP 200 中报告解析异常、写入上限、嵌入资源上限、任务超时或元数据截断。插件拒绝非空的 `tk:exception:*` 或 `tk:warn:*` 标记，显式 `false` 值除外。包括递归子文档出现标记的情况，整个操作都会失败，不会把部分结果当作成功提取返回。这项保守策略也会拒绝某些在 Tika 中非致命的解析警告。错误不会回显异常详情字符串。

响应必须具有预期媒体类型及有效 JSON 结构，每个文档必须包含有效的 `Content-Type`。旧版 `X-TIKA:*` 字段会触发明确的版本兼容错误。200 响应未报告警告并不能证明提取完整：服务端过滤器可能移除字段，解析器有格式限制，部署设置也会影响结果。请保留 Tika 的失败和截断元数据，使上述检查能生效。

HTTP 错误、无效文档元数据、多个连续 JSON 文档、无效 UTF-8、结果超限和平台文件错误都会明确失败。错误不包含上游错误正文、凭证或连接 URL。429 响应会报告受限的重试等待时间，但不会自动重试。任何请求都不会自动重放；调用方可以检查服务状态后明确决定重试。

## 限制与功能范围

输入文件上限为 32 MiB。读取前检查声明大小，读取后检查实际字节数；非零声明大小必须与实际字节数一致。SDK 会将文件载入内存。提取响应上限为 16 MiB，递归结果最多 10000 项。检测响应上限为 4 KiB。输入或输出超过上限会失败，不会静默截断。

操作的平台超时为 300 秒，HTTP 超时为 240 秒。服务端和代理限制可能更早结束请求。插件不会覆盖 Tika 超时、OCR 配置或嵌入资源数量上限。不开放任意服务端路径、外部下载器、配置端点、语言检测、提取文件归档或异步 Pipes 任务。仓库分别记录 fixture 测试与真实服务验收。

## 官方参考

- [Tika Server 4.2 文档](https://tika.apache.org/docs/4.2.x/using-tika/server/index.html)
- [Tika Server 4.x 迁移](https://tika.apache.org/docs/4.2.x/migration-to-4x/migrating-tika-server-4x.html)
- [Tika 4.2.0 提取端点源码](https://github.com/apache/tika/blob/4.2.0/tika-server/tika-server-core/src/main/java/org/apache/tika/server/core/resource/TikaResource.java)
- [Tika 4.2.0 元数据键与异常标记](https://github.com/apache/tika/blob/4.2.0/tika-core/src/main/java/org/apache/tika/metadata/TikaCoreProperties.java)
- [Tika 4.2.0 元数据序列化](https://github.com/apache/tika/blob/4.2.0/tika-serialization/src/main/java/org/apache/tika/serialization/serdes/MetadataSerializer.java)
- [许可证与依赖通知](https://github.com/apache/tika/blob/4.2.0/LICENSE.txt)
