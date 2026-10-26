# 企业微信插件

向企业通讯录成员发送文字、链接卡片和可交互消息。普通微信收到链接卡片后，点击“查看消息”进入微信内网页，再点击调用方配置的按钮；这不是直接在普通微信消息内操作。企业微信客户端还可使用原生模板卡片的消息内按钮。

0.3.0 提供 34 个操作。交互消息只负责展示内容、识别成员并发出动作事件，适用于发布确认、任务反馈、通知回执等场景；具体业务规则由工作流或内部系统定义。

## 功能范围

| 管理后台功能 | 插件支持 | 需要额外配置或限制 |
|---|---|---|
| 发送消息 | 11 类原生消息、自定义 H5 交互、专用原生模板卡片操作、临时素材上传及撤回 | 普通微信不支持 markdown、小程序通知、原生模板卡片及撤回 |
| 接收消息 | 加密验证、普通消息和应用事件触发器、最近记录查询 | 后台设置 API 接收，填写回调 URL、Token、EncodingAESKey |
| 自动回复 | 文本精确/包含匹配、菜单点击规则，持久保存 | 先配置 API 接收；只回复已启用且匹配的规则 |
| 自定义菜单 | 获取、设置、删除 | 微信和企微支持的菜单动作不同 |
| 聊天附件栏、聊天工具栏 | 提供网页身份和 JS-SDK 签名能力 | 入口在管理后台配置；需已有业务网页并接入前端 JS-SDK，非普通微信功能 |
| 工作台自定义展示 | 模板、单成员数据及批量成员数据 | 管理后台先启用，仅企业微信客户端生效 |
| 网页授权及 JS-SDK | H5 成员自动识别、成员换码、双签名生成 | 可信域名及归属校验仍需配置 |
| 企业微信授权登录 | 当前网页登录 URL、code 换成员身份 | 业务登录回调自己核验 state 并建立会话 |
| 审批接口 | 自建应用引擎查询/状态回调；审批应用模板和申请 API | 两套体系不同；自建应用流程由前端 JSAPI 发起，审批应用需授权 |
| 企业可信 IP | 通过接口错误提示检查 | 必须在企业微信后台添加插件出口 IP，插件无法代设 |

完整用法：[消息、菜单、工作台](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/app-features.md) · [接收消息与自动回复](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/callbacks.md) · [身份、JS-SDK 与审批](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/identity-approval.md)。这些是集成能力，不会自动修改已有菜单、创建审批单或安装业务页面。

## 开通条件

1. 在企业微信创建自建应用，记录企业 ID、AgentId 和应用 Secret，将收件人加入通讯录并纳入应用可见范围。
2. 在应用的企业可信 IP 中加入**插件进程的公网出口 IP**。
3. 成员用普通微信关注“我的企业 → 微信插件”中的邀请二维码，完成成员绑定，开启应用消息接收。
4. 取消“成员使用微信插件时需要使用企业微信客户端”；不要开启“在微工作台中始终进入主页”（否则消息会被截短或转为去企业微信查看的提示）。
5. 在 Sokel 安装插件并创建上述凭证。启动插件后，从该凭证的 Webhook 面板复制完整 HTTPS URL。
6. 将 URL 的域名配置到企业微信应用的**网页授权可信域名**，按管理后台要求完成域名归属校验。验证文件需由该域名的网站根目录或反向代理提供，不能只放在 `/hooks/…` 路径下。
7. 如果平台域名不归你们所有、不能完成归属校验，应使用可配置并完成校验的自有域名反向代理该平台 Webhook。保持对外 URL 与 `portal_url` 一致。

只支持企业成员 UserID，不接收手机号、微信号或外部联系人 external_userid，不支持 `@all`。同一 CorpID + AgentId 的凭证共享交互消息命名空间；不要用同一个自建应用隔离互不信任的租户。

## 多应用、多凭证

同一个插件进程可以服务多个应用。在 Sokel 为每个自建应用创建一份凭证，分别填写 CorpID、AgentId、Secret；接收回调时再填写各自 Token、EncodingAESKey，并把该凭证自己的 Webhook URL 加 `/callback` 配入对应应用。工作流的操作节点与事件触发器选择相应凭证即可。不同企业也可使用不同 CorpID 的凭证。

