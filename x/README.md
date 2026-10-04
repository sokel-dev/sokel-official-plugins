# x — X（推特）读写与事件触发插件

发推/推串/媒体、点赞转推收藏关注、搜索与时间线、私信、列表，共 **22 个操作**；
外加 **2 个事件**（被提及 / 关键词命中）作为工作流触发源。

面向用户的说明书是 [docs/x.md](docs/x.md)（随握手上报，界面上「使用说明」看到的就是它）。
本文件是给改这个插件的人看的：为什么长这样、坑在哪、接入点有哪些。

## 设计判断

对标的是 n8n 的 X 节点（Make 于 2025-04 下架原生 X 模块，Zapier 2023 年即废弃，
能抄的只剩这一家）。它有 8 个动作：发推/回复、删推、搜索、点赞、转推、发私信、查用户、
加列表成员。以下五条是**在它之上的取舍**，不是设计偏好：

1. **读按平台的增量流形状，不照抄 X 的分页。** X 的翻页是 `next_token`——一小时失效，
   且只在一次搜索会话内有效。工作流是「每小时跑一次、接着上次往下拉」，把 `next_token`
   存进数据表下次必然报错。所以对外是 `cursor`(since_id) 进、`next_cursor`/`has_more` 出，
   与其它增量流插件一个形状；`next_token` 只在一次调用内部自动翻页时用。
   **空结果原样返回游标**，不冲掉进度。

2. **转推与回复要能一眼分掉。** 搜索结果里大半是转推，而 X 不给「这是什么」这个字段。
   `kind`（original / replied_to / retweeted / quoted）由 `referenced_tweets` 推出来，
   否则下游只能靠 `"RT @"` 前缀猜。

3. **推串是一个操作。** X 没有推串接口，靠逐条 `in_reply_to` 串。摆在画布上就是 N 个节点
   + N 条连线，改一次文案动 N 处，断了也没人记得断在哪。中途失败**不回滚**（前面几条已经
   在公开时间线上了），但已发出的 id 会同时出现在错误里和出参 `ids` 里。

4. **媒体上传独立成操作。** 它是 initialize/append/finalize 三段式 + 转码轮询，塞进发推里
   会让「发一条纯文本」也背上分片逻辑。拆开后画布上是显式一步，失败可单独重试。

5. **归一化在插件里做完。** X 把作者、媒体、被引用的推文都堆在 `includes` 里靠下标关联，
   画布上引用 `includes.users[3].username` 是不可能稳定的。出去的每条推文自带
   `author_username`、展开后的媒体、以及**展开成原地址的外链**（正文里是 t.co 短链，下游用不了）。

## 文件

| 文件 | 干什么 |
|---|---|
| `schema/schema.go` | 操作/事件/凭证契约声明（**事实源**，改完 `go generate`） |
| `schema/types.go` | 出参元素形状（Post / User / Media / DMEvent） |
| `client.go` | 认证、代理、429 退避、错误翻译、读接口公共参数 |
| `normalize.go` | X 应答 → 契约形状（includes 关联、kind 推断、链接展开） |
| `post.go` | 发推 / 推串 / 删推 |
| `read.go` | 搜索 / 时间线 / 提及 / 列表 / 批量取 / 查用户 + `fetchPosts` 公共分页 |
| `engage.go` | 赞 / 转推 / 书签 / 关注 各带取消 + 列表成员 |
| `media.go` | 分片上传 + 等转码 + 替代文本 |
| `dm.go` | 发私信 / 拉私信 |
| `watch.go` | 事件源：轮询提及与关键词 |
| `zz_*.go` | sokel-gen 生成，别手改 |

## 认证：只有 OAuth 一条路

X 的写操作必须是用户身份，`client_secret` 在平台手里，所以没有「填个密钥就能用」的路。
凭证里的 `access_token` 由平台注入（插件不碰 `refresh_token`）。

作用域**一次要齐**（X 不支持增量授权，少一个就得让所有人重新授权一遍），见
`schema.Credential.AuthMeta`。`offline.access` 是命门：没有它就没有刷新令牌，
access_token 两小时后失效且无法自愈。

