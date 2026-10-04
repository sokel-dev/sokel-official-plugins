# telegram-bot — Telegram Bot 的发送/操作侧（第一方自部署插件）

16 个操作（发消息/图片/文件、编辑、删除、按钮应答、webhook 管理…）+ 4 个事件
（收到消息 / 消息被编辑 / 按钮点击 / bot 成员状态变化）。
面向用户的说明书是 [docs/telegram-bot.md](docs/telegram-bot.md)。

## 两条设计判断

1. **一个通用 `call` + 一组 typed 便捷操作**。`call` 收 `method` + `params`，
   覆盖**整个** Bot API——Telegram 加新方法时这里零改代码；而常用的那十几个另做成
   typed 操作，因为画布上要的是「填字段」而不是「拼 JSON」。两者并存不是重复：
   前者保底，后者好用。
2. **bot_token 绝不进节点入参与输出**。它只在插件内部拼进 URL 路径——
   token 一旦出现在入参里，就会跟着运行记录、画布变量、日志四处扩散，而那是收不回来的。

## 定位：只做「发送/操作」这一半

一个全功能 TG bot 分两半——发送/操作（本插件，纯出站调 `api.telegram.org`）与
接收/触发（收到消息 → 起工作流）。后者靠平台的**事件源**机制（`updates.go` 里的长轮询），
而不是让用户去公网架 webhook。

## 文件

| 文件 | 干什么 |
|---|---|
| `schema/` | 契约（事实源） |
| `updates.go` | 事件源：getUpdates 长轮询 → 四类事件 |
| `webhook_ops.go` | webhook 的设置/查询/删除（与长轮询二选一，别同时开） |

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
