# github 插件

> 给**改这个插件的人**看。用户看的是 `docs/github.md`（随注册握手上报，界面「使用说明」显示它）。

GitHub 项目维护自动化，43 个操作 + 10 个事件。对标的是**各类 GitHub 机器人**
（Probot 系、Renovate、stale-bot、reviewdog、Mergify、release-drafter、ChatOps），
验收标准是「能不能拿这套操作把那些机器人重搭一遍」，而不是「REST 接口包全了没有」。

## 为什么长这样

每条都对应一个真实的坑，动之前先读。

### 1. `/issues` 会把 PR 一起返回，所以默认剔掉

GitHub 的 Issue 与 PR 共用编号空间，`GET /issues` **连 PR 一起返回**，而且不报错。
照抄 GitLab 的心智去列 Issue，会拿到一堆 PR 混在里面。

`IssuesList` 默认剔掉（`include_prs` 显式打开），出参给 `dropped_prs` 让人看得见剔了几条，
每条 Issue 还带 `is_pr`。判据是 payload 里有没有 `pull_request` 这个键——
不是看标题、不是看 URL（`isPullRequest`）。

⚠️ 剔除发生在**分页之后**，所以 `count` 是剔完的条数而 `has_more` 是上游的。
这不是 bug：翻页要看上游有没有下一页。

### 2. Webhook 验签是 HMAC-SHA256，不是明文比对

GitLab 把 secret 明文放在头里比对，GitHub 用 **HMAC-SHA256 签整个原始请求体**
（`X-Hub-Signature-256: sha256=<hex>`）。照抄 GitLab 那份会永远验不过。

三条不能松的：

- 比对用 `hmac.Equal`（常数时间），不是 `==`；
- 签的必须是**原始字节**——对 body 做过一次 JSON 解码再编码，签名就对不上了；
- 配了 secret 但请求没带签名 → **拒**。放行等于假装验过了。

### 3. `event_id` 直接用 `X-GitHub-Delivery`

GitHub 给每次投递一个 UUID，**重投时沿用同一个**——正好就是平台去重要的语义。
不用自己按「对象 id + 时间戳」拼一个脆弱的键（GitLab 那边就得那么干）。

### 4. 写文件要先探一次拿 blob sha

Contents API 更新已存在的文件时不带 `sha` 会 422，而错误文字不会说「你少给了 sha」。
`opFileWrite` 内部 get-then-put，多一次往返换掉一整类让人摸不着头脑的失败。

### 5. 403 有两种完全不同的含义

权限不够和被限流都是 403。区分靠 `X-RateLimit-Remaining` 为不为 0。
混为一谈的话，用户会拿着「权限不够」去反复检查令牌 scope，而其实只要等几分钟——
这是接 GitHub 最费时间的一个误导。`ghErr` 里分开了，还单独认了「二级速率限制」。

### 6. 机器人回执面是这个插件的重点

`commit_status_create` / `check_run_create` / `reaction_add` 这三个是
「GitHub 机器人」区别于「GitHub API 客户端」的地方：机器人要能把结论**写回** PR 页面。
没有它们的话，评审类流程只能发一条评论，而评论不参与分支保护的必需检查。

### 7. Projects V2 只有 GraphQL

经典 Projects 的 REST 接口已下线。看板四个操作走 GraphQL，其余走 REST，
分界线在 `client.go`，契约上看不出来（也不该看出来——调用方不关心传输）。

两个 GraphQL 特有的坑：

- **组织与个人是两个不同的查询**，认错**不报错**，返回 null 数据，表现成「这个看板是空的」。
  所以 `is_org` 留空时两种都试。
- **GraphQL 的错误不走 HTTP 状态码**：查询失败照样 200，错误在响应体的 `errors` 里。
  不解 `errors` 的话，一个拼错的字段名会表现成「返回了空数据」。
- GHES 的 GraphQL 端点是 `/api/graphql`，**不跟 REST 的 `/api/v3`**。照 REST 的形状拼会 404，
  而 404 看起来像「你没权限」，很难往「端点拼错了」上想。

