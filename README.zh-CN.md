# Sokel 官方插件

[English](README.md) · 简体中文

Sokel 项目维护的集成插件：消息、社交、开发工具、云服务、数据源和存储引擎，供 [Sokel](https://github.com/sokel-dev)
工作流平台使用。每个目录是一个插件，用[插件 SDK](https://github.com/sokel-dev/sokel-plugin-sdk) 编写；
各插件的操作和凭证可以在[插件目录](https://sokel-dev.github.io/sokel-registry/)里浏览。

## 运行一个插件

在你平台的插件市场里安装插件，建一个接入组并复制它的 token，然后在能同时连到目标服务和你平台的机器上起一个副本：

```bash
docker run -d --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://sokel.example.com \
  -e SOKEL_TOKEN=skp_xxx \
  ghcr.io/sokel-dev/sokel-plugin-<名字>:latest
```

凭证（API Key、机器人 token）不在这里配，而是放在平台的凭证管理里。每个插件的 README 和 `docs/` 写了它需要什么。

## 插件列表

| 目录 | 名称 | 用途 |
|---|---|---|
| [`aliyun`](aliyun) | 阿里云 | 把阿里云管控面接进工作流：查 SLS 日志、看 RDS 状态与慢 SQL、改 DNS 解析、管 ACK 集群、查云监控指标。 |
| [`bluesky`](bluesky) | Bluesky | 发帖、发帖串、删帖。免费、无审批、无按次计费——是几个公开社交平台里接入成本最低的一个。 |
| [`claude-code`](claude-code) | Claude Code | 把本机装的 Claude Code 接进工作流：给它一个 GitLab 项目和一句任务， |
| [`discord`](discord) | Discord | 往频道发消息。免费、无审批，建一个 Webhook 只要频道管理员点几下。 |
| [`elasticsearch`](elasticsearch) | Elasticsearch | 把 ES 接进工作流：查日志与文档、灌数据、管索引。ES 7/8/9 与 OpenSearch 都支持—— |
| [`feed`](feed) | Feed 订阅 | 从一个来源拉新条目，产出统一形状的 JSON。换来源不用改下游节点。 |
| [`feishu-webhook`](feishu-webhook) | 飞书群机器人 | 往一个飞书群里发消息的最短路径：不用建应用、不用找管理员，30 秒配好。 |
| [`feishu`](feishu) | 飞书 | 把飞书接进工作流：发消息/卡片、收消息触发、写云文档、读写多维表格。 |
| [`github`](github) | GitHub | GitHub 项目维护自动化（github.com / GitHub Enterprise Server）：Issue 与 PR 全流程、仓库读写与建分支、Actions 触发与失败日志、发版、Projects V2 看板流转，外加机器人回执面（回写提交状态/检查运行/表情）——自动分类、stale 清理、PR 评审、ChatOps 都能拼出来。事件支持 Webhook（零延迟）与轮询两条路。**需自行部署插件进程**：安装后在本页接入组里复制接入命令。 |
| [`gitlab`](gitlab) | GitLab | 把 GitLab 接进工作流：读写仓库文件、建/评/合 MR、开 Issue、查流水线与失败日志、触发构建。 |
| [`gmail`](gmail) | Gmail | 读写你的 Gmail：列邮件 / 读邮件 / 发邮件，并可作为事件源——收到新邮件时触发工作流。 |
| [`hackernews`](hackernews) | Hacker News | 读榜单、帖子、评论，按关键词搜索；有人评论你的帖子、或回复你的评论时触发工作流。 |
| [`kbstore-es`](kbstore-es) | 知识库存储（Elasticsearch） | 知识库的存储与检索后端：写入分块、向量检索、BM25、混合检索（RRF 融合）。 |
| [`kbstore-pgvector`](kbstore-pgvector) | 知识库存储（pgvector） | 知识库存储引擎插件（Postgres + pgvector），与 kbstore-es 实现同一份存储契约。 |
| [`kubernetes`](kubernetes) | Kubernetes | 在画布上操作 K8s 集群：查 pod、看容器日志、滚动重启、扩缩副本、看事件与节点健康。 |
| [`linkedin`](linkedin) | LinkedIn | 以授权账号的个人身份发动态。自助接入、无需合作伙伴审批，适合投研观点触达专业受众。 |
| [`mastodon`](mastodon) | Mastodon | 发嘟文、发嘟文串、删嘟文。免费、无审批、无按次计费，且因为是联邦网络， |
| [`notion`](notion) | Notion | 读写 Notion：搜索、查表、建页、改正文（markdown）、评论、文件，并可作为事件源—— |
| [`redis`](redis) | Redis | 把 Redis 当工作流的共享内存：跨运行的计数器、去重集合、状态标记、队列、排行榜， |
| [`submail`](submail) | 赛邮短信 | 通过 SUBMAIL（赛邮云通信）发国内短信与国际短信：告警通知、验证码、运营触达。 |
| [`synology`](synology) | 群晖 NAS | 监听 NAS 目录里的文件变动并触发工作流（落定检测已内置）。**需自行部署插件进程**，且必须部署在能访问该 NAS 的机器上。 |
| [`telegram-bot`](telegram-bot) | Telegram Bot | 把 Telegram 机器人接进工作流：收消息触发、发消息/图片/文件、下载用户发来的附件，也可以直调任意 Bot API 方法。 |
| [`threads`](threads) | Threads | 发帖、发帖串。免费、无按次计费；受众规模是这批海外渠道里最大的之一。 |
| [`tushare`](tushare) | Tushare Pro | 券商研报增量 + 221 个 TuShare 目录接口。只负责取数——不落库、不去重、不加工。 |
| [`umeng`](umeng) | 友盟推送 | App 推送的友盟通道：按设备 token 单播/列播，或全量广播。与「阿里云」插件的 |
| [`wechat-claw`](wechat-claw) | 微信 ClawBot | 接入微信官方 ClawBot（iLink Bot 接口）：你在微信里给 ClawBot 发消息即触发工作流，工作流可回复文本、图片、文件。只有扫码绑定的本人能与它对话。 |
| [`wechat-mp`](wechat-mp) | 微信公众号 | 把文章写进草稿箱、发布、拿到永久链接。国内唯一一条「API 正经、金融内容可做、覆盖面够」的主渠道。 |
| [`x`](x) | X (Twitter) | 发推、发推串、传图传视频、点赞转推、搜索与时间线、私信、列表，以及被提及 / 关键词命中时触发工作流。 |
| [`xueqiu`](xueqiu) | 雪球（非官方） | 发帖（可带图）+ 检查凭证。 |
| [`youtube-transcript`](youtube-transcript) | YouTube 字幕 | 取任意公开 YouTube 视频的字幕，产出带时间轴的分句和拼好的全文。不需要 YouTube 账号，也不需要 API … |

## 构建

```bash
docker build --build-arg PLUGIN=telegram-bot -t sokel-plugin-telegram-bot .   # 所有插件共用一份镜像配方
cd telegram-bot && go generate ./... && go test ./...                         # 改了插件 schema 之后
```

每次推送到 `main`，Images 流水线都会构建 linux/amd64 和 linux/arm64 镜像并发布到 ghcr（`:latest` 和 `:sha-<提交>`）。

## 发版

给 `main` 上的提交打 `<插件>/vX.Y.Z` 标签就是发版：

```bash
git tag discord/v1.0.1 && git push origin discord/v1.0.1
```

Release 流水线随即构建 `ghcr.io/sokel-dev/sokel-plugin-discord:1.0.1`，重写它在[插件目录](https://github.com/sokel-dev/sokel-registry)里的条目
（契约取自 `sokel-gen export`、`plugin.version`、按摘要钉死的镜像），用目录自己的准入检查和版本门校验，到那边开 PR，并在本仓发一个 GitHub release。
合并那个 PR，新版本就上了目录页。版本号必须比条目现有的高；插件的第一个条目要手工建（见目录仓的 CONTRIBUTING）。

## 参与贡献

欢迎提 PR 修 bug 和改进。这些插件在 Sokel 项目里维护、同步到这里，合并的改动由维护者带回去。
你自己的新插件不必放在这里：在你自己的仓库发布，再加进[插件目录](https://github.com/sokel-dev/sokel-registry)即可。

## 许可

[Apache-2.0](LICENSE)。
