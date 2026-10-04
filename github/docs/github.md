# GitHub

把 GitHub 的项目维护搬进工作流：**各类 GitHub 机器人能做的事，这里都能拼出来**——
自动打标签分类、stale 清理、PR 评审回执、ChatOps 斜杠命令、发版通知、看板流转。
github.com 与 GitHub Enterprise Server 都支持。

## 凭证怎么配

### 访问令牌（必填）

GitHub 头像 → **Settings → Developer settings → Personal access tokens**。两种令牌都行：

| 令牌类型 | 勾什么 | 说明 |
|---|---|---|
| **经典令牌**（classic） | `repo`（私有仓库必需）、`workflow`（要触发 Actions）、`project`（要用看板） | 一把钥匙开所有仓库，最省事 |
| **细粒度令牌**（fine-grained） | 按仓库授权，至少给 Contents、Issues、Pull requests 的读写 | 更安全；**要回写检查结果还得加 Commit statuses 与 Checks**，要用看板加 Projects |

> 建议专门建一个机器人账号来发令牌。机器人以谁的身份说话，评论区就显示谁——
> 用自己的账号，同事会以为是你半夜在回 Issue。

⚠️ **细粒度令牌不会回报自己的权限范围**。所以凭证测试里「令牌权限范围」是空的属于正常，
不代表没权限。

### GitHub Enterprise Server

「GitHub 地址」填实例根地址，如 `https://github.example.com`——**不用带 `/api/v3`**，插件会自己补。
（填了也没关系，会去重。）

### Webhook Secret（可选，配 Webhook 才要）

见下一节。

### 事件盯哪些仓库（可选，用轮询才要）

见下一节。

## 事件：两条路，二选一

想让「有人开了 Issue」这类事情触发工作流，有两条路。**只选一条**——两条都开，
同一个动作会各触发一次。

### 路一：Webhook（零延迟，推荐）

1. 在平台的凭证行上点「Webhook」，复制生成的地址；
2. GitHub 仓库 → **Settings → Webhooks → Add webhook**：
   - **Payload URL**：粘贴刚才那个地址
   - **Content type**：选 `application/json`（**不是** `x-www-form-urlencoded`，选错了插件解不出内容）
   - **Secret**：自己想一串，**同一串填回平台凭证的「Webhook Secret」**
   - **Which events**：选 "Let me select individual events"，勾上你要的（见下表）
3. 保存后 GitHub 会立刻发一条 ping，配置页上那一条显示绿勾就是通了。

需要勾哪些事件：

| 想要的事件 | 在 GitHub 勾 |
|---|---|
| 有新提交 | Pushes |
| 新 PR / PR 被合并 / 给 PR 打标签 | Pull requests |
| PR 有评审 | Pull request reviews |
| 新 Issue / Issue 被打标签 | Issues |
| Issue 或 PR 有新评论 | Issue comments |
| 有新发布 | Releases |
| 工作流失败 | Workflow runs |

> **Secret 一定要填**。不填的话任何人只要知道那个地址就能往你的工作流里灌假事件。
> 平台侧凭证没配 Secret 时插件会跳过验签——这是为内网自建留的口子，公网仓库别这么用。

### 路二：轮询（不用碰 GitHub 的设置）

凭证里填「事件盯哪些仓库」，如 `sokel-dev/sokel,acme/infra`（逗号分隔）。填了才启动。

- 延迟约 1 分钟（GitHub 的活动流接口本身有约 60 秒缓存，轮更快也没用）；
- **插件刚启动时不会补发历史事件**——第一轮只记录游标，从第二轮起才推。
  所以刚配好之后要等一分钟，再去 GitHub 上制造一个动作来验证；
- 适合：没有仓库管理员权限、或者仓库在内网 GitHub 上不方便回调平台。

## 能拼出哪些机器人

| 想做的事 | 用什么 |
|---|---|
| **自动分类** 新 Issue 打标签、指派、进看板 | 触发器「新 Issue」→ 打标签 / 指派 / 加进看板 |
| **stale 清理** 很久没动的 Issue 先警告再关 | 定时 → Issue 列表（按 updated 正序）→ 发评论 → 改 Issue（关闭，原因选 `not_planned`） |
| **PR 评审回执** 跑检查然后把结论写回 PR 页面 | 触发器「新 PR」→ PR 改了哪些文件 → 你的检查 → 回写提交状态（用事件里的 `head_sha`） |
| **行内评审意见** | 提交评审，`comments` 填文件+行号 |
| **ChatOps** 在评论里打 `/deploy` | 触发器「PR 有新评论」→ 判 `author_association` 与 `is_bot` → 加表情 👀 → 干活 → 加表情 🚀 |
| **自动合并** 检查过了就合 | 触发器「PR 有评审」→ 判条件 → 合 PR（**`expect_sha` 填 `head_sha`**） |
| **开 PR 的机器人**（改依赖、回合补丁） | 建分支 → 写文件 → 开 PR → 请人评审 |
| **发版通知** | 触发器「有新发布」→ 发消息 |
| **CI 看门狗** | 触发器「工作流失败」→ 运行的作业 → 作业日志 → 开 Issue 或通知 |

## 几个会让人卡住的地方

- **Issue 列表里混着 PR**。GitHub 的 Issue 接口本来就会把 PR 一起返回。这个插件**默认帮你剔掉**了
  （出参 `dropped_prs` 告诉你剔了几条）；要连 PR 一起要，打开「把 PR 也算进来」。
- **`closed` 不等于 `merged`**。关掉不合也是 closed。所以「PR 被合并」是单独一个事件，
  别拿「PR 关闭」去代替。
- **给 PR 发普通评论用「发评论」**，和 Issue 是同一个操作——GitHub 两者共用评论接口。
  只有**针对 diff 某一行**的评论才走「提交评审」。
- **行内评论的行号必须落在这次 diff 动过的行上**，否则整条评审被拒（不是那一条被忽略）。
  拿不准就先用「PR 改了哪些文件」打开 `with_patch` 看看 diff 里有哪些行。
- **回写提交状态要用 PR 的 `head_sha`**，不是合并提交的 sha——写错地方的状态不会出现在 PR 页面上。
- **「建检查运行」需要 GitHub App 或带 Checks 权限的细粒度令牌**，经典 PAT 建不了。
  经典令牌请改用「回写提交状态」，效果类似（少了注解和 markdown 摘要）。
- **触发工作流拿不到运行 ID**。GitHub 这个接口回空响应，只能隔几秒去查「工作流运行记录」。
- **ChatOps 一定要判 `is_bot`**，否则机器人会回复自己的评论，转起来几分钟就能把速率配额打光。
  顺便判一下 `author_association`（`OWNER`/`MEMBER`/`COLLABORATOR`），
  不判的话任何路人都能用斜杠命令指挥你的机器人。
- **看板要的是账号名，不是仓库名**。Projects V2 挂在用户或组织下。
  组织的看板要把「是组织」打开（留空的话插件会两种都试一次，多一次请求而已）。

## 限额

认证请求每小时 **5000 次**（GHES 由管理员配置）。凭证测试会回报当前余量。

被限流时插件会明确说是限流、还要等多久——**这与「权限不够」是两回事**，都是 403，
但前者等一会儿就好，后者要去改令牌权限。

短时间内大量写操作还会触发「二级速率限制」，这时插件会提示你在节点之间加等待。
