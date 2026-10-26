# 接收消息与自动回复

接收用户发给自建应用的消息、菜单操作和官方已授权推送的事件。不是个人微信全部聊天记录的接收器，也不会让没有开放事件权限的应用自动获得外部联系人或通讯录变更。

## 配置 API 接收

1. 在企业微信「应用管理 → 自建应用 → 接收消息 → 设置 API 接收」生成 Token 和 43 位 EncodingAESKey，暂不保存。
2. 在 Sokel 本插件的同一凭证中填入「接收消息 Token」与「接收消息 EncodingAESKey」，保存；原 CorpID、AgentId、Secret 保持正确。
3. 从该凭证 Webhook 面板复制完整 HTTPS URL，在末尾加 `/callback`，填入企业微信的 URL。例如 `https://sokel.example.com/hooks/本凭证令牌/callback`。
4. 确认两边 Token、EncodingAESKey 完全一致，再保存企业微信配置。插件会验证 GET 挑战并返回解密后的原文，无需再部署一个回调服务。
5. 从企业微信应用会话发送一条文字，调用 `list_received` 查看；然后在 Sokel 选择本凭证，创建「收到应用消息」或「收到应用事件」触发器。

`send_interactive_message` 的 H5 入口使用不带 `/callback` 的完整 Webhook 地址。H5 的 `interaction_action` 来自插件网页操作，不要求配置这里的官方 API 接收；原生模板卡片、应用消息和菜单事件则需要。主站继续提供域名校验文件。插件进程和平台消息通道需在线，入口不能被额外的登录页拦截；服务器时间应准确，官方 API 回调只接受当前时间前后 5 分钟的签名请求。

普通消息需要正确 AgentID；官方部分系统事件可以缺 AgentID。所有消息均验证加密接收企业、签名和 XML，未通过的消息不会保存或触发流程。callback 配置为空时不会影响发送普通消息或使用 H5 交互。

## 自动回复

用 `auto_reply_set` 保存规则：

```json
{
  "rule_id": "help",
  "enabled": true,
  "match_type": "text_exact",
  "keyword": "帮助",
  "reply": "请点击通知中的查看消息，在网页中选择反馈。输入状态可查看服务说明。",
  "priority": 10
}
```

`match_type` 支持 `text_exact`（精确文字）、`text_contains`（包含文字）和 `menu_click`（菜单事件的 key）。规则 ID 使用字母、数字、下划线、短横线，最多 64 字节；回复最多 2048 UTF-8 字节。数值较大的优先级先匹配，相同优先级按 ID 排序，只回复第一条匹配规则。`auto_reply_list` 查询、`auto_reply_delete` 按 rule_id 删除；同 ID 保存即更新。

回复作为同一次回调的加密被动文本响应返回。未命中则只确认接收。企业微信重试相同消息时返回原回复，不重复触发事件。模板卡片交互不会套用文本自动回复规则，应由工作流调用 `update_template_card`。

## 事件和查询

- `message_received`：普通文字、图片、语音、视频、位置、链接等消息。
- `application_event`：菜单点击、进入应用、模板卡片交互、审批状态等事件；按 `event_type`、`event_key`、`change_type` 分支。
- `list_received`：`{"limit":20}` 查看最近的本应用接收记录，最大 100 条。

上述两个事件提供 `event_id`、`msg_id`、`from_user`、`to_user`、`agent_id`、`message_type`、`content`、`event_type`、`event_key`、`change_type`、`created_at`、`received_at`、`media_id`、`pic_url`、`task_id`、`card_type`、`response_code` 和完整 `raw_xml`。不适用的字段为空；图片/位置/审批等更详细内容保留在 XML。`msg_id` 是字符串，不能转换成 JS number 丢失精度。创建时间为 Unix 秒，接收时间为 RFC3339。

H5 自定义按钮使用独立事件 `interaction_action`，包含 `action_id`、`action_value`、`context` 等字段，格式见[交互回调说明](wecom.md#处理交互回调)。它与 `application_event` 的原生 `event_key` 不可混用，也不在 `list_received` 中按官方 XML 记录查询。

例如「收到应用事件 → event_type 为 template_card_event → 校验 from_user 与业务状态 → 更新内部系统 → update_template_card」，用此次事件的 `response_code`、`from_user` 更新对应成员卡片；模板卡片不适用于普通微信。审批事件根据使用的审批体系筛选对应事件类型，详见 [身份与审批](identity-approval.md)。

## 持久化和交付

先保存回调和待提交事件，再向企业微信确认。事件源每 5 秒最多向平台提交 16 条，同一 `event_id` 去重。Sokel SDK 的 Trigger 没有平台接收确认：本地提交成功不等于工作流已经运行或内部系统已经更新；重要业务应查询接收记录、检查工作流结果并设计补偿。

已提交记录保留七天，随写入清理；最多 5000 条消息、2000 条规则，快照最多 64 MiB。满额时返回错误让上游重试，未提交事件不会被自动丢弃。企业微信的重试次数有限，应监测平台连接和存储容量。

应用 Secret 轮换不会改变记录归属。EncodingAESKey 轮换会建立新的回调历史/规则命名空间：先清空待提交事件，再轮换并重新建立自动回复规则。不要同时运行多个使用独立数据卷的插件副本。

依据：[回调配置](https://developer.work.weixin.qq.com/document/path/90238)、[消息格式](https://developer.work.weixin.qq.com/document/path/90239)、[事件格式](https://developer.work.weixin.qq.com/document/path/90240)、[被动回复](https://developer.work.weixin.qq.com/document/path/90241)、[加解密说明](https://developer.work.weixin.qq.com/document/path/90968)。
