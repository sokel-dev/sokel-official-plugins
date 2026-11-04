# Google Docs

通过 Google OAuth 读取文档并自动编辑文字。在 OAuth 应用对应的 Google Cloud 项目启用 Google Docs API，配置平台的 `google` 提供商，然后授权凭据。申请 `documents` 和 `userinfo.email` 权限；平台负责管理访问令牌与刷新令牌。授权账号必须拥有目标文档的访问权限。

## 操作

- **获取文档和标签页文字**返回文档 ID、标题、当前修订版本及包含子标签页的扁平列表。每个标签页包含 ID、父标签页 ID、正文和正文结束索引。支持段落、表格单元格及目录文字；不导出格式、图片、页眉、页脚、脚注或建议。纯文本仅供阅读，不是原文档的索引映射。
- **创建空白文档**使用标题创建文档。再次获取文档以取得首个标签页 ID，再插入文字。文件夹、共享、复制和导出请使用 Google Drive。
- **插入或追加文字**要求指定标签页 ID。省略 `index` 或填写零，会在该标签页最后一个换行符之前追加；正数表示在对应位置插入。Google Docs 的索引按 UTF-16 代码单元计算，不是字节数或 Unicode 字符数。
- **替换标签页文字**仅在指定标签页中按字面匹配替换。默认不区分大小写。空替换值会删除匹配内容。请检查 `occurrences_changed`；零表示没有匹配。
- **删除文字范围**使用包含起点的 `start_index` 和不包含终点的 `end_index`。不得删除标签页末尾换行符，也不得拆分 Unicode 代理对。结构位置应来自 Google 文档模型，不能根据扁平文本偏移推算。
- **检查凭据**只读调用 Google 用户信息接口。成功只验证授权，不代表对所有文档有权限，也不代表已启用 Docs API。

如果写入必须在“读取后文档未被其他人编辑”的情况下执行，请将最新 `revision_id` 传入 `required_revision_id`。省略该值会针对当前文档写入。修订版本不匹配时不会自动重试，应重新读取并决定如何应用编辑。每次编辑使用一个 batchUpdate 请求，写入均不会自动重试。

示例：获取模板文档，选择标签页 ID，再将 `{{customer}}` 替换成工作流变量。如需为每个客户创建副本，先用 Google Drive 复制。本插件不提供自动触发器。

请求仅访问固定 Google 域名，拒绝重定向，JSON 上限为 16 MiB。大型文档可能需要更有针对性的集成。API 失败返回英文错误，不包含上游响应正文。测试套件基于模拟响应，尚未完成真实账号验收。

参考：[Docs 标签页](https://developers.google.com/workspace/docs/api/how-tos/tabs)、[请求格式](https://developers.google.com/workspace/docs/api/reference/rest/v1/documents/request)、[batchUpdate 与写入控制](https://developers.google.com/workspace/docs/api/reference/rest/v1/documents/batchUpdate)。
