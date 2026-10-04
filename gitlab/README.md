# gitlab — GitLab 插件（第一方内置目录）

18 个操作 + 5 类轮询事件覆盖四个域：仓库（读写文件/分支/提交）、MR（建/查/评/合）、Issue、
CI/CD（流水线/触发/Job 日志）+ `call` 保底 + `health_check`（/user）。
自建 CE 与 gitlab.com 通吃（凭证里 base_url）。说明书 [docs/gitlab.md](docs/gitlab.md)。

## 事件源（events.go）

轮询模式（GitLab 无长轮询/推流）：Events API + 失败流水线各一个游标，60s 间隔，
范围=凭证 watch_projects 点名的项目。**「MR 被合并」在 Events API 里是
action=accepted 不是 merged**——按直觉找 merged 一个事件都收不到，测试钉着。
失败流水线不在 Events API 里（GitLab 的空白），单独按 updated_after 轮询。
首轮只记位置不回溯；重启后的短暂重报交平台按 event_id 去重。

## Webhook（webhook.go）

平台代收 webhook 的零延迟版事件（与轮询源**二选一**，同一动作两路都开会各触发一次）：
用户在凭证行生成 `/hooks/{token}` URL 配进 GitLab，平台按 token 转发 `__webhook__`
帧到 `handleWebhook`。要点：

- **验签是明文比对**：GitLab 把 Secret token 原样放 `X-Gitlab-Token` 头（不是 HMAC），
  与凭证 `webhook_secret` 比对；凭证没配则跳过（内网自建可接受，说明书写明）。
- **事件映射**：Push/Merge Request/Issue/Pipeline 四类 hook → 既有五事件契约；
  MR 只认 action=open/merge，Pipeline 只认 status=failed。
- **event_id 用 `wh:` 前缀**与轮询源分域——两边的对象 id 不同域，硬拼映射会脆。
- **认不出的事件类型回 200**：GitLab 按项目配置会发一堆类型，非 2xx 会让它反复重试。

## issue_commented：三道过滤都是实测定下来的

对着**真实 GitLab 的 /events** 抓了一次，三个坑没有一个能靠读代码想出来：

| 想当然 | 实况 |
|---|---|
| `target_iid` 是 Issue 编号 | 是**评论自己的 id**（实测 1970，而 Issue 是 #4）。编号在 `note.noteable_iid` |
| Note 事件就是 Issue 评论 | Issue 与 MR 的评论走**同一种**事件，靠 `note.noteable_type` 分 |
| note 都是人写的 | 改标签、指派也会生成 note（`note.system=true`）——不挡住就是「动一下标签就派一次活」，直接烧钱 |

webhook 侧同样要挡 `system`。夹具用的就是实测那份形状。

## issue_commented：会成环，事件必须给足防环的料

机器人回复 Issue 也是一条评论 → 又触发一次 → 无限循环。插件侧不该硬编码"哪个账号是机器人"
（那是部署问题），所以事件必须把 `author` 与 `comment` **原文**给全，让工作流自己滤。
文档里把两道防线写成必须项：滤作者 + 认前缀。

只认 `noteable_type == "Issue"` 的评论：MR / 提交 / 代码行评论走同一个 Note Hook,
不区分的话「在 MR 里说句话」也会去派 Issue 的活（测试钉着）。轮询侧 Events API 记成
`target_type=Note` + `action_name="commented on"`，正文在 `note.body`。

## issue_labeled：为什么必须单开一个事件

加标签在 GitLab 那边是 `action: "update"`，不是 open——`issue_opened` 再也不会响，
于是「先建 Issue、看一眼再决定派不派活」这条**最自然的用法整个是死的**（用户实报）。

两条路填法不同：**webhook** 从 `changes.labels.previous/current` 求差集（摘标签同样是
update，必须只回真新增，否则摘个标签也会派活）；**轮询** Events API 根本报不出标签变更
（它只有 opened/closed 那几个 action），只能单独轮 issues 列表 + 在内存里比对上一轮的标签，
首轮只记位置。`added_labels` 排过序：它拼进 event_id，抖动会破坏去重。

## Issue 事件的正文/标签

`issue_opened` 除了 iid/title/author，还带 `description`（正文）、`labels`、`url`——
「照着 Issue 干活」的下游要的是正文，而标签是「谁能触发」的唯一实用闸门。
两条路填法不同：**webhook** 直接从 `object_attributes` 取（标签在 payload 里有两处、
形状还不一样，`hookLabels` 两处都试）；**轮询**的 Events API 只给标题，
要多取一次 `/issues/:iid` 详情——取失败只让正文为空，**不能让事件本身发不出去**。

## raw 归一不了，但要让下游分得清

`raw` 在两条路上形状**本来就不一样**：轮询是 Events API 的事件对象，webhook 是 Hook 请求体。
这两份上游载荷没法归一（真去归一就得为每条事件多打一次 API）。

所以做法是**停止假装它们一样**：每条带 `raw` 的事件都必填 `source`（`poll` / `webhook`），
字段说明直说「要下钻 raw 就必须先看 source」。测试钉着两条路都不能漏填——
漏一条的后果是同一个 `{{raw.xxx}}` 换一种部署方式就取到空，而且不报错。

**优先用已归一的字段**（description / labels / iid / author…），raw 是逃生口不是主路。

## 列表的分页位

六个列表操作（项目/分支/提交/MR/Issue/流水线）都回 `total` 与 `has_more`，取自 GitLab 的
`X-Total` / `X-Next-Page` 响应头——glCall 一直在返回 header，只是从前没人读。

只回「本页条数」是**静默截断**：正好 50 条时调用方无从知道是刚好这么多还是被截了。
**判翻页要看 `has_more` 不看 `total`**：超大结果集上 GitLab 会省略 `X-Total`（算总数太贵），
那时 total=0 而 has_more 仍为 true——看 total 会以为一条都没有。测试钉着这两种形态。

## 坑（改代码前先读）

- **project 双形态**：数字 ID 原样、路径要 `url.PathEscape`（`backend/server` →
  `backend%2Fserver`）进路径段。
- **文件路径整体编码**（含 `/` → `%2F`）——files 接口的约定，`pathEscapeAll`。
- **GitLab 对无权限的项目回 404 不回 403**（防探测）——404 的报错必须提这一句，
  否则用户会一直核对路径。测试钉着。
- **写文件走 commits 接口**（不是 files 接口）：先探文件在不在定 action
  （create/update），一次提交带提交信息；多文件走 call。
- **pipeline 变量是 `[{key,value}]` 数组**不是对象——直接传对象 GitLab 回 400。
- 应答体上限 32MB（job 日志可能很大），日志尾部截断在插件侧做。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
