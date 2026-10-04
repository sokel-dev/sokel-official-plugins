# mastodon — 发布器插件（P0 三件套之一）

与 [bluesky](../bluesky/README.md) 同一套 publish 契约（[docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §3）：
发布回 `id` + `url`、长内容分段归插件、媒体随发布走、健康检查用平台约定的 `health_check`。

面向用户的说明书是 [docs/mastodon.md](docs/mastodon.md)（随握手上报）。设计判断写在 `schema/schema.go` 顶部。

## 四条与别家不同的

1. **字数上限问实例**（`client.go::maxChars`）。Mastodon 是联邦网络，500 只是官方默认；
   不少中文实例是 5000、有的到 11000。写死的话，用户在网页上明明发得出去的长文，
   在这里被插件自己拦下。问一次缓存住。
2. **发布带幂等键**（`Idempotency-Key`，一小时内同键只落一条）。工作流会重试，
   不带的话一次超时重试就是时间线上两条一样的嘟文。键取「正文+回复目标+可见性+CW」的摘要——
   重跑同一个节点这几样不会变。
3. **CW 与可见性整串继承**。一串里混进公开与不列出，读者只能看到断断续续的半串。
4. **视频要等转码**。v2 媒体接口对图片同步返回、对视频返回 202 且 url 为空，
   这时发嘟会被 422 拒（错误只说「媒体不可用」）。插件轮询到处理完再发。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
