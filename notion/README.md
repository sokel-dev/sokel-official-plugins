# notion — 读写 Notion + 数据源变动触发（第一方自部署插件）

18 个操作（搜索/读页/查表/建页改页/markdown 正文/评论/文件上传…）+ 2 个事件
（新增行 / 行被修改）。面向用户的说明书是 [docs/notion.md](docs/notion.md)。

## 三条设计判断（详见 `schema/schema.go` 顶部）

1. **按数据源建模，不留 `database_id` 的老形状**。2025-09-03 起 Notion 把 database 拆成
   「容器 + 数据源」，一个库可以挂多个数据源；查询、建行、关联用的都是 `data_source_id`。
   按旧形状做出来的契约，用户一给多源库就全线报错，再改就是破坏性迁移（n8n 已经历过一次）。
2. **正文走 markdown，块树只作兜底**。`GET/PATCH /pages/{id}/markdown` 让「读一页给模型 /
   让模型改一页」变成一次调用；块级接口仍保留，因为数据库块、嵌入这类东西 markdown 表达不了。
3. **属性给两份**：`props` 归一化（下游与模型直接可读）、`properties_raw` 原样
   （rollup/formula 这类归一化必然丢信息的类型有退路）。

## 认证两种并存

内部集成密钥（凭证里填 `ntn_` 开头的 token，自部署最省事）**或** OAuth 授权
（平台侧 notion provider 代答）。两者是同一类东西——「这个集成能看到哪些页面」的凭据，
所以不拆成两条凭证行：填了 token 用 token，没填则用授权拿到的 access_token。

## 事件源为什么是轮询

Notion 的 webhook 订阅**只能在它的集成设置页手工建**（还要把 verification_token 粘回去验证），
API 建不了，一个集成也只能挂一个 URL——「插件替用户装好 webhook」这条路它不给。
要实时就把画布上 webhook 触发节点的地址粘进 Notion（说明书里有）。

## 文件

| 文件 | 干什么 |
|---|---|
| `schema/` | 契约（事实源） |
| `client.go` | 出站：认证、代理、限流（3 次/秒）、错误翻译 |
| `props.go` | 属性归一化（两份都给的那一半） |
| `read.go` / `write.go` | 读侧 / 写侧操作 |
| `watch.go` | 事件源：按 `last_edited_time` 拉增量 |

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