### 8. 轮询源首轮不触发

活动流一次给最近 30 条。首轮照发的话，插件一启动就会把三十条陈年事件全推进工作流。
所以首轮只记游标（`primed`），从第二轮起才推。工作流失败那条路同理。

### 9. ChatOps 必须能认出机器人自己

`is_bot` 出现在评论类事件里。不挡的话机器人回复自己的评论会无限循环，
而且每轮一次 API 调用，几分钟就能把速率配额打光。
判据两个都要：`user.type == "Bot"` 是权威的，`login` 以 `[bot]` 结尾兜住 type 缺席的老 payload。


### 轮询路径为什么以前没有测试

不是疏忽，是签名。三个 poll 函数原先收 `sokel.SourceCtx`——那是个**结构体**，
`Trigger` 假不出来，于是整条轮询路径一行测不到。webhook 有测试纯属运气：
`sokel.WebhookCtx` 恰好是 `plugin.SourceCtx` 的**别名**，是接口。

现在三个 poll 函数都收接口 `plugin.SourceCtx`（`runEvents` 仍收结构体，那是
`RegisterSource` 的要求）。**新写事件源时照这个来**：顶层收结构体，干活的函数收接口。

轮询这条路上七个「不报错但没用」的点已装齿：首轮不推、旧→新的顺序、过滤已见过的、
事件 id 按数值比（进位时 `"9" > "10"` 会让轮询**永久哑掉**且不报错）、活动流 payload
形状（它与 webhook 的**不一样**）、失败工作流按 run_id+attempt 去重、没开 Actions 的
404 不能把整轮带崩。

## 文件

| 文件 | 是什么 |
|---|---|
| `schema/schema.go` | 顶注 + 复用字段 + 仓库域契约 |
| `schema/issue.go` | Issue 域契约 |
| `schema/pr.go` | PR 域 + 机器人回执面契约 |
| `schema/actions.go` | Actions + 发布 + 仓库家务契约 |
| `schema/projects.go` | 看板 + 搜索 + 保底 + 健康检查契约 |
| `schema/events.go` | 事件契约 + 凭证契约 |
| `zz_*.go` | **生成物，别手改**（`sokel-gen`） |
| `client.go` | REST + GraphQL 调用层、错误翻译、解析小工具 |
| `ops_*.go` | 各域 handler |
| `webhook.go` | 平台代收 webhook（验签 + 分发） |
| `events.go` | 轮询事件源 |
| `main.go` | 接线 |
| `docs/github.md` | **用户**看的说明书，`//go:embed` 进二进制随握手上报 |

## 开发

```bash
go generate ./...            # 改了 schema/ 之后必须跑
go build ./... && go vet ./...
go test ./...
```

改了 `schema/` 不重新生成，`sokel-gen check .` 会红——CI 拦在那里。

### 本地跑

```bash
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx go run .
```

## 对着真 GitHub 联调

假上游只能验我们自己写的装配逻辑。有四类东西它天然验不了，而这四类最容易错：
GraphQL 查询串写没写对（假上游根本不解析它，看板四个操作在假测里是**零覆盖**）、
`/issues` 到底会不会返回 PR、Link 头与限流头长什么样、令牌权限够不够。

### 1. API 那半：`live_test.go`

```bash
# 只读那批，任何令牌都能跑
GITHUB_TOKEN=ghp_xxx go test -run Live -v ./...

# 加上写操作（会真的建 Issue/分支/PR）——**务必指向一个一次性仓库**
GITHUB_TOKEN=ghp_xxx GITHUB_WRITE_REPO=you/scratch go test -run Live -v ./...

# 看板（令牌要有 project 权限）
GITHUB_TOKEN=ghp_xxx GITHUB_PROJECT_OWNER=you go test -run LiveProject -v ./...

# GHES
GITHUB_TOKEN=xxx GITHUB_BASE_URL=https://github.example.com go test -run Live -v ./...
```

