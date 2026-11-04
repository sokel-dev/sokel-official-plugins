# Sokel 官方插件

[English](README.md) · 简体中文

Sokel 项目维护的集成插件：消息、社交、开发工具、云服务、数据源和存储引擎，供 [Sokel](https://github.com/sokel-dev)
工作流平台使用。每个目录是一个插件，用[插件 SDK](https://github.com/sokel-dev/sokel-plugin-sdk) 编写；
已发布版本的操作和凭证可以在[插件目录](https://sokel-dev.github.io/sokel-registry/)里浏览。

## 运行一个插件

以下市场和镜像步骤适用于已完成发布的插件；尚待首次发布的插件可先从本仓库源码构建。

在你平台的插件市场里安装插件，建一个接入组并复制它的 token，然后在能同时连到目标服务和你平台的机器上起一个副本：

```bash
docker run -d --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://sokel.example.com \
  -e SOKEL_TOKEN=skp_xxx \
  ghcr.io/sokel-dev/sokel-plugin-<名字>:latest
```

凭证（API Key、机器人 token）不在这里配，而是放在平台的凭证管理里。每个插件的 README 和 `docs/` 写了它需要什么。

## 插件列表

本次源码更新新增 **68 个集成、491 个操作和 9 个事件**，尚待首次镜像发布和目录条目上线。表格列出全部 104 个集成源码，并不表示所有集成都已可从目录安装。源码验证和临时目录 fixture 已完成；正式上线仍需要实际构建的镜像摘要和发布流水线。

| 目录 | 名称 | 用途 | 发布状态 |
|---|---|---|---|
| [`airtable`](airtable) | Airtable 数据表 | 数据库结构、记录读写与变化轮询 | 待首次发布 |
| [`aliyun`](aliyun) | 阿里云 | 把阿里云管控面接进工作流：查 SLS 日志、看 RDS 状态与慢 SQL、改 DNS 解析、管 ACK 集群、查云监控指标。 | 既有集成 |
| [`amap`](amap) | 高德地图 | 地址解析、地点搜索、路线、天气和行政区划。 | 待首次发布 |
| [`apache-tika`](apache-tika) | Apache Tika | 通过自建 Apache Tika 4.2 服务提取文档文本、元数据和嵌入内容 | 待首次发布 |
| [`apify`](apify) | Apify | 异步 Actor 运行与数据集分页访问 | 待首次发布 |
| [`asana`](asana) | Asana | 浏览项目并管理任务和评论。 | 待首次发布 |
| [`baserow`](baserow) | Baserow | 发现表并管理自托管 Baserow 服务中的动态行 | 待首次发布 |
| [`bluesky`](bluesky) | Bluesky | 发帖、发帖串、删帖。免费、无审批、无按次计费——是几个公开社交平台里接入成本最低的一个。 | 既有集成 |
| [`bocha-search`](bocha-search) | 博查搜索 | 按时效过滤搜索网页，并可选择生成摘要。 | 待首次发布 |
| [`bookstack`](bookstack) | BookStack | 在自建 BookStack 实例上读取、搜索和维护知识页面。 | 待首次发布 |
| [`box`](box) | Box | 通过显式分页读取、搜索和传输 Box 文件 | 待首次发布 |
| [`brevo`](brevo) | Brevo | 管理联系人及列表，并显式发送事务邮件 | 待首次发布 |
| [`browser-use`](browser-use) | Browser Use 云服务 | 运行并查询 Browser Use V4 异步智能体 | 待首次发布 |
| [`calendly`](calendly) | Calendly | 读取 Calendly 活动与可用时间并创建单次预约链接 | 待首次发布 |
| [`claude-code`](claude-code) | Claude Code | 把本机装的 Claude Code 接进工作流：给它一个 GitLab 项目和一句任务， | 既有集成 |
| [`clickup`](clickup) | ClickUp | 发现工作区并管理任务和评论。 | 待首次发布 |
| [`confluence`](confluence) | Confluence Cloud | 读取空间并发布经过版本校验的 Confluence Cloud 页面 | 待首次发布 |
| [`crawl4ai`](crawl4ai) | Crawl4AI | 通过自托管 Crawl4AI 服务器抓取页面并提取 Markdown | 待首次发布 |
| [`dingtalk`](dingtalk) | 钉钉 | 钉钉 | 待首次发布 |
| [`discord`](discord) | Discord | 往频道发消息。免费、无审批，建一个 Webhook 只要频道管理员点几下。 | 既有集成 |
| [`dropbox`](dropbox) | Dropbox | 通过显式游标读取、搜索和传输 Dropbox 文件 | 待首次发布 |
| [`elasticsearch`](elasticsearch) | Elasticsearch | 把 ES 接进工作流：查日志与文档、灌数据、管索引。ES 7/8/9 与 OpenSearch 都支持—— | 既有集成 |
| [`exa`](exa) | Exa | 网页搜索、带引用回答和异步研究 | 待首次发布 |
| [`feed`](feed) | Feed 订阅 | 从一个来源拉新条目，产出统一形状的 JSON。换来源不用改下游节点。 | 既有集成 |
| [`feishu-webhook`](feishu-webhook) | 飞书群机器人 | 往一个飞书群里发消息的最短路径：不用建应用、不用找管理员，30 秒配好。 | 既有集成 |
| [`feishu`](feishu) | 飞书 | 把飞书接进工作流：发消息/卡片、收消息触发、写云文档、读写多维表格。 | 既有集成 |
| [`figma`](figma) | Figma | 读取设计元数据、渲染图片并管理评论 | 待首次发布 |
| [`firecrawl`](firecrawl) | Firecrawl | 网页抓取、网站遍历与结构化提取 | 待首次发布 |
| [`gitea`](gitea) | Gitea | 在自建 Gitea 实例上操作仓库、议题、评论和拉取请求。 | 待首次发布 |
| [`github`](github) | GitHub | GitHub 项目维护自动化（github.com / GitHub Enterprise Server）：Issue 与 PR 全流程、仓库读写与建分支、Actions 触发与失败日志、发版、Projects V2 看板流转，外加机器人回执面（回写提交状态/检查运行/表情）——自动分类、stale 清理、PR 评审、ChatOps 都能拼出来。事件支持 Webhook（零延迟）与轮询两条路。**需自行部署插件进程**：安装后在本页接入组里复制接入命令。 | 既有集成 |
| [`gitlab`](gitlab) | GitLab | 把 GitLab 接进工作流：读写仓库文件、建/评/合 MR、开 Issue、查流水线与失败日志、触发构建。 | 既有集成 |
| [`gmail`](gmail) | Gmail | 读写你的 Gmail：列邮件 / 读邮件 / 发邮件，并可作为事件源——收到新邮件时触发工作流。 | 既有集成 |
| [`google-calendar`](google-calendar) | Google 日历 | 管理日程、查询忙闲并监听变更 | 待首次发布 |
| [`google-docs`](google-docs) | Google Docs | 读取文档标签页并自动编辑文字 | 待首次发布 |
| [`google-drive`](google-drive) | Google 云端硬盘 | 检索、传输并监听云端硬盘文件 | 待首次发布 |
| [`google-sheets`](google-sheets) | Google 表格 | 读取、写入并监听电子表格行 | 待首次发布 |
| [`gotenberg`](gotenberg) | Gotenberg | 通过自建 Gotenberg 服务将 HTML 和办公文档转换为 PDF，并合并平台 PDF 文件 | 待首次发布 |
| [`grafana`](grafana) | Grafana | 在自建 Grafana 上查看仪表盘及安全的数据源摘要，读取或创建注释。 | 待首次发布 |
| [`hackernews`](hackernews) | Hacker News | 读榜单、帖子、评论，按关键词搜索；有人评论你的帖子、或回复你的评论时触发工作流。 | 既有集成 |
| [`hubspot`](hubspot) | HubSpot CRM | 使用私有应用令牌读取及管理联系人、公司和交易 | 待首次发布 |
| [`intercom`](intercom) | Intercom | 通过对应区域的 Intercom API 管理联系人和会话 | 待首次发布 |
| [`jina-reader`](jina-reader) | Jina Reader | 将网页转换为正文和结构化元数据 | 待首次发布 |
| [`jira`](jira) | Jira Cloud | 查询和管理 Jira Cloud issue、状态流转及评论 | 待首次发布 |
| [`kbstore-es`](kbstore-es) | 知识库存储（Elasticsearch） | 知识库的存储与检索后端：写入分块、向量检索、BM25、混合检索（RRF 融合）。 | 既有集成 |
| [`kbstore-pgvector`](kbstore-pgvector) | 知识库存储（pgvector） | 知识库存储引擎插件（Postgres + pgvector），与 kbstore-es 实现同一份存储契约。 | 既有集成 |
| [`kubernetes`](kubernetes) | Kubernetes | 在画布上操作 K8s 集群：查 pod、看容器日志、滚动重启、扩缩副本、看事件与节点健康。 | 既有集成 |
| [`linear`](linear) | Linear | 查询 Linear issue 和项目，创建或更新 issue | 待首次发布 |
| [`linkding`](linkding) | linkding | 在自托管 linkding 服务上搜索、保存和整理书签 | 待首次发布 |
| [`linkedin`](linkedin) | LinkedIn | 以授权账号的个人身份发动态。自助接入、无需合作伙伴审批，适合投研观点触达专业受众。 | 既有集成 |
| [`mailchimp`](mailchimp) | Mailchimp | 读取受众、成员、标签和营销活动；显式新增或更新联系人 | 待首次发布 |
| [`mastodon`](mastodon) | Mastodon | 发嘟文、发嘟文串、删嘟文。免费、无审批、无按次计费，且因为是联邦网络， | 既有集成 |
| [`meilisearch`](meilisearch) | Meilisearch | 在自建 Meilisearch 服务中索引和搜索文档，并检查异步任务 | 待首次发布 |
| [`microsoft-excel`](microsoft-excel) | Microsoft Excel 365 | 使用明确会话读取和编辑商业工作簿的区域与表格行 | 待首次发布 |
| [`microsoft-onedrive`](microsoft-onedrive) | Microsoft OneDrive | 浏览和搜索网盘、上传小文件并显式跟踪变更 | 待首次发布 |
| [`microsoft-outlook`](microsoft-outlook) | Microsoft Outlook | 读取和发送邮件、下载附件及管理日历事件 | 待首次发布 |
| [`microsoft-sharepoint`](microsoft-sharepoint) | Microsoft SharePoint | 发现站点与列表、查看列定义并读写列表项字段 | 待首次发布 |
| [`microsoft-teams`](microsoft-teams) | Microsoft Teams | 浏览团队和聊天，以委托身份读取或发送消息 | 待首次发布 |
| [`mineru`](mineru) | MinerU | 异步文档解析、批量任务与结构化结果链接 | 待首次发布 |
| [`miniflux`](miniflux) | Miniflux | 在自托管 Miniflux 上管理订阅、已存文章和阅读状态 | 待首次发布 |
| [`monday`](monday) | Monday | 通过指定版本的 GraphQL 发现 Monday 看板并管理项目、列值和动态 | 待首次发布 |
| [`nextcloud`](nextcloud) | Nextcloud | 在自托管 Nextcloud 上读取、上传、下载和整理文件 | 待首次发布 |
| [`notion`](notion) | Notion | 读写 Notion：搜索、查表、建页、改正文（markdown）、评论、文件，并可作为事件源—— | 既有集成 |
| [`ntfy`](ntfy) | ntfy | 通过自己的 ntfy 服务发布和轮询通知 | 待首次发布 |
| [`paperless-ngx`](paperless-ngx) | Paperless-ngx | 在自建 Paperless-ngx 实例上搜索、上传和管理文档、入库任务及标签。 | 待首次发布 |
| [`pipedrive`](pipedrive) | Pipedrive | 通过 Pipedrive API v2 管理 CRM 联系人、组织和交易 | 待首次发布 |
| [`pocketbase`](pocketbase) | PocketBase | 读取和管理自托管 PocketBase 服务中的记录 | 待首次发布 |
| [`producthunt`](producthunt) | Product Hunt | 读产品、评论和每日榜单；你的产品下有新评论或新回复时触发工作流。发布和回复在网页上做。 | 既有集成 |
| [`prometheus`](prometheus) | Prometheus | 查询自建 Prometheus 服务器的指标，查看标签元数据并读取活动告警。 | 待首次发布 |
| [`qdrant`](qdrant) | Qdrant | 在自建 Qdrant 服务中管理稠密向量集合、点和相似度查询 | 待首次发布 |
| [`reddit`](reddit) | Reddit | 搜帖子、读版块和用户的帖子、读评论；你的帖子下有新评论或关键词命中时触发工作流。走公开 feed，不需要 API key；不能发帖回复。 | 既有集成 |
| [`redis`](redis) | Redis | 把 Redis 当工作流的共享内存：跨运行的计数器、去重集合、状态标记、队列、排行榜， | 既有集成 |
| [`resend`](resend) | Resend | 发送和查询事务邮件、收件及域名配置 | 待首次发布 |
| [`salesforce`](salesforce) | Salesforce | 查询 Salesforce 元数据和记录，并创建、更新或按外部 ID 写入记录 | 待首次发布 |
| [`searxng`](searxng) | SearXNG | 通过自托管 SearXNG 实例搜索网页 | 待首次发布 |
| [`shopify`](shopify) | Shopify | 读取店铺、商品、订单和库存项，并更新商品信息 | 待首次发布 |
| [`slack`](slack) | Slack 协作 | 频道消息、线程、文件与新消息轮询 | 待首次发布 |
| [`stripe`](stripe) | Stripe | 读取客户、订阅和 PaymentIntent，并验证签名 webhook 事件 | 待首次发布 |
| [`submail`](submail) | 赛邮短信 | 通过 SUBMAIL（赛邮云通信）发国内短信与国际短信：告警通知、验证码、运营触达。 | 既有集成 |
| [`supabase`](supabase) | Supabase | 查询数据库记录、调用函数和访问存储 | 待首次发布 |
| [`synology-upload`](synology-upload) | 群晖上传 | 把工作流里的文件上传到群晖 NAS 的文件夹，走 DSM 的 FileStation 接口，Synology Drive / Cloud Sync 能照常同步。**需自行部署插件进程**，部署在能访问到 DSM 的机器上即可。 | 既有集成 |
| [`synology`](synology) | 群晖 NAS | 监听 NAS 目录里的文件变动并触发工作流（落定检测已内置）。**需自行部署插件进程**，且必须部署在能访问该 NAS 的机器上。 | 既有集成 |
| [`telegram-bot`](telegram-bot) | Telegram Bot | 把 Telegram 机器人接进工作流：收消息触发、发消息/图片/文件、下载用户发来的附件，也可以直调任意 Bot API 方法。 | 既有集成 |
| [`tencent-docs`](tencent-docs) | 腾讯文档 | 腾讯文档 | 待首次发布 |
| [`threads`](threads) | Threads | 发帖、发帖串。免费、无按次计费；受众规模是这批海外渠道里最大的之一。 | 既有集成 |
| [`tianyancha`](tianyancha) | 天眼查 | 商业企业信息和工商变更查询。 | 待首次发布 |
| [`todoist`](todoist) | Sokel for Todoist | 管理 Todoist 项目和任务；本插件并非由 Todoist 创建、关联或支持 | 待首次发布 |
| [`trello`](trello) | Trello | 发现 Trello 看板并通过请求头鉴权管理卡片和评论 | 待首次发布 |
| [`tushare`](tushare) | Tushare Pro | 券商研报增量 + 221 个 TuShare 目录接口。只负责取数——不落库、不去重、不加工。 | 既有集成 |
| [`twilio`](twilio) | Twilio | 发送短信和彩信，查询投递状态、电话号码和媒体元数据 | 待首次发布 |
| [`typeform`](typeform) | Typeform | 通过显式分页读取 Typeform 工作区、表单和已完成答卷 | 待首次发布 |
| [`umeng`](umeng) | 友盟推送 | App 推送的友盟通道：按设备 token 单播/列播，或全量广播。与「阿里云」插件的 | 既有集成 |
| [`vikunja`](vikunja) | Vikunja | 管理自托管 Vikunja API v2 服务中的项目、任务和评论 | 待首次发布 |
| [`wechat-claw`](wechat-claw) | 微信 ClawBot | 接入微信官方 ClawBot（iLink Bot 接口）：你在微信里给 ClawBot 发消息即触发工作流，工作流可回复文本、图片、文件。只有扫码绑定的本人能与它对话。 | 既有集成 |
| [`wechat-mp`](wechat-mp) | 微信公众号 | 把文章写进草稿箱、发布、拿到永久链接。国内唯一一条「API 正经、金融内容可做、覆盖面够」的主渠道。 | 既有集成 |
| [`wecom`](wecom) | 企业微信 | 自建应用消息、自定义交互按钮、原生卡片、接收回调、菜单、工作台、身份与审批集成 | 既有集成 |
| [`wordpress`](wordpress) | WordPress | 读取文章与分类、创建草稿、更新内容及上传媒体 | 待首次发布 |
| [`wps`](wps) | WPS365 云文档 | 管理 WPS365 云文档元信息与文件夹 | 待首次发布 |
| [`x-free`](x-free) | X（免费读写） | 使用账号 Cookie 搜索和读取 X 推文、查看用户资料、发帖、回复与删除，无需官方 API 套餐。 | 既有集成 |
| [`x`](x) | X (Twitter) | 发推、发推串、传图传视频、点赞转推、搜索与时间线、私信、列表，以及被提及 / 关键词命中时触发工作流。 | 既有集成 |
| [`xueqiu`](xueqiu) | 雪球（非官方） | 发帖（可带图）+ 检查凭证。 | 既有集成 |
| [`youtube-transcript`](youtube-transcript) | YouTube 字幕 | 取任意公开 YouTube 视频的字幕，产出带时间轴的分句和拼好的全文。不需要 YouTube 账号，也不需要 API … | 既有集成 |
| [`youtube`](youtube) | YouTube | 取视频信息、下载视频/音频、抓字幕文本（基于 yt-dlp） | 既有集成 |
| [`zendesk`](zendesk) | Zendesk | 读取、搜索和管理 Zendesk Support 工单与评论 | 待首次发布 |
| [`zoom`](zoom) | Zoom | 读取用户并安排或更新 Zoom 会议 | 待首次发布 |

## 构建

```bash
docker build --build-arg PLUGIN=telegram-bot -t sokel-plugin-telegram-bot .   # Go 插件共用一份镜像配方
cd telegram-bot && go generate ./... && go test ./...                         # 改了插件 schema 之后
```

每次推送到 `main`，Images 流水线都会构建 linux/amd64 和 linux/arm64 镜像并发布到 ghcr（`:latest` 和 `:sha-<提交>`）。

Python 插件保留各自的 Dockerfile，例如：

```bash
docker build -f youtube/Dockerfile -t sokel-plugin-youtube youtube
cd youtube && pip install -r requirements.txt && python -m unittest discover -v
```

CI 检查两种运行时。Python 契约用 SDK v0.7.6 的 `sokel-gen generate .` 重新生成。

## 发版

给 `main` 上的提交打 `<插件>/vX.Y.Z` 标签就是发版：

```bash
git tag discord/v1.0.1 && git push origin discord/v1.0.1
```

Release 流水线随即构建 `ghcr.io/sokel-dev/sokel-plugin-discord:1.0.1`，重写它在[插件目录](https://github.com/sokel-dev/sokel-registry)里的条目
（契约取自 `sokel-gen export`、`plugin.version`、按摘要钉死的镜像），用目录自己的准入检查和版本门校验，到那边开 PR，并在本仓发一个 GitHub release。
合并那个 PR，新版本就上了目录页。版本号必须比条目现有的高；首次发版会使用导出的契约、本地使用文档和本地翻译文件创建条目（新插件使用英文原文与 `locales/zh-CN.json`，旧插件仍兼容 `locales/en.json`），镜像摘要取自实际构建结果；schema 导出缺少的展示或部署元数据取自同名的本地 `manifest.yml` 或 `sokel.yaml`，导出中已有的字段优先；目录 PR 仍须维护者审核。

**一次最多推三个标签**：一次推送里标签超过三个时，GitHub 对其中任何一个都不触发工作流，所以给一堆插件打完标签后 `git push --tags` 什么都不会发布。要三个一组地推。

## 默认语言

新建或修改的插件界面使用英文：插件名、操作与字段标签、描述、凭证帮助、运行时错误、README 和默认用户文档均以英文为原文。中文翻译放在 `locales/zh-CN.json`，键必须与英文原文完全一致；整篇文档翻译以完整英文 Markdown 为键，中文文档可另存为 `*.zh-CN.md`。无需为了迁移语言而重写无关旧插件。

翻译文件必须是 JSON 对象，从非空原文字符串映射到译文字符串，不允许重复键。首次发布接受新规范的 `zh-CN.json` 或旧规范的 `en.json`。

## 参与贡献

欢迎提 PR 修 bug 和改进。这些插件在 Sokel 项目里维护、同步到这里，合并的改动由维护者带回去。
你自己的新插件不必放在这里：在你自己的仓库发布，再加进[插件目录](https://github.com/sokel-dev/sokel-registry)即可。

## 许可

[Apache-2.0](LICENSE)。
