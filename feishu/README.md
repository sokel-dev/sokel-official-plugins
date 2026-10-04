# feishu — 飞书自建应用插件（第一方自部署）

22 个操作（消息/卡片/查人/群管理/云文档/多维表格/网盘 + `call` 保底）+ 3 个事件
（收到消息 / 卡片按钮点击 / bot 被拉进群）。面向用户的说明书是
[docs/feishu.md](docs/feishu.md)。群 webhook 机器人是**另一个插件**
（`../feishu-webhook`）：凭证形态与授权范围不同，不混池。

## 为什么这是全站第一个引厂商 SDK 的插件

此前所有插件都是裸 HTTP。飞书破例引 [larksuite/oapi-sdk-go](https://github.com/larksuite/oapi-sdk-go)
v3（MIT），理由只有两个，都写在 `client.go` 顶注：

1. **长连接事件订阅（larkws）**——事件不走公网 webhook，插件主动向飞书建
   WebSocket。帧协议是飞书私有的（protobuf），自己实现又脆又不值。
2. **tenant_access_token 生命周期**——获取/缓存/过期刷新，SDK 内置。

用法上仍然克制：REST 一律走 raw `client.Do`（路径/请求体自己拼，与其他插件
风格一致）；typed 模块只用在 multipart 上传（im 图片/文件、drive）。

## 踩过/防住的坑（改代码前先读）

- **content 是「JSON 串」不是 JSON**：`/im/v1/messages` 的 content 字段要双重编码
  （`jsonStr()`），拼错的表现是 invalid content。
- **发送带 uuid 幂等键**（`sendUUID`）：从平台 Trace（run_id+node_id）派生，工作流
  重试不双发。**没有 Trace 时必须不带 uuid**——试调用拿恒定键会把第二次试调用
  静默去重成「不发」。
- **业务码非 0 大多是 HTTP 200**：`callRaw` 统一拦下转错误；`call` 保底操作例外
  （用户要原样 code/msg 对照文档），走 `callRawFull`。
- **高频错误码翻译**（`feishuErr`）：230002=拉 bot 进群、99991672=开权限并**重新发布
  版本**、1254050=把表格分享给应用。飞书的 msg 是给开发者的英文，看到报错的是
  画布上的用户。
- **docx 单次追加上限 50 块**：`appendBlocks` 分批，长报告一次几百块是常态。
- **Markdown→docx 是行级保守转换**：行内加粗/链接原样留为文本。docx 的行内 style
  模型复杂一个数量级，精排版走 `call` 直调 blocks API。
- **client 按 app_id 缓存**（`clientOf`）：每次新建=每次重新换 token，白吃频控。
  测试里每个用例独立 app_id，否则 baseURL 会串。
- **baseURL 第三分支透传完整地址**：httptest 假飞书与将来的私有化部署都靠它。
  测试打到真 open.feishu.cn 的症状是 `code:10003`。

## 事件源（events.go）

per-credential：一条凭证（=一个应用）一条长连接，多应用单实例由 SDK 的 source
supervisor 管（telegram 多 bot 同款）。去重靠飞书的 event_id 交平台按
(pluginId, event, eventID) 处理，插件内不自建去重表。

用户侧前置（写在 docs）：开放平台「事件与回调」订阅方式选**长连接**，勾选
`im.message.receive_v1` / `im.chat.member.bot.added_v1`；卡片回调同样选长连接。

## 文件

| 文件 | 干什么 |
|---|---|
| `schema/` | 契约（事实源），`go generate` 出 `zz_*.go` |
| `client.go` | SDK client 缓存 + raw 调用 + 错误码翻译 |
| `im.go` | 消息（发/回/撤/上传），uuid 幂等 |
| `misc.go` | 查人/群管理/`call`/health_check |
| `content.go` | docx（含 Markdown 转块）/ bitable / drive |
| `events.go` | larkws 长连接 → 三类事件 |

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
