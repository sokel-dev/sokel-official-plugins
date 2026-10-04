# threads — Threads（Meta）发布器（P1 最后一个）

与其余发布器同一套 publish 契约。面向用户的说明书是 [docs/threads.md](docs/threads.md)。

## 四条设计判断

1. **两步发布包成一个操作**。Meta 的形状是「建媒体容器拿 creation_id → 发布它」，
   但那是它的实现细节，不该变成画布上的两个节点。容器 id **不是**帖子 id——
   混用的话下游拿到的链接打不开。
2. **带媒体必须等容器就绪再发布**。Meta 建完容器还要在后台把文件拉下来处理，
   立刻发布会被拒；不等的话表现是**随机失败、重试又能成**——最难查的那一类。
   状态 ERROR 时把 Meta 给的 `error_message` 原样带出来（多半是那个地址它下载不到）。
3. **媒体是「让它来拉」不是上传**。所以给的必须是公网可达的绝对地址：
   平台在把文件交给插件时会换成带签名的下载地址，`mediaURLs` 只收 `http(s)://` 开头的，
   相对路径一律丢弃——让 Threads 去下载 `/api/v1/files/xx` 只会得到一句无关的错误。
4. **图片与视频的参数名不同**（`image_url` / `video_url`），给错了 Meta 只说
   「缺少必需参数」，不会告诉你给错了哪个。按扩展名判（去掉 query 再看）。

多个媒体走轮播：每个先建 `is_carousel_item` 容器，再建一个 `CAROUSEL` 容器把它们串起来。

## 令牌与配额

- **60 天到期且没有 refresh_token**（续期是拿长令牌换新的长令牌）。平台侧的 `threadsOAuth`
  在换码时做了**两跳**：授权码 → 1 小时短令牌 → 60 天长令牌。**只做第一跳的话，
  凭证一小时后失效**，而那时没人会联想到少了一步。
- **每 24 小时 250 条**（账号级滚动窗口）。`health_check` 顺带把已用/总额度带出来，
  工作流可以据此决定还发不发——这比撞上 429 再重试划算。

## 平台侧接入点（本插件新增）

`server/internal/credential/oauth.go` 的 `providers` 表加 `threads` 实现 +
`config.go`/`api/server.go` 的 `THREADS_OAUTH_*` 四个环境变量 +
`api/seed.go` 目录行与 `plugin_test.go` 计数。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

测试（假 Threads）钉的正是上面那四条 + 帖串串接 + 额度带出 + 令牌过期文案 + 输入守卫。

## 没做的

- **删帖**：Threads API 的删除能力尚不稳定，与其猜一个端点不如不做（发错了去 App 里删）。
- **读**（自己的帖子/回复/洞察）：本插件定位是发布器。