访问令牌和 JS-SDK 票据按完整应用凭证隔离；交互消息按 CorpID + AgentId 隔离，回调规则和历史再加入 EncodingAESKey 的哈希。一个应用的凭证不能读取另一个应用的交互消息或派发其待处理事件。同一 CorpID + AgentId 重复创建凭证是同一应用的别名，会共享交互消息数据；回调密钥也相同则共享回调规则和历史，不是独立租户。

不需要为每个应用运行单独的容器；仍保持整个插件一个副本与一个持久卷。所有使用同一出口的应用，都要把该出口 IP 加入各自的可信 IP 配置，并分别设置可见范围与网页可信域名。

## 安装与运行

开发者先在插件目录执行 `go build -o wecom .`。Sokel「插件 → 导入清单」导入本目录 `manifest.yml`，获得接入组令牌，然后运行：

```sh
SOKEL_ENDPOINT=https://sokel.example.com \
SOKEL_TOKEN=skp_replace_me \
SOKEL_DATA_DIR=/var/lib/sokel-wecom \
SOKEL_INSTANCE_ID=wecom-1 \
./wecom
```

凭证只在 Sokel 凭证页面配置，不能写进环境变量或数据文件。插件主动连接平台，H5 使用平台 Webhook，无需新增监听端口。数据目录必须可写、挂载持久卷，**仅运行一个副本**；同目录第二个进程会拒绝启动。不要用两个不同数据卷启动同一个接入组的副本。支持 Linux/macOS，Windows 存储锁目前不支持。

官方镜像为 `ghcr.io/sokel-dev/sokel-plugin-wecom:0.3.0`，支持 Linux amd64/arm64；官方目录将镜像固定到发布摘要。请使用以下持久化启动方式，平台生成的普通启动命令不会自动创建持久卷：

```sh
docker run -d --name sokel-wecom --restart unless-stopped \
  -e SOKEL_ENDPOINT=https://sokel.example.com \
  -e SOKEL_TOKEN="$SOKEL_PLUGIN_ACCESS_TOKEN" \
  -e SOKEL_INSTANCE_ID=wecom-1 \
  -e SOKEL_DATA_DIR=/var/lib/sokel/wecom \
  -v sokel-wecom-data:/var/lib/sokel \
  ghcr.io/sokel-dev/sokel-plugin-wecom:0.3.0
```

`SOKEL_PLUGIN_ACCESS_TOKEN` 为接入组令牌，由部署环境注入。只启动一个副本，并在更新或重建容器时复用同一个命名卷。也可使用[Compose 示例](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docker-compose.yml)或[Kubernetes 单副本持久化示例](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/deploy/k8s.yaml)；Kubernetes 使用 Secret 保存接入信息、PVC 保存数据。

## 发送方式

| 操作 | 用途 |
|---|---|
| `send_text` | 普通文字消息，最多 2048 字节 |
| `send_card` | 标题、说明、链接组成的卡片，说明作为纯文字转义；用于打开业务页面 |
| `send_interactive_message` | 保存内容与自定义按钮，发送链接卡片，在 H5 中操作并回传事件 |
| `get_interactive_message` | 查询内容、按钮、业务上下文、最近动作及发送结果 |
| `send_template_card` | 发送官方原生卡片对象，仅企业微信客户端支持消息内交互 |
| `send_message` | 高级发送接口，支持 11 种原生消息类型与对应官方参数 |
| `health_check` | 验证凭证所属应用及本地存储，不发送消息，不验证收件人和微信显示 |

## 普通微信：链接卡片与 H5 自定义按钮

`send_interactive_message` 输入示例：

```json
{
  "users": ["zhangsan", "lisi"],
  "title": "请确认发布计划",
  "content": "版本 rel-001 计划今晚发布，请核对变更说明并反馈意见。此示例只收集意见，不会执行发布。",
  "buttons": [
    {"id": "approve", "text": "同意", "value": {"decision": "approved"}},
    {"id": "reject", "text": "暂不同意", "value": {"decision": "rejected"}}
  ],
  "context": {"scene": "release_review", "release_id": "rel-001"},
  "request_id": "release-review-rel-001",
  "portal_url": "https://sokel.example.com/hooks/本凭证的令牌",
  "button": "查看消息",
  "expires_hours": 72
}
```

字段规则：

