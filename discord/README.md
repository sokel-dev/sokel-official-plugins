# discord — 发布器插件（P0 三件套之一）

与 [bluesky](../bluesky/README.md) 同一套 publish 契约（[docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §3）：
发布回 `id` + `url`、长内容分段归插件、媒体随发布走、健康检查用平台约定的 `health_check`。

面向用户的说明书是 [docs/discord.md](docs/discord.md)（随握手上报）。设计判断写在 `schema/schema.go` 顶部。

## 定位与三条判断

**社群分发，不是公开发现渠道**——发进去只有频道成员看得到。研报出来推一条到投研群用它；
要公开曝光用 bluesky / mastodon / x。

1. **走 Webhook 不做 Bot**。发消息这件事上两者能力一样，而 Webhook 只要频道管理员点几下，
   Bot 要建应用、配 intents、邀请进服务器、管权限。
2. **嵌入卡片是主形态**。财经推送是「标题 + 摘要 + 链接 + 几个数字」，裸文本在群里不可读。
   卡片字段**按键名排序**——map 遍历是随机的，不排的话同样的输入每次排版都不同。
3. **回执要给消息 id**：Webhook 默认回 204 空体，插件恒定带 `?wait=true`，
   否则下游想改/删这条消息都做不到。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