没设 `GITHUB_TOKEN` 就整批跳过，CI 与别人机器上不会因此变红。

`TestLivePullRequestFlow` 会留下一个分支和一个文件（PR 会自动关掉），需要的话手动删。

### 2. Webhook 那半：`webhook-replay.sh`

webhook 走 HTTP 进平台，Go 单测覆盖不到；让真 GitHub 打到本机又要内网穿透。
这个脚本把「GitHub 发过来」换成本地 curl，**签名照真算**，所以验签、分发、去重三段都真的在跑：

```bash
./webhook-replay.sh http://localhost:8088/hooks/whk_xxx s3cret issues
```

一次打两发（正确签名 + 错误签名），然后去平台「插件详情 → Webhook」tab 对：
200 那条触发事件数非 0，401 那条为 0。再用同一个投递 id 打一次，应当被去重。

### 3. 端到端：真 GitHub → 真平台

前两步都过了再做这步，否则排查面太大。

```bash
# 让 GitHub 能打到本机（二选一）
cloudflared tunnel --url http://localhost:8088
```

拿到公网地址后，在 GitHub 仓库 Settings → Webhooks 里填 `<公网地址>/hooks/<平台给的 token>`，
Content type 选 `application/json`，Secret 与凭证里的一致。保存后 GitHub 立刻发一条 ping，
配置页显示绿勾就是通了；随后去仓库里开一个 Issue，看工作流有没有被触发。

**没有公网地址就用轮询源**：凭证填「事件盯哪些仓库」，等一分钟（首轮只记游标不补发历史），
再去仓库制造一个动作。

### 联调时优先看这几条

| 现象 | 多半是 |
|---|---|
| 所有 webhook 都 401 | 两边 secret 不一致；或 Content type 选成了 `x-www-form-urlencoded` |
| webhook 200 但工作流没动 | 事件类型没在 GitHub 那边勾上；或触发器绑的事件对不上 |
| 同一个动作触发两次 | webhook 与轮询源同时开着（文档说的二选一） |
| 轮询源配好了没反应 | 首轮只记游标——等一分钟后再制造一个新动作 |
| 回写的状态在 PR 页面上看不到 | sha 用错了，要用 PR 的 `head_sha` 而不是合并提交 |
| 看板操作回「空列表」而不是报错 | 组织/个人认错了（把「是组织」显式打开试试） |

## 改契约之后还要动哪些地方

1. `go generate ./...` 重新生成 `zz_*.go`；
2. 加了**操作** → `main.go` 里加一行 `OnXxx(p, opXxx)`，否则契约上有、调用时说找不到；
3. 加了**事件** → 两条来路都要发：`webhook.go` 与 `events.go`。
   只加一条的症状是「配了 webhook 能触发，改用轮询就不触发了」，反之亦然；
4. 改了用户能看见的行为 → 同步 `docs/github.md`；
5. 平台侧那一行简介在 `server/internal/api/seed.go`，图标在
   `web/src/features/plugin/components/PluginIcon.tsx` 的 `BRANDS` 与 seed 的 `icons` 表。

## 没做的

- **GitHub App 身份**。现在只支持个人访问令牌。App 能拿到更高的速率配额、能建检查运行、
  还能以「应用」身份出现在 PR 上——但要处理 JWT 签名与 installation token 轮换，
  是另一个量级的工作。`check_run_create` 在经典 PAT 下会 403，错误信息里说明了这一点。
- **Discussions**。GraphQL only，形状与 Issue 差得远，暂时没有需求驱动。
- **组织级操作**（成员管理、团队、组织设置）。这个插件的定位是「维护项目」，不是「管组织」。
- **分支保护的写入**。只做了读（合规巡检够用）。写入的 payload 很大且一次写错就会
  把主分支的保护弄松，风险与收益不匹配——要改请走「通用调用」，那样是显式的。
- **轮询源的游标持久化**。游标在内存里，插件重启后第一轮重新 prime（即不补发）。
  重启期间发生的事件会漏——这是选择 webhook 那条路的理由之一，文档里写明了。