- `users` 是明确的企业成员 UserID 数组；只有本次发送被接口接受的收件人可以进入并操作。
- `title` 为 1–128 个字符，`content` 为 1–16384 个 UTF-8 字节。消息摘要会截短，H5 保留完整正文。
- `buttons` 为 1–8 个按钮。`id` 唯一，使用 1–64 个 ASCII 字母、数字、下划线或短横线；`text` 为 1–40 个字符。可选 `value` 必须为 JSON 对象。
- 可选 `context` 是所有动作共用的 JSON 对象。`context` 与全部按钮 `value` 编码后的合计大小不得超过 16 KiB。它们会进入回调，不要放密码或凭证。
- `request_id` 为可选的发送幂等键。未填写时，每次调用都创建新的交互消息；填写时，同一应用内相同键和内容返回已有消息。
- `portal_url` 使用发送凭证自己的完整 Webhook URL，不加 `/callback`。`button` 是消息卡片进入 H5 的链接文案，为 1–4 个字符，默认“查看消息”，与 `buttons` 中的动作按钮不同。
- `expires_hours` 默认 72，可设 1–720；过期后不能再操作。

这里的按钮均为动作回调按钮。仅需跳转链接时使用 `send_card`。`approve`、`reject` 等 ID 是调用方的业务定义；插件不内置严重程度、认领权限、确认终态或审批状态机。

返回 `interaction_id`、`interaction_url`、`reused`、`msg_id`、`invalid_user`、`unlicensed_user` 和 `all_accepted`。复用幂等键时不再次发送，重试不会延长原有效期；改变收件人、内容、按钮、上下文或入口等配置时，应使用新的键。去重仅覆盖记录保留期，清理后的同一键不能继续保证复用。重复请求不会补发此前无效的收件人，修复成员配置后应重新发送并使用新键。

检查输出 `all_accepted`、`invalid_user` 和 `unlicensed_user`。企业微信接口返回成功不代表所有人都接收成功，也不代表手机收到推送或用户已读。

## 处理交互回调

在微信内打开卡片时，页面会自动通过静默网页授权识别成员，成功后显示消息正文和按钮。重新打开或刷新页面仍会重新识别身份；验证失败、授权回调异常或浏览器后退恢复页面时，会显示手动重试入口，避免反复跳转。其他浏览器保留手动入口，建议使用微信打开。

平台只接受官方授权返回的成员 ID，不信任 URL 或表单传来的用户名。一次成功操作消费票据并换发新票据，成员可以继续点击；同一票据和同一动作的请求重试会返回原结果，不重复产生事件。是否只允许一次选择、多人之间如何冲突处理、某个状态下是否允许操作，必须由工作流或业务系统校验。

每次新动作产生 `interaction_action` 事件，包括：

- `event_id`：本次动作的幂等键。
- `interaction_id`、`msg_id`：插件交互消息 ID 和企业微信消息 ID。
- `request_id`：发送时提供的可选幂等键。
- `action_id`、`action_text`、`action_value`：被点击按钮的 ID、文字与对象值。
- `context`：发送时的业务上下文对象，例如 `scene` 与 `release_id`。
- `actor_user_id`、`occurred_at`：操作者和操作时间。
- `revision`：同一交互消息递增的动作修订号。

在 Sokel 配置：**交互消息动作事件 → 按 `context.scene` 筛选 → 按 `event_id` 去重 → 校验业务权限和当前状态 → 按 `action_id` 分支 → 保存处理结果或发送回执**。示例中的 `approve` / `reject` 只是反馈选择；创建该消息或收到其动作都不会自动发布版本，也不会自动发起企业微信审批申请。

事件字段在自身命名空间下，例如 `{{#n_event.interaction_action.action_id#}}`、`{{#n_event.interaction_action.context.release_id#}}`。工作流选择与发送相同的凭证；需要使用事件凭证回话时，把下游凭证绑定到 `{{#n_event.credential_id#}}` 并限定允许的凭证清单。真实事件驱动的流程需要发布并启用相应触发器。

以 `event_id` 去重，并用 `revision` 拒绝会覆盖新状态的旧更新。插件记录的是成员动作，不是业务审批已经完成；例如内部系统拒绝第二次审批时，应自行记录拒绝原因、按需回执。

调用 `get_interactive_message`，输入 `{"interaction_id":"发送结果中的 ID"}`，可查询内容、`buttons`、`context`、`request_id`、`last_action_id`、`actor_user_id`、`revision`、`created_at`、`updated_at`、`expires_at`、`sent`、`msg_id`。该查询提供最近动作，不是完整动作历史重放接口。

