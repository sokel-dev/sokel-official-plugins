# Sokel Official Plugins

[![CI](https://github.com/sokel-dev/sokel-official-plugins/actions/workflows/ci.yml/badge.svg)](https://github.com/sokel-dev/sokel-official-plugins/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

English · [简体中文](README.zh-CN.md)

The integration plugins maintained by the Sokel project: messaging, social, developer tools, cloud, data sources and
storage engines for the [Sokel](https://github.com/sokel-dev) workflow platform. Each directory is one plugin, written
with the [plugin SDK](https://github.com/sokel-dev/sokel-plugin-sdk); browse them with their operations and
credentials in the [plugin registry](https://sokel-dev.github.io/sokel-registry/).

## Run one

Install the plugin from your platform's marketplace, create an access group and copy its token, then start a
replica anywhere that can reach both the service and your platform:

```bash
docker run -d --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://sokel.example.com \
  -e SOKEL_TOKEN=skp_xxx \
  ghcr.io/sokel-dev/sokel-plugin-<name>:latest
```

Credentials (API keys, bot tokens) are not set here: they live in the platform's credential store. Each plugin's
README and `docs/` explain what it needs.

## Plugins

| Directory | Name | What it does |
|---|---|---|
| [`aliyun`](aliyun) | Alibaba Cloud | Bring the Alibaba Cloud control plane into workflows: query SLS logs, check RDS status and slow SQL, update DNS records, manage ACK clusters, and query CloudMonitor metrics. |
| [`bluesky`](bluesky) | Bluesky | Post, thread, and delete posts. Free, no approval needed, no per-call charges — one of the lowest-cost public social platforms to integrate with. |
| [`claude-code`](claude-code) | Claude Code | Wires the locally installed Claude Code into the workflow: give it a GitLab project and a task, |
| [`discord`](discord) | Discord | Sends messages to a channel. Free, no approval needed — a channel admin can create a webhook in a few clicks. |
| [`elasticsearch`](elasticsearch) | Elasticsearch | Brings Elasticsearch into workflows: query logs and documents, load data, manage indices. Supports ES 7/8/9 and OpenSearch — |
| [`feed`](feed) | Feed subscription | Pulls new entries from a source, producing uniformly shaped JSON. Switching sources requires no change to downstream nodes. |
| [`feishu-webhook`](feishu-webhook) | Feishu group bot | The shortest path to sending a message to a Feishu group: no app to create, no admin to ask — set up in 30 seconds. |
| [`feishu`](feishu) | Feishu | Bring Feishu into workflows: send messages/cards, trigger on incoming messages, write documents, and read/write Base records. |
| [`github`](github) | GitHub | GitHub project maintenance automation (github.com / GitHub Enterprise Server): the full issue and PR workflow, repo read/write and branch creation, Actions triggering and failure logs, releases, Projects V2 board transitions, plus a bot-reply surface (writing back commit statuses/check runs/reactions) — auto-triage, stale cleanup, PR review, and ChatOps can all be built from these. Events support both webhook (zero latency) and polling. **Requires self-hosting the plugin process**: after installing, copy the connection command from this page's access group. |
| [`gitlab`](gitlab) | GitLab | Brings GitLab into workflows: read and write repository files, create/comment on/merge MRs, open issues, check pipelines and failure logs, and trigger builds. |
| [`gmail`](gmail) | Gmail | Read and write your Gmail: list emails / read emails / send emails, and it can also act as an event source — triggering a workflow when a new email arrives. |
| [`hackernews`](hackernews) | hackernews |  |
| [`kbstore-es`](kbstore-es) | Knowledge base store (Elasticsearch) | Storage and retrieval backend for knowledge bases: write chunks, vector search, BM25 and hybrid search (RRF fusion). |
| [`kbstore-pgvector`](kbstore-pgvector) | Knowledge base store (pgvector) | Knowledge base storage engine on Postgres + pgvector; implements the same storage contract as kbstore-es. |
| [`kubernetes`](kubernetes) | Kubernetes | Operate a Kubernetes cluster from the canvas: inspect pods, view container logs, do rolling restarts, scale replicas, and check events and node health. |
| [`linkedin`](linkedin) | LinkedIn | Post updates as the authorized account's personal profile. Self-service setup with no partner approval needed — good for reaching a professional audience with research commentary. |
| [`mastodon`](mastodon) | Mastodon | Post, post threads, and delete posts. Free, no approval needed, no per-call charge, and because it is a federated network, |
| [`notion`](notion) | Notion | Reads and writes Notion: search, query tables, create pages, edit content (markdown), comments, files, and can act as an event source — |
| [`redis`](redis) | Redis | Use Redis as shared memory for your workflow: cross-run counters, dedup sets, status flags, queues, leaderboards, |
| [`submail`](submail) | SUBMAIL SMS | Send domestic and international SMS through SUBMAIL: alert notifications, verification codes, and marketing outreach. |
| [`synology`](synology) | Synology NAS | Watches a NAS directory for file changes and triggers a workflow (settle detection is built in). **You must deploy the plugin process yourself**, on a machine that can access this NAS. |
| [`telegram-bot`](telegram-bot) | Telegram Bot | Connects a Telegram bot into your workflow: trigger on incoming messages, send messages/images/files, download attachments users send, or call any Bot API method directly. |
| [`threads`](threads) | Threads | Post, and post threads. Free, no per-call charge; one of the largest audiences among these overseas channels. |
| [`tushare`](tushare) | Tushare Pro | Brokerage research report updates (incremental) + 221 Tushare catalog endpoints. Only responsible for fetching data — no persistence, no deduplication, no processing. |
| [`umeng`](umeng) | Umeng push | The Umeng channel for app push notifications: unicast/list-cast by device token, or full broadcast. Same as the "Aliyun" plugin's |
| [`wechat-claw`](wechat-claw) | Personal WeChat | Connects a personal WeChat account into workflows: trigger on incoming messages, send text/image/file. |
| [`wechat-mp`](wechat-mp) | WeChat Official Account | Writes articles into the draft box, publishes them, and returns a permanent link. The only domestic channel with a proper API, that allows financial content, and has broad enough reach. |
| [`x`](x) | X (Twitter) | Post tweets and threads, upload images and video, like and retweet, search and read timelines, send DMs, manage lists, and trigger workflows on mentions / keyword matches. |
| [`xueqiu`](xueqiu) | Xueqiu (unofficial) | Post (optionally with images) + check credential. |
| [`youtube-transcript`](youtube-transcript) | YouTube transcript | Fetches the transcript of any public YouTube video, producing timestamped segments and the assembled full text. No YouTube account or API … |

## Build

```bash
docker build --build-arg PLUGIN=telegram-bot -t sokel-plugin-telegram-bot .   # one image recipe for all plugins
cd telegram-bot && go generate ./... && go test ./...                         # after changing a plugin's schema
```

Images for linux/amd64 and linux/arm64 are published to ghcr by the Images workflow on every push to `main`.

## Contributing

Bug fixes and improvements are welcome as pull requests. The plugins are maintained in the Sokel project and mirrored
here, so a merged change is carried over by a maintainer. A new plugin of your own does not need to live here:
publish it from your own repository and add it to the [registry](https://github.com/sokel-dev/sokel-registry).

## License

[Apache-2.0](LICENSE).
