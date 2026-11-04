# Google Calendar

列出、读取、创建、更新、删除日程，查询忙闲，并轮询日历变更。calendar_id 留空使用 primary；监听凭证中的 watch_calendar_id 则需显式填写。

## 操作

| 操作 | 说明 |
|---|---|
| `list_events` | 按 time_min / time_max / query 查询；展开重复日程并按开始时间排序；page_size 1–2500，page_token 继续分页 |
| `get_event` | calendar_id + event_id 读取单条 |
| `create_event` | summary、start、end；可选描述、地点、全天标志、IANA 时区和参与者邮箱 |
| `update_event` | 对已知 event_id PATCH；**支持的字段作为完整编辑表单提交**：描述、地点和参与者留空会清空原值；其他未支持字段保留 |
| `delete_event` | 删除已知 event_id |
| `free_busy` | RFC3339 时间窗口 + 1–50 个 calendar_ids；逐日历返回 busy 和 errors，不能把有 errors 的日历当作空闲 |
| `health_check` | 验证身份授权 |

非全天时间必须带时区偏移，例如 `2026-11-03T09:00:00+08:00`；可同时指定 IANA time_zone，例如 Asia/Shanghai。全天使用 `2026-11-03`；end 是不含在内的结束日期，一天日程应填次日。结束必须晚于开始。

send_updates 默认 none，可选 all 或 externalOnly。需要邀请通知请显式选择；none 可能影响参与者外部日历同步，操作依然可能受 Google 自身通知规则约束。不实现会议链接创建、附件、提醒、自定义重复规则、ACL 或日历创建。重复系列的修改需要先判断 event_id 是实例还是主系列；本插件不会代选。

## 日程变更事件

凭证 watch_calendar_id 填 primary 或具体日历 ID 后，每 60 秒轮询一次。首次完整同步只保存最终 sync token，不发送已有日程。增量同步保留相同查询条件、逐页消费，最后才更新游标；支持取消/删除事件，状态为 cancelled 时其他字段可能为空。事件输出 event 的标准字段包含时区、参与者、etag 和更新时间。

增量同步不展开重复系列，不等于实例到期提醒。HTTP 410 表示游标过期：明确报告错误并保持原游标；手动清空 poll_cursor 才会重新建基线，间隔期间的变更可能无法恢复。更换监听日历会建立新基线。单轮最多 1000 页；事件或游标写回失败可能重发，平台按稳定 ID 去重。

权限为 calendar.events + calendar.freebusy；Google 日历共享权限仍决定能否编辑。

SDK v0.7.7 的事件和凭证更新是 fire-and-forget：调用成功仅表示本地发布被受理，不证明平台已经接收或持久化。事件采用尽力交付，断线可能丢失或重发；稳定 ID 供平台去重，但不保证恰好一次。端到端交付与持久化仍需平台实机验收。自动管理的 next_poll_at 防止源重启绕过 60 秒间隔，请勿手动修改。

## 配置与运行

管理员先在平台配置 Google OAuth 应用及精确回调地址，并在同一 Google Cloud 项目启用本插件对应的 API。创建插件凭证后点击「授权」。本插件只使用平台注入的短期 access_token；client secret、换码和刷新都由平台负责，不会在插件里保存令牌文件。还会请求 userinfo.email 用于显示授权账号。

```sh
SOKEL_ENDPOINT=https://sokel.example.com SOKEL_TOKEN=skp_xxx ./google-calendar
```

凭证中的 refresh_token 是平台授权流程使用的密文字段，不要手填。插件不接受自定义 Google API 地址，也不跟随 HTTP 重定向；请求错误不回传上游响应正文、访问令牌或完整 URL。读写都不自动重试；写入成功后网络中断可能造成结果不确定，请先核对上游再重试。

`health_check` 只验证 Google 身份授权，不证明目标 API 已启用、具体文件可访问或具有写权限。

## 验证状态

已用依据官方文档编写的合成协议样本验证请求字节、分页、失败分支和事件游标。样本不是真实账号抓包；本次没有 Google 实机凭证，未做 OAuth 授权、线上文件/日程读写或部署验收。生产使用前仍需测试账号完成验证。
