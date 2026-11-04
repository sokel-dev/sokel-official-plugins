# Confluence Cloud

使用 Confluence Cloud v2 API 读取空间、创建和更新已发布页面。凭据检查使用 v1 当前用户接口。不支持 Server 或 Data Center。

## 凭据与权限

在 Sokel 凭据中保存 Atlassian 账号 `email` 和 `api_token`。无作用域令牌配置 `site` 为 `https://your-site.atlassian.net` 或裸主机名。带作用域令牌改为填写租户 `cloud_id` UUID；它优先于 site，使用固定网关 `https://api.atlassian.com/ex/confluence/{cloud_id}`。Cloud ID 可从站点的 `/_edge/tenant_info` 或管理员处获取。

两种路由均使用邮箱/令牌的 Basic 认证。仅接受 HTTPS Atlassian Cloud 站点或固定的作用域令牌网关。不允许 URL 用户信息、自定义域名、端口、路径、查询参数和重定向。过期令牌需手动替换，不支持 OAuth 或自动刷新。

授予目标空间、页面的权限及所用接口的读写作用域。v2 读取要求空间/页面读取作用域，写入要求页面写入作用域；v1 身份检查还要求相应用户读取作用域。`health_check` 只读调用 `GET /wiki/rest/api/user/current`，返回 `ok` 和脱敏 `message`，不会验证所有空间的写入权限。

## 操作与分页

| 操作 | 行为 |
| --- | --- |
| `list_spaces` | 读取一页可访问空间，可按空间 key 过滤。 |
| `get_space` | 根据数字 ID 读取空间。 |
| `list_pages` | 读取一页已发布页面，可按空间 ID、精确标题过滤。 |
| `get_page` | 根据数字 ID 读取页面、正文和当前版本。 |
| `create_page` | 在空间中创建已发布页面，可指定父页面。 |
| `update_page` | 校验给定当前版本后替换标题和正文。 |
| `health_check` | 使用 v1 接口读取当前用户身份。 |

列表接受 `limit`（1–100，默认 50）和 `cursor`。将 `next_cursor` 用于下一次调用，保持过滤条件不变；空游标表示结束。插件从 v2 `_links.next` 或 `Link` 响应头提取游标，并校验域名和资源路径，不直接请求任意分页 URL。

空间输出包括 ID、key、名称、类型和状态。页面输出包括 ID、空间/父页面 ID、标题、状态、版本、正文、格式、创建时间戳和网页路径。`web_path` 为上游路径，不保证是完整 URL。日期原样返回。页面、父页面和空间 ID 必须是正整数字符串。

## 页面正文与写入

`representation` 支持 `storage`（默认，Confluence 存储 XHTML）或 `atlas_doc_format`（编码为字符串的 JSON 文档）。ADF 必须包含 `type: "doc"` 和 `version: 1`，其余内容由 Confluence 校验。不支持将纯 Markdown 或渲染视图 HTML 作为其他格式。Storage 内容原样发送，插件不渲染或净化标记。

```json
{"space_id":"12345","parent_id":"67890","title":"Research notes","body":"<p>Quarterly findings.</p>","representation":"storage"}
```

创建仅发布 current 页面。更新前先调用 `get_page`，结合正文合并修改，再将返回版本作为 `expected_version`，同时提交完整的新标题和正文：

```json
{"page_id":"67890","expected_version":7,"title":"Research notes","body":"<p>Revised findings.</p>","version_message":"Update analysis"}
```

插件会再次 GET，拒绝过期版本或非 current 页面，之后在本例提交版本 8。检查后若其他作者并发修改，API 冲突会原样归入冲突错误，不会自动覆盖重试。发生冲突后需重新读取并合并。这会替换完整正文。必须提供完整的期望内容，省略或空正文都会被拒绝。如需主动清空 storage 正文，请发送 `<p></p>` 等明确的空段落。标题和正文均为必填。

Confluence 可能将已发布页面的更新与现有草稿合并，内容差异较大时可能覆盖草稿。版本检查只保护已发布版本，替换前请检查现有草稿；本插件不管理草稿。

## 限制与错误

平台操作超时 60 秒，HTTP 超时 55 秒，请求上限 4 MiB，响应上限 8 MiB。HTTP 失败返回脱敏英文错误，不包含上游正文、凭据或 URL。空数组合法，缺失列表字段报错。不自动重试限流或其他错误。写入超时可能已提交，重试前应检查页面。

本版本不创建/修改空间、不删除页面、不管理草稿、附件或权限限制、不支持 CQL 搜索，也没有事件触发器。合成协议样例覆盖请求字节、分页链接和并发冲突，目前尚未进行真实账号验收。