动作与待发送事件在同一快照落盘。发送到平台失败会重试，重启后由该凭证事件源继续提交；相同事件使用固定 ID。**“通知已提交”仅表示 SDK 接受了发送，不能代表平台已收到或工作流已经完成回写**。当前 SDK 事件协议没有接收确认，网络缓冲期间崩溃仍可能丢事件；本插件的 outbox 保障本地发布前的失败重试，不承诺至少一次送达。重要业务应通过 `get_interactive_message` 核对最近已保存动作，并检查自己的业务记录与工作流结果、设计补偿。后台每 5 秒最多提交 16 个事件，页面保存动作后不等待下游工作流完成。

## 企业微信：原生模板卡片

使用 `send_template_card`，输入 `users` 与官方原生 `card` 对象。此路径面向企业微信客户端，支持的卡片类型和内容遵循官方接口；需要 API 接收配置。其原生操作继续通过 `application_event` 回调，按 `event_type=template_card_event`、`event_key`、`task_id`、`response_code` 处理，再按需调用 `update_template_card`。它不会创建 H5 交互消息，也不产生 `interaction_action`。完整示例见[应用功能文档](https://github.com/sokel-dev/sokel-official-plugins/blob/wecom/v0.3.0/wecom/docs/app-features.md)。

## 部署和验收边界

- 使用企业微信官方接口；不使用个人微信外挂、Hook 或客户端自动化。
- 企业微信应用发送存在频控；大量通知应先在上游聚合。网络中断或“上游发送成功、插件尚未保存结果”时无法保证通知恰好一次，重试可能出现重复消息。
- OAuth state 和操作票据均 5 分钟有效，票据只对一条交互消息及一个成员有效，成功操作即消耗并换发新票据。Sokel Webhook 不转发 Cookie，本插件用浏览器 sessionStorage 绑定授权 state，不依赖 Cookie。
- 平台 Webhook 日志会记录请求／响应，可能含短时授权材料和消息内容。限制日志查看权限与保留期；不要把 Secret 或业务密码放入正文、按钮值或业务上下文。长期 Secret 和 access_token 不写入插件数据文件。
- 交互消息在有效期结束后继续保留 24 小时，随后随写入清理；未提交事件不会自动丢弃。单实例最多保留 10000 条交互消息、20000 条待处理或保留事件、20000 条每类认证记录；达到容量会明确失败。请备份持久卷并监控磁盘与事件源状态。
- 单次数据快照须小于 128 MiB，超过会明确拒绝写入；此存储面向中小规模交互场景，不适合作为高吞吐消息队列。
- 早期版本曾在真实企业账号验证发送接口接受文字／链接卡片、普通微信成员身份识别及旧流程动作落盘。这不能替代 0.3.0 的验收：新自定义按钮、重复点击、非收件人拒绝、原生模板卡片回调、Sokel 事件接收及内部系统回写都需要按目标客户端验证。

官方依据：[微信插件](https://open.work.weixin.qq.com/help2/pc/14797)、[发送应用消息](https://developer.work.weixin.qq.com/document/path/90236)、[网页授权链接](https://developer.work.weixin.qq.com/document/path/91022)、[访问用户身份](https://developer.work.weixin.qq.com/document/path/91023)。

## 升级说明

0.3.0 将交互能力改为通用消息和自定义按钮，移除公开的 `send_alert`、`get_alert` 与 `alert_action`。旧工作流必须迁移操作、事件、字段引用和业务逻辑，不能直接沿用：

| 旧接口 | 新接口 | 迁移内容 |
|---|---|---|
| `send_alert` | `send_interactive_message` | `description` 改为 `content`；显式配置 `buttons`；业务标识放入 `context`，可选发送去重键为 `request_id` |
| `get_alert` | `get_interactive_message` | 输入 `interaction_id`，读取最近动作；业务状态由工作流或内部系统维护 |
| `alert_action` | `interaction_action` | 改用 `action_id`、`action_value`、`context` 等新字段，不再接收内置认领/确认状态 |

V1 存储仍可读，旧记录中没有 `buttons` 的消息保持只读，不会自动转成新的可操作消息。升级前备份持久卷；重新发送需要交互的消息，并完成工作流迁移。新操作、事件未承诺兼容旧流程。

API 接收配置仍为可选，未配置不会影响文字、链接卡片与 H5 交互。应用 Secret 轮换保留交互消息和待提交回调；EncodingAESKey 轮换会切换官方 API 回调的历史和规则命名空间，请先处理待提交事件并重建规则。

只有已配置权限的接口才能在真实企业调用。API 接收、自动回复、原生卡片交互、工具栏业务页面和审批需要分别完成后台配置与客户端验收。
