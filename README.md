# Sokel Official Plugins

[![CI](https://github.com/sokel-dev/sokel-official-plugins/actions/workflows/ci.yml/badge.svg)](https://github.com/sokel-dev/sokel-official-plugins/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

English · [简体中文](README.zh-CN.md)

The integration plugins maintained by the Sokel project: messaging, social, developer tools, cloud, data sources and
storage engines for the [Sokel](https://github.com/sokel-dev) workflow platform. Each directory is one plugin, written
with the [plugin SDK](https://github.com/sokel-dev/sokel-plugin-sdk); browse them with their operations and
credentials for released versions in the [plugin registry](https://plugins.sokel.dev/).

## Run one

Install the plugin from your platform's marketplace, create an access group and copy its token.
Open the plugin's [registry entry](https://plugins.sokel.dev/) and copy the complete
container image reference, including its version tag and `@sha256:` digest. Replace the placeholder
below with that reference, then start a replica anywhere that can reach both the service and your platform:

```bash
SOKEL_PLUGIN_IMAGE='<copy-complete-image-reference-from-registry>'
docker run -d --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://sokel.example.com \
  -e SOKEL_TOKEN=skp_xxx \
  "$SOKEL_PLUGIN_IMAGE"
```

Also pass any required deployment environment variables listed in that registry entry.

Credentials (API keys, bot tokens) are not set here: they live in the platform's credential store. Each plugin's
README and `docs/` explain what it needs.

## Plugins

The repository contains **104 integrations**. The eight expansion batches added **68 integrations with 491 operations and 9 events**. All Go and Python plugins use SDK **v0.7.8**. Find each released version, container image, configuration and usage guide in the [plugin registry](https://plugins.sokel.dev/).

| Directory | Name | What it does | Release status |
|---|---|---|---|
| [`airtable`](airtable) | Airtable tables | Base schemas, record operations and change polling | Published |
| [`aliyun`](aliyun) | Alibaba Cloud | Bring the Alibaba Cloud control plane into workflows: query SLS logs, check RDS status and slow SQL, update DNS records, manage ACK clusters, and query CloudMonitor metrics. | Existing integration |
| [`amap`](amap) | AMap | Geocoding, place search, routes, weather and administrative districts. | Published |
| [`apache-tika`](apache-tika) | Apache Tika | Extract document text, metadata and embedded content with a self-hosted Apache Tika 4.2 service | Published |
| [`apify`](apify) | Apify | Asynchronous Actor runs and paginated dataset access | Published |
| [`asana`](asana) | Asana | Browse projects and manage tasks and comments. | Published |
| [`baserow`](baserow) | Baserow | Discover tables and manage dynamic rows on a self-hosted Baserow service | Published |
| [`bluesky`](bluesky) | Bluesky | Post, thread, and delete posts. Free, no approval needed, no per-call charges — one of the lowest-cost public social platforms to integrate with. | Existing integration |
| [`bocha-search`](bocha-search) | Bocha Search | Search the web with freshness filters and optional summaries. | Published |
| [`bookstack`](bookstack) | BookStack | Read, search and maintain knowledge pages on a self-hosted BookStack instance. | Published |
| [`box`](box) | Box | Read, search and transfer Box files with explicit pagination | Published |
| [`brevo`](brevo) | Brevo | Manage contacts and lists and explicitly send transactional email | Published |
| [`browser-use`](browser-use) | Browser Use Cloud | Run and inspect asynchronous Browser Use V4 agents | Published |
| [`calendly`](calendly) | Calendly | Read Calendly events and availability and create single-use scheduling links | Published |
| [`claude-code`](claude-code) | Claude Code | Wires the locally installed Claude Code into the workflow: give it a GitLab project and a task, | Existing integration |
| [`clickup`](clickup) | ClickUp | Discover workspaces and manage tasks and comments. | Published |
| [`confluence`](confluence) | Confluence Cloud | Read spaces and publish version-checked Confluence Cloud pages | Published |
| [`crawl4ai`](crawl4ai) | Crawl4AI | Crawl pages and extract Markdown with a self-hosted Crawl4AI server | Published |
| [`dingtalk`](dingtalk) | DingTalk | DingTalk | Published |
| [`discord`](discord) | Discord | Sends messages to a channel. Free, no approval needed — a channel admin can create a webhook in a few clicks. | Existing integration |
| [`dropbox`](dropbox) | Dropbox | Read, search and transfer Dropbox files with explicit cursors | Published |
| [`elasticsearch`](elasticsearch) | Elasticsearch | Brings Elasticsearch into workflows: query logs and documents, load data, manage indices. Supports ES 7/8/9 and OpenSearch — | Existing integration |
| [`exa`](exa) | Exa | Web search, cited answers and asynchronous research | Published |
| [`feed`](feed) | Feed subscription | Pulls new entries from a source, producing uniformly shaped JSON. Switching sources requires no change to downstream nodes. | Existing integration |
| [`feishu-webhook`](feishu-webhook) | Feishu group bot | The shortest path to sending a message to a Feishu group: no app to create, no admin to ask — set up in 30 seconds. | Existing integration |
| [`feishu`](feishu) | Feishu | Bring Feishu into workflows: send messages/cards, trigger on incoming messages, write documents, and read/write Base records. | Existing integration |
| [`figma`](figma) | Figma | Read design metadata, render images and manage comments | Published |
| [`firecrawl`](firecrawl) | Firecrawl | Web scraping, crawling and structured extraction | Published |
| [`gitea`](gitea) | Gitea | Work with repositories, issues, comments and pull requests on a self-hosted Gitea instance. | Published |
| [`github`](github) | GitHub | GitHub project maintenance automation (github.com / GitHub Enterprise Server): the full issue and PR workflow, repo read/write and branch creation, Actions triggering and failure logs, releases, Projects V2 board transitions, plus a bot-reply surface (writing back commit statuses/check runs/reactions) — auto-triage, stale cleanup, PR review, and ChatOps can all be built from these. Events support both webhook (zero latency) and polling. **Requires self-hosting the plugin process**: after installing, copy the connection command from this page's access group. | Existing integration |
| [`gitlab`](gitlab) | GitLab | Brings GitLab into workflows: read and write repository files, create/comment on/merge MRs, open issues, check pipelines and failure logs, and trigger builds. | Existing integration |
| [`gmail`](gmail) | Gmail | Read and write your Gmail: list emails / read emails / send emails, and it can also act as an event source — triggering a workflow when a new email arrives. | Existing integration |
| [`google-calendar`](google-calendar) | Google Calendar | Manage calendar events, query availability and watch changes | Published |
| [`google-docs`](google-docs) | Google Docs | Read document tabs and automate text edits | Published |
| [`google-drive`](google-drive) | Google Drive | Search, transfer and watch Google Drive files | Published |
| [`google-sheets`](google-sheets) | Google Sheets | Read, write and watch spreadsheet rows | Published |
| [`gotenberg`](gotenberg) | Gotenberg | Convert HTML and office documents to PDF and merge platform PDF files with a self-hosted Gotenberg service | Published |
| [`grafana`](grafana) | Grafana | Inspect dashboards and safe data source summaries, and read or create annotations on self-hosted Grafana. | Published |
| [`hackernews`](hackernews) | Hacker News | Read lists, stories and comments, and search by keyword; trigger a workflow when someone comments on your story or replies to your comment. | Existing integration |
| [`hubspot`](hubspot) | HubSpot CRM | Read and manage contacts, companies and deals with a private-app token | Published |
| [`intercom`](intercom) | Intercom | Manage contacts and conversations with regional Intercom API access | Published |
| [`jina-reader`](jina-reader) | Jina Reader | Convert pages into content and structured metadata | Published |
| [`jira`](jira) | Jira Cloud | Search and manage Jira Cloud issues, transitions and comments | Published |
| [`kbstore-es`](kbstore-es) | Knowledge base store (Elasticsearch) | Storage and retrieval backend for knowledge bases: write chunks, vector search, BM25 and hybrid search (RRF fusion). | Existing integration |
| [`kbstore-pgvector`](kbstore-pgvector) | Knowledge base store (pgvector) | Knowledge base storage engine on Postgres + pgvector; implements the same storage contract as kbstore-es. | Existing integration |
| [`kubernetes`](kubernetes) | Kubernetes | Operate a Kubernetes cluster from the canvas: inspect pods, view container logs, do rolling restarts, scale replicas, and check events and node health. | Existing integration |
| [`linear`](linear) | Linear | Query Linear issues and projects and create or update issues | Published |
| [`linkding`](linkding) | linkding | Search, save and organize bookmarks on a self-hosted linkding service | Published |
| [`linkedin`](linkedin) | LinkedIn | Post updates as the authorized account's personal profile. Self-service setup with no partner approval needed — good for reaching a professional audience with research commentary. | Existing integration |
| [`mailchimp`](mailchimp) | Mailchimp | Read audiences, members, tags and campaigns; explicitly upsert contacts | Published |
| [`mastodon`](mastodon) | Mastodon | Post, post threads, and delete posts. Free, no approval needed, no per-call charge, and because it is a federated network, | Existing integration |
| [`meilisearch`](meilisearch) | Meilisearch | Index and search documents and inspect asynchronous tasks on a self-hosted Meilisearch service | Published |
| [`microsoft-excel`](microsoft-excel) | Microsoft Excel 365 | Read and edit business workbooks with explicit sessions, ranges and table rows | Published |
| [`microsoft-onedrive`](microsoft-onedrive) | Microsoft OneDrive | Browse, search, upload small files and explicitly track drive changes | Published |
| [`microsoft-outlook`](microsoft-outlook) | Microsoft Outlook | Read and send mail, download attachments and manage calendar events | Published |
| [`microsoft-sharepoint`](microsoft-sharepoint) | Microsoft SharePoint | Discover sites and lists, inspect columns and read or write list item fields | Published |
| [`microsoft-teams`](microsoft-teams) | Microsoft Teams | Browse teams and chats and read or send delegated messages | Published |
| [`mineru`](mineru) | MinerU | Asynchronous document parsing, batches and structured result links | Published |
| [`miniflux`](miniflux) | Miniflux | Manage subscriptions, stored articles and reading state on a self-hosted Miniflux service | Published |
| [`monday`](monday) | Monday | Discover Monday boards and manage items, column values and updates with versioned GraphQL | Published |
| [`nextcloud`](nextcloud) | Nextcloud | Read, upload, download and organize files on a self-hosted Nextcloud service | Published |
| [`notion`](notion) | Notion | Reads and writes Notion: search, query tables, create pages, edit content (markdown), comments, files, and can act as an event source — | Existing integration |
| [`ntfy`](ntfy) | ntfy | Publish and poll notifications through your own ntfy server | Published |
| [`paperless-ngx`](paperless-ngx) | Paperless-ngx | Search, upload and manage documents, ingestion tasks and tags on a self-hosted Paperless-ngx instance. | Published |
| [`pipedrive`](pipedrive) | Pipedrive | Manage CRM persons, organizations and deals with Pipedrive API v2 | Published |
| [`pocketbase`](pocketbase) | PocketBase | Read and manage records in a self-hosted PocketBase service | Published |
| [`producthunt`](producthunt) | Product Hunt | Reads products, comments and the daily leaderboard; fires a workflow when your product gets a new comment or reply. Launching and replying happen on the site. | Existing integration |
| [`prometheus`](prometheus) | Prometheus | Query metrics, inspect label metadata and read active alerts from a self-hosted Prometheus server. | Published |
| [`qdrant`](qdrant) | Qdrant | Manage dense-vector collections, points and similarity queries on a self-hosted Qdrant service | Published |
| [`reddit`](reddit) | Reddit | Search posts, read subreddit and user posts and comments; fires a workflow on new comments under your posts or on a keyword hit. Uses public feeds, no API key; cannot post or reply. | Existing integration |
| [`redis`](redis) | Redis | Use Redis as shared memory for your workflow: cross-run counters, dedup sets, status flags, queues, leaderboards, | Existing integration |
| [`resend`](resend) | Resend | Send and inspect transactional email, received mail and domain configuration | Published |
| [`salesforce`](salesforce) | Salesforce | Query Salesforce metadata and records and create, update or upsert records | Published |
| [`searxng`](searxng) | SearXNG | Search the web through a self-hosted SearXNG instance | Published |
| [`shopify`](shopify) | Shopify | Read shop, products, orders and inventory items and update product details | Published |
| [`slack`](slack) | Slack collaboration | Channel messages, threads, files and new-message polling | Published |
| [`stripe`](stripe) | Stripe | Read customers, subscriptions and payment intents; verify signed webhook events | Published |
| [`submail`](submail) | SUBMAIL SMS | Send domestic and international SMS through SUBMAIL: alert notifications, verification codes, and marketing outreach. | Existing integration |
| [`supabase`](supabase) | Supabase | Query database records, call functions and access storage | Published |
| [`synology-upload`](synology-upload) | Synology Upload | Upload files from a workflow into a Synology NAS folder through DSM's FileStation API, so Synology Drive / Cloud Sync sync them as usual. **You deploy the plugin process yourself**, on any machine that can reach DSM. | Existing integration |
| [`synology`](synology) | Synology NAS | Watches a NAS directory for file changes and triggers a workflow (settle detection is built in). **You must deploy the plugin process yourself**, on a machine that can access this NAS. | Existing integration |
| [`telegram-bot`](telegram-bot) | Telegram Bot | Connects a Telegram bot into your workflow: trigger on incoming messages, send messages/images/files, download attachments users send, or call any Bot API method directly. | Existing integration |
| [`tencent-docs`](tencent-docs) | Tencent Docs | Tencent Docs | Published |
| [`threads`](threads) | Threads | Post, and post threads. Free, no per-call charge; one of the largest audiences among these overseas channels. | Existing integration |
| [`tianyancha`](tianyancha) | Tianyancha | Commercial company information and registration change queries. | Published |
| [`todoist`](todoist) | Sokel for Todoist | Manage Todoist projects and tasks; not created by, affiliated with, or supported by Todoist | Published |
| [`trello`](trello) | Trello | Discover Trello boards and manage cards and comments with header authentication | Published |
| [`tushare`](tushare) | Tushare Pro | Brokerage research report updates (incremental) + 221 Tushare catalog endpoints. Only responsible for fetching data — no persistence, no deduplication, no processing. | Existing integration |
| [`twilio`](twilio) | Twilio | Send SMS and MMS, inspect delivery state, phone numbers and media metadata | Published |
| [`typeform`](typeform) | Typeform | Read Typeform workspaces, forms and completed responses with explicit pagination | Published |
| [`umeng`](umeng) | Umeng push | The Umeng channel for app push notifications: unicast/list-cast by device token, or full broadcast. Same as the "Aliyun" plugin's | Existing integration |
| [`vikunja`](vikunja) | Vikunja | Manage projects, tasks and comments on a self-hosted Vikunja API v2 service | Published |
| [`wechat-claw`](wechat-claw) | WeChat ClawBot | Connects WeChat's official ClawBot (iLink Bot API): messaging ClawBot in WeChat triggers a workflow, which can reply with text, images or files. Only the person who bound it by scanning the QR code can chat with it. | Existing integration |
| [`wechat-mp`](wechat-mp) | WeChat Official Account | Writes articles into the draft box, publishes them, and returns a permanent link. The only domestic channel with a proper API, that allows financial content, and has broad enough reach. | Existing integration |
| [`wecom`](wecom) | WeCom | Custom app messages, configurable action buttons, native cards, incoming callbacks, menus, workbench, identity, and approval integrations | Existing integration |
| [`wordpress`](wordpress) | WordPress | Read posts and categories, create drafts, update content and upload media | Published |
| [`wps`](wps) | WPS365 Cloud Documents | Manage WPS365 cloud file metadata and folders | Published |
| [`x-free`](x-free) | X (Free Read/Write) | Search and read X posts, view profiles, post, reply, and delete using account cookies, without an official API subscription. | Existing integration |
| [`x`](x) | X (Twitter) | Post tweets and threads, upload images and video, like and retweet, search and read timelines, send DMs, manage lists, and trigger workflows on mentions / keyword matches. | Existing integration |
| [`xueqiu`](xueqiu) | Xueqiu (unofficial) | Post (optionally with images) + check credential. | Existing integration |
| [`youtube-transcript`](youtube-transcript) | YouTube transcript | Fetches the transcript of any public YouTube video, producing timestamped segments and the assembled full text. No YouTube account or API … | Existing integration |
| [`youtube`](youtube) | YouTube | Fetch video information, download video or audio, and extract subtitle text with yt-dlp | Existing integration |
| [`zendesk`](zendesk) | Zendesk | Read, search and manage Zendesk Support tickets and comments | Published |
| [`zoom`](zoom) | Zoom | Read users and schedule or update Zoom meetings | Published |

## Build

```bash
docker build --build-arg PLUGIN=telegram-bot -t sokel-plugin-telegram-bot .   # shared recipe for Go plugins
cd telegram-bot && go generate ./... && go test ./...                         # after changing a plugin's schema
```

The Release workflow publishes versioned images for linux/amd64 and linux/arm64 and records their
digests in the registry. The separate Images workflow provides `:latest` and `:sha-<commit>`
tags from `main`. Deploy using the registry image reference with its version and digest.

Python plugins keep a Dockerfile in their directory; for example:

```bash
docker build -f youtube/Dockerfile -t sokel-plugin-youtube youtube
cd youtube && pip install -r requirements.txt && python -m unittest discover -v
```

CI checks both runtimes. Regenerate Python contracts with `sokel-gen generate .` (SDK v0.7.6).

## Releasing

A plugin is released by tagging a commit of `main` as `<plugin>/vX.Y.Z`:

```bash
git tag discord/v1.0.1 && git push origin discord/v1.0.1
```

The Release workflow then builds `ghcr.io/sokel-dev/sokel-plugin-discord:1.0.1`, rewrites the plugin's entry in the
[catalog](https://github.com/sokel-dev/sokel-registry) (contract from `sokel-gen export`, `plugin.version`, the image
pinned by its digest), checks it with the catalog's own admission and version gate, opens the pull request there and
publishes a GitHub release here. Merging that pull request puts the version on the catalog page. The version must go
up from the entry's current one. A first release creates its entry from the exported contract, local usage document
and its locale assets, using the image digest returned by the build. If a schema export omits display or deployment
metadata, the matching local `manifest.yml` or `sokel.yaml` supplies it; exported fields take precedence. New plugins use English source text
and `locales/zh-CN.json` for Chinese translations; legacy Chinese-source plugins may still ship `locales/en.json`. The catalog still requires maintainer review.

Push **at most three tags at a time**: GitHub starts no workflow for any of the tags in a push that carries more than
three, so `git push --tags` after tagging many plugins releases nothing. Push them in groups of three instead.

## Interface language

Use English for new or changed plugin names, operation/field labels, descriptions, credential help,
runtime errors, README files, and the default user guide. Chinese translations belong in
`locales/zh-CN.json`, keyed by the exact English source; a translated guide uses the complete English
markdown as its key. Localized guide files may use `*.zh-CN.md`. Existing unrelated plugins do not
need to be migrated. Locale files must be valid JSON objects mapping nonempty source strings to
translation strings; duplicate keys are rejected. First releases accept either the new `zh-CN.json`
or a legacy `en.json` file.

## Contributing

Bug fixes and improvements are welcome as pull requests. The plugins are maintained in the Sokel project and mirrored
here, so a merged change is carried over by a maintainer. A new plugin of your own does not need to live here:
publish it from your own repository and add it to the [registry](https://github.com/sokel-dev/sokel-registry).

## License

[Apache-2.0](LICENSE).
