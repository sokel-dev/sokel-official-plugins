# LinkedIn（个人动态）

以授权账号的**个人身份**发动态。自助接入、**无需合作伙伴审批**，适合投研观点触达专业受众。

> 本插件**只发个人动态，不发公司页**。公司页要 `w_organization_social` + 合作伙伴计划审批
> （周期以周到月计）；把两者塞进一个插件只会让「为什么我发不了公司页」变成解释不清的问题。

## 配置

1. 到 [LinkedIn 开发者后台](https://www.linkedin.com/developers/apps) 建一个 App
   （要关联一个公司页，这只是建 App 的前提，不影响发个人动态）；
2. **Products 里加两个自助产品**：`Sign In with LinkedIn using OpenID Connect` 和
   **`Share on LinkedIn`**。两个都是点了就有，不用审批；
3. Auth 页里把回调地址填成平台的：`https://<你的平台域名>/api/v1/credentials/oauth/callback`，
   与平台环境变量 `LINKEDIN_OAUTH_REDIRECT_URL` **一字不差**；
4. 平台管理员配好这几个环境变量：
   ```
   LINKEDIN_OAUTH_CLIENT_ID / LINKEDIN_OAUTH_CLIENT_SECRET
   LINKEDIN_OAUTH_REDIRECT_URL / LINKEDIN_OAUTH_PROXY（连不上时填）
   ```
5. 建凭证 → 点「授权」，作用域是 `openid profile w_member_social`。

## ⚠️ 令牌 60 天到期

LinkedIn 的访问令牌**只活 60 天**，而且**多数自助应用拿不到刷新令牌**
（那个要单独申请 Marketing Developer Platform）。所以到期后要人再点一次「授权」。

这不会悄悄失灵：凭证的「检查凭证」会在它失效时把状态标成 invalid，
「凭证失效」告警随之触发，可以接一条工作流通知你去重新授权。

## 操作

| 操作 | 说明 |
|---|---|
| 发动态 | 正文 + 可选图片 + 可见性。回 `id`（URN）与 `url` |
| 删动态 | 只能删自己发的；URN 或动态链接都收 |
| 检查凭证 | 令牌还有效吗（顺带回账号姓名） |

## 正文怎么写

**纯文本 + 换行，不认 Markdown 也不认 HTML**。写 `**加粗**` 出来就是四个星号。
上限 3000 字符。

正文里的链接由 LinkedIn 自动识别并生成卡片，**不用自己拼**——放一个 URL 进去即可。

@提及在 LinkedIn 上要用 URN 而不是 @名字，本插件不做提及（要提及请人工发）。

## 图片

最多 9 张、每张 ≤10MB。给一张是单图，多张自动变成多图动态。
替代文本按顺序对应，少给几条不影响发布。

## 限制

| 项 | 上限 |
|---|---|
| 正文 | 3000 字符 |
| 图片 | 9 张，每张 10MB |
| 频率 | **150 次/人/天**、10 万次/应用/天 |

## 常见问题

**401**——多半就是 60 天到期了，重新授权一次。

**403**——授权时缺 `w_member_social`，或 App 没加「Share on LinkedIn」产品。
改完产品要**重新授权**。

**426**——插件钉的 `LinkedIn-Version`（当前 202601）被 LinkedIn 停用了，需要升一版。
LinkedIn 每月发一版、老版本约一年后停用；钉死是有意的，升级应当是显式动作。

**连不上**——linkedin.com 在境外，在**凭证**里配出站代理；容器里跑用
`http://host.docker.internal:7897`。
