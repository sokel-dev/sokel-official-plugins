# linkedin — LinkedIn 个人动态发布器（P1）

与三个 P0 发布器同一套 publish 契约。面向用户的说明书是 [docs/linkedin.md](docs/linkedin.md)。

## 为什么只做个人号

个人动态走自助产品「Share on LinkedIn」+ `w_member_social`，**点了就有、无审批**；
公司页要 `w_organization_social` + 合作伙伴计划，审批周期以周到月计。
两者塞进一个插件，会让「为什么我发不了公司页」变成一个永远解释不清的问题——
真要发公司页时另开一个插件，那时它的凭证、作用域、审批状态都和这个不一样。

## 四条设计判断

1. **两个头都必须带**：`LinkedIn-Version`（YYYYMM）与 `X-Restli-Protocol-Version: 2.0.0`。
   少任何一个都是 426 或形状对不上，而错误信息不会告诉你缺的是头。版本**钉死**在
   `linkedInVersion`——LinkedIn 每月发一版、老版本约一年后停用，升级应当是显式动作。
2. **author 必须是账号自己的 URN**（`urn:li:person:{sub}`，从 `/v2/userinfo` 拿）。
   它不会变，所以按 access_token 缓存——每次发帖都去问一次是纯浪费。
3. **发帖成功时 id 在 `x-restli-id` 响应头里**，正文可能是空的。只解析 body 的话，
   会得到「发成功了但没有 id」，下游拿不到链接也删不掉。
4. **图片是三步**：`initializeUpload` 拿上传地址与 URN → PUT 二进制到 LinkedIn 给的
   **临时地址**（不是 API 域名、不带版本头）→ URN 塞进帖子。单图走 `media`，多图走 `multiImage`。

## 令牌 60 天，且多数应用没有刷新令牌

自助接入的应用只拿到 60 天的 access_token，refresh_token 要单独申请
（Marketing Developer Platform）。所以平台侧的 `linkedinOAuth` **不像 Google 那样把
「没有 refresh_token」判成哑弹**——判了的话所有自助凭证都授权不完；走 Notion 那条路，
access_token 直接落进凭证。

到期不会悄悄失灵：`health_check` 会把它标成 invalid，「凭证失效」告警随之触发
（见 `docs/dev-playbook.md` §4.4）。401 的错误文案直接点名「多半是 60 天到期了」。

## 文件

| 文件 | 干什么 |
|---|---|
| `schema/schema.go` | 操作/凭证/认证契约（**事实源**，改完 `go generate`） |
| `post.go` | 出站（两个头 + 错误翻译）、账号 URN 缓存、发/删/健康检查、图片三步上传 |

## 平台侧接入点（本插件新增）

- `server/internal/credential/oauth.go`：`providers` 表加 `linkedin` 实现；
- `server/internal/config/config.go` + `internal/api/server.go`：`LINKEDIN_OAUTH_*` 四个环境变量；
- `server/internal/api/seed.go`：目录行 + `plugin_test.go` 的计数。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

测试（假 LinkedIn）钉的是上面那四条 + 401 的文案 + 输入守卫 + 动态链接转 URN。

## 没做的

- **公司页**（见开头）、**@提及**（LinkedIn 要 URN 不是 @名字，自动化容易 @ 错人）、
  **视频**（另一套分片 + ETag 流程，等有场景再加）。
