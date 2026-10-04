# wechat-claw —— 微信 iLink Bot 插件（收发一体）

基于 [wechat-clawbot-client-go](https://github.com/importcjj/wechat-clawbot-client-go)。
一条凭证 = 一个微信账号；多账号单实例由平台 per-credential supervisor 天然覆盖（配几个凭证跑几个账号）。

## 接入

```bash
SOKEL_ENDPOINT=https://<平台地址> \
SOKEL_TOKEN=skp_xxx \
./wechat-claw
```

平台侧：插件管理新建插件 → 拿组 token 启动本进程 → 凭证管理给该插件建凭证（字段留空）→
点凭证行「登录 / 授权」→ 弹二维码 → 微信扫码确认 → session 由平台写入凭证行 →
约 20s 内（下次心跳）事件源自动以该账号上线（实例表「事件源」列可见）。

## 能力

- **事件**：`message`（收到消息）——`chat_id`（对方 wxid，公共字段顶层平铺）/ text / message_id / 媒体计数 / raw。
- **操作**：`send_text` / `send_image` / `send_file`（to = 事件里的 chat_id）。
- **协作式登录**：schema 里 `auth.QR()` 声明 + 生成的 `RegisterAuth(p, start, poll)`（凭证面板扫码，不上画布）。

## 设计要点

- **凭证不落地**：session 读=注册下发（platformStore.Load），写=`sokel.credential.update` 回写
  （运行中 token 刷新不丢）；sync 游标与 context token 仅内存（重启代价：从当下重新收；
  重启后需对方先来一条消息才能回话）。
- **发送依赖运行中 client**（context token 在其 store 里）→ **微信组建议单副本部署**；
  多副本时 send 操作可能落到不持有该账号事件源的副本（会给出明确报错，不静默）。
- 会话失效 → 实例表该账号亮「待登录」，重新扫码即可（无需重启进程）。

## ⚠️ 合规

iLink bot API 属非官方通道，存在封号风险——建议专号专用，勿用主力账号。