平台侧为 X 加了两处通用能力（`server/internal/credential/`）：

- **PKCE**（X 强制）：`code_verifier` 不存也不进 URL——state 里带 nonce，verifier 由
  `JWT.DeriveKey("oauth-pkce:"+nonce)` 两边各算一遍（`oauth.go::pkceFor`）。
- **refresh_token 轮转**（X 每刷新一次就换一个、旧的当场作废）：`preauth.go::enrichOAuth`
  把新的落库，并按 refresh 哈希加逐键串行锁。不做的话症状是「授权当天好好的，
  两小时后全线 401」。

## 事件源

X 的实时推送（filtered stream / Account Activity）只在 Enterprise 档，自助档拿不到，
所以是轮询。**盯什么在凭证里配**（事件源是常驻进程，没有节点配置）：
`watch_mentions`(on/off) / `watch_query`(X 搜索语法) / `poll_seconds`(默认 300，下限 60)。

四条规矩，前三条是 notion/gmail 那两个源踩出来的：

- **首次启动不推历史**（接一个用了十年的账号会瞬间冲垮工作流），只记当前位置；
- **游标写回凭证**（`watch_cursor`，两条流各一个），否则重启要么重推全部要么漏掉停机期；
- **推失败就地停下**，游标不前进，下一轮重来（宁可重复不丢，平台侧有去重）；
- **间隔有下限**——X 的读按条计费，一个手滑的 10 秒轮询就是一天几万条的账单。

⚠️ **单副本**：多副本会各自推进同一个游标，导致重复推送与游标来回跳。

## 花钱（2026-02 起 X 取消免费档）

读一条 ≈ $0.005、发一条 ≈ $0.015（带链接 ≈ $0.20）、查用户 ≈ $0.010。因此：
`me()` 按 access_token 缓存（每个操作都查一次是纯浪费）、读操作的「最多几条」默认 50、
事件源间隔默认 300 秒。402（额度不足）会被翻译成人话报出来。

## 部署

```bash
# 1) 平台：插件页装上 X → 建 nats 接入组 → 复制 skp_ token
# 2) 起副本（本目录的 docker-compose.yml；SOKEL_ENDPOINT / SOKEL_TOKEN 从环境取）
docker compose up -d
# 3) 平台环境变量（换 token / 刷 token 走它）
#    X_OAUTH_CLIENT_ID / X_OAUTH_CLIENT_SECRET / X_OAUTH_REDIRECT_URL / X_OAUTH_PROXY
# 4) 建凭证 → 点「授权」；连不上 x.com 就在**凭证**里填出站代理
```

**代理两处、值不一样**：平台跑在宿主上用 `http://127.0.0.1:7897`；插件跑在容器里，
凭证里要填 `http://host.docker.internal:7897`（容器里的 127.0.0.1 是它自己）。
插件的代理按凭证配而不是给容器设 `HTTP_PROXY`——后者是全局的，会把连 broker 的流量一起带偏。

## 开发

```bash
go generate ./...          # 改了 schema 之后必跑
go build ./... && go vet ./... && go test -race ./...
```

测试用假上游（`x_test.go`），钉的是**别处看不出来的那几件事**：归一化、游标语义、
推串串接与中断、媒体片序、`x-rate-limit-reset` 是绝对时间戳而非秒数、403 的三种成因。

改契约后除了本目录，还要看这几个接入点（详见 `docs/dev-playbook.md` §4.4 / §5）：
`server/internal/api/seed.go::seedFirstPartyPlugins`（目录行）、
`internal/api/plugin_test.go::TestListPluginsSeeded`（计数）、
本目录的 `docker-compose.yml`（部署）。

## 没做的东西

- **引用转推**接口做了，但 X 把它划进了 Enterprise 档，自助档调用会 403——契约里写明了。
- **全量存档搜索**（`/tweets/search/all`）同样是 Enterprise，只做了最近 7 天的 recent。
- **Spaces / Communities / Trends / 分析数据**：暂时没有工作流场景，要用再加。
- **粉丝列表 / 关注列表的批量拉取**：按条计费下这类操作一次就是几十美元，
  等有明确场景再说。
