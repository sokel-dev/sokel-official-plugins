# 企业微信应用功能使用说明

本页介绍 0.3.0 的应用消息、素材、原生模板卡片、菜单和工作台操作。普通微信中的自定义按钮使用 `send_interactive_message`：消息先打开 H5，再在网页中操作，配置和回调格式见 [主文档](wecom.md)。

## 先选择接收客户端

| 能力 | 普通微信中的微信插件 / 微工作台 | 企业微信客户端 |
|---|---|---|
| 文字、图片、语音、视频、文件、链接卡片和图文消息 | 可使用；仍受应用配置、成员绑定和客户端显示限制 | 可使用 |
| `send_interactive_message` 的自定义按钮 | 在微信内打开 H5 后操作 | 在 H5 中操作 |
| Markdown、小程序通知、原生模板卡片 | 官方不支持展示 | 可使用；小程序须与应用关联 |
| 模板卡片消息内按钮、投票和多项选择 | 不支持 | 支持，并可接收操作回调 |
| 撤回应用消息 | 不支持撤回微信插件端消息 | 仅可撤回发送后 24 小时内的消息 |
| 工作台自定义展示 | 不适用 | 支持关键数据、图片、列表和网页展示 |

扩展插件功能不会改变微信客户端本身的限制。只用普通微信的成员，应使用文字、链接卡片和 H5 交互；普通微信消息本身没有本插件提供的原生操作按钮。消息类型及客户端限制以[发送应用消息官方文档](https://developer.work.weixin.qq.com/document/path/90236)为准；撤回限制见[官方说明](https://developer.work.weixin.qq.com/document/path/94867)。

## 一次性后台配置

在企业微信管理后台进入「应用管理 → 自建 → 目标应用」。插件凭证中的 AgentId 必须对应这个应用。

| 要使用的功能 | 后台必须完成的配置 |
|---|---|
| 所有应用 API | 配置应用 Secret；在「企业可信 IP」添加插件进程的公网出口 IP |
| 向成员发消息 | 在应用「可见范围」加入接收成员；使用通讯录中的成员账号 UserID |
| 普通微信收消息 | 「我的企业 → 微信插件」配置成员关注和绑定；开启接收应用消息；不要启用「在微工作台中始终进入主页」 |
| 接收消息、菜单点击、模板卡片操作回调 | 在应用「接收消息 → 设置 API 接收」配置 URL、Token、EncodingAESKey |
| 网页内识别成员、H5 交互 | 「网页授权及 JS-SDK」配置应用网页授权可信域名，并完成域名文件校验 |
| 工作台自定义展示 | 进入「工作台自定义展示」，先启用该功能，再调用插件设置模板 |
| 小程序消息或菜单 | 先将小程序与企业及当前应用关联，使用实际 AppID 和页面路径 |

API 接收配置步骤：

1. 在 Sokel 该插件凭证中填写「接收消息 Token」和「接收消息 EncodingAESKey」。它们与应用 Secret 是三种不同配置。
2. 从同一凭证的 Webhook 面板取得完整 HTTPS URL，末尾加 `/callback` 后填写到企业微信「设置 API 接收」的 URL；两端填写完全相同的 Token 和 EncodingAESKey。
3. 保持插件在线，保存企业微信配置并通过 URL 验证。插件处理企业微信的加密验证和消息回调。
4. 在 Sokel 配置该凭证的「收到应用消息」或「收到应用事件」事件工作流。记录去重使用 `event_id`，不能仅依靠消息文本。

可信 IP 是插件所在机器的出口 IP，不是网站 DNS 的解析 IP。K8s 部署后通常应填节点、NAT 网关或统一出口的公网 IP。域名校验文件应继续由 web 服务在域名根目录提供。

## 通用发送应用消息

操作：`send_message`。

- `users`：1–1000 个明确的企业成员 UserID。不支持 `@all`、部门或标签群发。
- `message_type`：从下面 11 种类型中选择。
- `body`：JSON 对象，包含与消息类型同名的内容对象。传对象，不要把整段 JSON 再编码成字符串。

`body` 不能包含 `agentid`、`touser`、`toparty`、`totag` 或 `msgtype`。应用取自凭证，收件人只取自 `users`，类型只取自 `message_type`。

| `message_type` | `body` 的主要结构 | 使用说明 |
|---|---|---|
| `text` | `{"text":{"content":"通知内容"}}` | 简单文字也可用 `send_text` |
| `image` | `{"image":{"media_id":"…"}}` | 先上传图片素材 |
| `voice` | `{"voice":{"media_id":"…"}}` | 先上传 AMR 语音素材 |
| `video` | `{"video":{"media_id":"…","title":"演示","description":"说明"}}` | 先上传 MP4 视频素材 |
| `file` | `{"file":{"media_id":"…"}}` | 先上传普通文件素材 |
| `textcard` | `{"textcard":{"title":"发布说明","description":"查看本次变更","url":"https://example.com/releases/rel-001","btntxt":"查看"}}` | 点击进入网页；简单卡片可用 `send_card` |
| `news` | `{"news":{"articles":[{"title":"通知","description":"说明","url":"https://example.com/news","picurl":"https://example.com/banner.png"}]}}` | 图文链接 |
| `mpnews` | `{"mpnews":{"articles":[{"title":"通知","thumb_media_id":"…","content":"<p>正文</p>"}]}}` | 需要缩略图 media_id，可带 HTML 正文 |
| `markdown` | `{"markdown":{"content":"**发布计划**：请核对今晚的变更说明"}}` | 仅企业微信支持 |
| `miniprogram_notice` | `{"miniprogram_notice":{"appid":"wx…","page":"pages/index","title":"待办提醒","content_item":[{"key":"状态","value":"待处理"}]}}` | 仅企业微信支持，并须关联小程序 |
| `template_card` | `{"template_card":{"card_type":"button_interaction",…}}` | 仅企业微信支持；也可使用专用 `send_template_card` 操作 |

文字消息的完整操作输入：

```json
{
  "users": ["zhangsan"],
  "message_type": "text",
  "body": {
    "text": {"content": "版本 rel-001 的变更说明已更新，请查阅。"},
    "enable_duplicate_check": 1,
    "duplicate_check_interval": 1800
  }
}
```

可选发送标志包括 `safe`、`enable_id_trans`、`enable_duplicate_check` 和 `duplicate_check_interval`。标志值按官方接口填写数字，不能用布尔值代替；去重时间最大 14400 秒。`safe=2` 仅 `mpnews` 支持。不同消息类型支持的具体标志和内容长度请核对[官方类型说明](https://developer.work.weixin.qq.com/document/path/90236)。

返回 `msg_id` 后检查 `all_accepted`、`invalid_user`、`unlicensed_user`。接口接受不代表用户已读，也不保证所有客户端都能展示。支持回调更新的模板卡片还会返回 `response_code`，应与业务任务 ID 一起保存。

## 上传临时素材

操作：`upload_media`。在工作流选择 `media_type`，将上游文件变量或上传的文件绑定到 `file`。文件需要包含名称和原始内容；此操作不接受用于远程下载的 URL。

| 素材类型 | 格式 | 最大大小 |
|---|---|---|
| `image` | JPG / JPEG / PNG，扩展名与内容一致 | 10 MiB |
| `voice` | AMR，播放长度不超过 60 秒 | 2 MiB |
| `video` | MP4 | 10 MiB |
| `file` | 普通文件 | 20 MiB |

所有素材都必须大于 5 字节。插件会检查大小和基本格式；企业微信仍可能按具体编码、内容或应用权限拒绝文件。

输出为 `media_id`、`media_type` 和 Unix 秒时间 `created_at`。media_id 有效期为 3 天；过期后重新上传，不能长期缓存复用。流程示例：

```text
生成发布说明文件 → 上传临时素材（file）
               → 发送应用消息（file，body.file.media_id 引用上传结果）
```

上传只在明确的 token 失效响应后刷新 token 并重试一次。网络超时和其他结果不明的错误不会自动重试；再次上传可能生成额外素材 ID。接口定义见[上传临时素材](https://developer.work.weixin.qq.com/document/path/90253)。

## 原生模板卡片：消息内操作和更新

此流程面向企业微信客户端。普通微信成员使用 `send_interactive_message` 的 H5 流程，两者的事件格式不同。

先完成「API 接收」配置，再用专用操作 `send_template_card` 发送。`users` 为明确的成员 UserID，`card` 为官方原生卡片对象：

```json
{
  "users": ["zhangsan"],
  "card": {
    "card_type": "button_interaction",
    "task_id": "release-review-rel-001",
    "main_title": {
      "title": "请确认发布计划",
      "desc": "版本 rel-001；此示例只收集意见，不会执行发布"
    },
    "button_list": [
      {"text": "同意", "style": 1, "key": "approve"},
      {"text": "暂不同意", "style": 4, "key": "reject"}
    ]
  }
}
```

卡片结构按官方卡片类型填写；按钮的 `key` 与 H5 按钮的 `id` 属于不同接口。高级调用仍可用 `send_message`，输入 `message_type="template_card"`、`body={"template_card":上述 card 对象}`，按需添加官方发送标志。

成员点击后，配置 `application_event`（收到应用事件）工作流，并筛选 `event_type=template_card_event`。右上角菜单操作对应 `template_card_menu_event`。事件包含操作者 `from_user`、动作 `event_key`、业务任务 `task_id`、`card_type` 和本次可用的 `response_code`；复杂投票选项等字段保留在 `raw_xml`。[官方回调格式](https://developer.work.weixin.qq.com/document/path/90240)

建议工作流顺序：收到事件 → 按 `event_id` 去重 → 检查成员和业务状态 → 保存处理结果 → 更新卡片。原生卡片不会自动创建 H5 交互消息或产生 `interaction_action`；需要回传的业务上下文可由业务系统按 `task_id` 查询。示例的同意按钮只传递意见，没有自动发布行为。

将成员自己的卡片按钮更新为不可点击的文案：

```json
{
  "users": ["zhangsan"],
  "response_code": "引用本次事件的 response_code",
  "body": {
    "button": {"replace_name": "已记录"}
  }
}
```

上述输入用于 `update_template_card`。`users` 可以引用 `[事件.from_user]`，一次最多 1000 个成员；不支持在 `body` 中增加 `atall`、部门、标签或其他收件人。整张卡片替换使用另一种 `body`：

```json
{
  "template_card": {
    "card_type": "text_notice",
    "main_title": {"title": "发布意见已记录", "desc": "张三选择了同意"},
    "card_action": {"type": 1, "url": "https://example.com/releases/rel-001"}
  },
  "enable_id_trans": 0
}
```

`button` 和 `template_card` 必须二选一。可替换为 `text_notice`、`news_notice`、`button_interaction`、`vote_interaction` 或 `multiple_interaction`；原消息必须是可回调的卡片。文本通知型和图文展示型原卡片需带 `action_menu` 才能回调更新。仅替换按钮适用于原按钮交互、投票、多项选择卡片。

每个 `response_code` **72 小时内有效且只能调用一次**。发消息所得 code 与后续每次用户交互回调所得 code 分别使用。一次更新里应包含本次要更新的全部目标成员；不要对同一个 code 按人循环调用，也不要在网络结果不明时自动重试。返回 `invalid_users` 和 `all_updated`：部分成员失败时，其他成员可能已经更新，且 code 已被消费。[更新模板卡片官方说明](https://developer.work.weixin.qq.com/document/path/94888)

## 自定义菜单

操作：`get_menu`、`set_menu`、`delete_menu`。`set_menu` 覆盖当前应用菜单；修改前可用 `get_menu` 保留原配置。

`set_menu` 的 `body` 示例：

```json
{
  "button": [
    {"name": "待处理", "type": "click", "key": "tasks_pending"},
    {"name": "发布列表", "type": "view", "url": "https://example.com/releases"},
    {
      "name": "更多",
      "sub_button": [
        {"name": "帮助", "type": "click", "key": "help"},
        {"name": "系统状态", "type": "view", "url": "https://example.com/status"}
      ]
    }
  ]
}
```

最多 3 个一级菜单，每项最多 5 个二级菜单。一级名称最多 16 字节，二级最多 40 字节，中文通常占 3 字节。事件 `key` 最多 128 字节；网页 URL 最多 1024 字节。

`view` 打开页面；需要识别访问成员的页面，应使用网页授权。`click` 需要 API 接收配置，在「收到应用事件」中根据 `event_type=click` 和 `event_key` 分支处理。也支持官方的扫码、拍照、相册、位置选择和 `view_miniprogram` 动作；客户端实际入口和展示需在目标手机验收。[菜单类型与字段](https://developer.work.weixin.qq.com/document/path/90231)

应用身份由凭证确定，菜单 JSON 不要添加 `agentid`。删除操作会移除整个应用的自定义菜单。

## 应用主页和设置

`get_application` 获取当前凭证所属应用详情，包含上游允许返回的可见范围等信息。`set_application` 只修改 `body` 中给出的字段，例如：

```json
{
  "home_url": "https://example.com/tasks",
  "isreportenter": 1
}
```

支持 `home_url`、`name`、`description`、`logo_mediaid`、`redirect_domain`、`report_location_flag`、`isreportenter`。名称最多 32 个字符；详情 4–120 个字符。`redirect_domain` 只填域名，修改域名不会代替后台的所有权校验。进入应用、位置上报等事件仍需要 API 接收配置。可见范围、企业可信 IP 等权限设置继续在管理后台维护。[官方应用设置字段](https://developer.work.weixin.qq.com/document/path/90228)

## 工作台自定义展示

在企业微信管理后台打开应用「工作台自定义展示」并启用，然后使用 `set_workbench_template` 设置应用的模板和默认数据。例如 `body`：

```json
{
  "type": "keydata",
  "keydata": {
    "items": [
      {"key": "待处理任务", "data": "12", "jump_url": "https://example.com/tasks"},
      {"key": "待确认发布", "data": "2", "jump_url": "https://example.com/releases?state=pending"}
    ]
  },
  "replace_user_data": false
}
```

`replace_user_data=true` 会覆盖企业内已设置的成员工作台数据；常规修改默认值时使用 `false`。`get_workbench_template` 可查询当前模板。取消自定义模式使用 `set_workbench_template`，`body={"type":"normal"}`。

`set_workbench_data` 为指定成员设置数据，输入 `user_id` 和 `body`。`batch_set_workbench_data` 使用 `users` 数组，为最多 1000 个明确成员设置同一份 `body`。成员须在应用可见范围，body 不要包含 `agentid`、`userid` 或 `userid_list`。

```json
{
  "user_id": "zhangsan",
  "body": {
    "type": "keydata",
    "keydata": {
      "items": [
        {"key": "我的待处理", "data": "3", "jump_url": "https://example.com/tasks/mine"}
      ]
    }
  }
}
```

可用 `get_workbench_data` 查询成员最终展示的数据。每个应用同一时刻使用一种模板类型；成员数据类型应与应用模板一致。可选类型：

| 类型 | 主要内容 | 限制 |
|---|---|---|
| `keydata` | `items` 中的 `key`、`data` 和跳转地址 | 最多 4 项 |
| `image` | 图片 `url` 和跳转地址 | 建议比例 3.35:1 |
| `list` | `items` 中的 `title` 和跳转地址 | 最多 3 项 |
| `webview` | 展示页面 `url`，可设置高度和隐藏标题 | 网页自身应支持该展示尺寸 |

网页应用使用 `jump_url`；小程序主页使用 `pagepath`，与应用主页类型保持一致。工作台接口有更新频率限制：单成员设置每应用每人每分钟最多 10 次；批量设置每应用每分钟最多 100000 人次。业务数据应聚合后更新，避免每次细小变更都刷新工作台。[工作台自定义展示官方文档](https://developer.work.weixin.qq.com/document/path/92535)

## 验收方式

建议使用一个明确的测试成员依次验证：上传小文件并发文件消息、菜单 view 跳转、菜单 click 回调、企业微信模板卡片点击与更新、工作台成员数据。检查操作输出和收到的事件，保留业务任务 ID 与 `event_id` 对应关系。

本地协议测试关注 HTTP 参数、multipart、输入校验、token 更新以及部分成员失败信息保留。它们不替代 0.3.0 在目标手机和企业账号上的验收。插件能发出事件也不代表内部系统已经写回成功，应在工作流中检查实际业务结果。
