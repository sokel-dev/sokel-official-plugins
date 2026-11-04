# Zoom

读取 Zoom 用户和会议、创建预约会议，并更新会议详情。

## 凭据与权限

在本插件之外创建 Zoom 用户管理的 OAuth 应用，将有效访问令牌填写到 **OAuth 访问令牌**。插件不会创建 OAuth 提供方、交换授权码或刷新令牌；令牌到期后需要在外部更新。本版本使用全球 `https://api.zoom.us/v2` API，面向用户管理的 OAuth；不承诺 Server-to-Server OAuth 可以通过 `me` 身份检查。

`health_check` 调用 `GET /users/me`，返回 `ok` 和 `message`。成功只确认用户身份，不代表全部操作权限均已具备。读取用户需要 `user:read:user` 或管理员变体；列出账号用户需要 `user:read:list_users:admin`。会议操作分别需要 `meeting:read:list_meetings`、`meeting:read:meeting`、`meeting:write:meeting`、`meeting:update:meeting`，或适用的 `:admin` 变体。Zoom 还记录了较宽泛的旧式 `user:read`、`meeting:read`、`meeting:write` 权限。账号角色、应用审批和资源访问控制仍然生效；仅授予工作流需要的权限。

## 操作

| 操作 | 行为 |
| --- | --- |
| `health_check` | 只读检查当前用户身份。 |
| `get_user` | 按 ID 或邮箱读取用户；省略 `user_id` 表示 `me`。 |
| `list_users` | 返回一页账号用户；可选状态为 `active`、`inactive`、`pending`。 |
| `list_meetings` | 返回主持人一页未过期的预约会议；可选类型为 `scheduled`、`live`、`upcoming`、`upcoming_meetings`、`previous_meetings`。 |
| `get_meeting` | 使用数字会议号读取会议。 |
| `create_meeting` | 根据主题、开始时间、时长创建类型 2 的预约会议，可附加议程和会议设置。 |
| `update_meeting` | 仅 PATCH 已提供的变更；HTTP 204 返回 `updated=true`。 |

列表操作接受 1–300 的 `page_size`（省略或 0 使用 30）和 `next_page_token`。下一次调用保持筛选条件不变并传入返回的令牌。令牌大约 15 分钟后过期。每次执行仅发送一次请求；令牌只作为查询参数，不会被当作 URL 跟随。`has_more=false` 表示结束分页。本版本不支持即时会议、历史会议实例、循环会议配置、录制、参会者及删除会议。Zoom 对部分未来或历史会议列表模式限制为六个月。

## 创建与修改会议

例如将 `topic` 设为 `Weekly review`，`start_time` 设为 `2030-01-01T10:00:00+08:00`，`duration` 设为 `30`。开始时间必须是带时区偏移的 RFC 3339，插件统一转换为 UTC。时长范围为 1–1440 分钟；主题最多 200 字符，议程最多 2,000 字符。创建时使用 `timezone=UTC`；Zoom 会应用账号默认值，例如会议密码要求。

创建时的 `details.settings` 或更新时的 `changes.settings` 支持 `host_video`、`participant_video`、`join_before_host`、`mute_upon_entry`、`waiting_room`。省略字段保留上游默认值或现值；显式 `false` 会发送。更新时将 `agenda` 设为空字符串可清空议程。更新必须提供至少一个字段；更新后的开始时间必须在未来。更新不提供单次会议选择器，对现有循环会议的修改可能由 Zoom 应用于整个系列。账号策略可能覆盖设置；修改后请重新读取会议或在 Zoom 中检查。会议 UUID 不能作为数字会议号传入。

输出为结构化会议摘要，包含参会者 `join_url`。该链接可能包含会议密码，应作为私有信息处理。插件不会输出具有主持权限的 `start_url` 或主持人密钥。数字会议号以字符串返回，不经过浮点数转换。

## 限制与验收

操作时限为 60 秒，HTTP 超时 50 秒，请求上限 1 MiB，响应上限 8 MiB。插件阻止重定向，错误消息不会回显令牌或上游响应正文。所有请求都不会自动重试，包括写入。超时后结果可能不确定，重复创建前应在 Zoom 中核对。Zoom 配额和账号套餐限制仍然适用；当前官方文档对创建会议规定了每个主持人的每日上限。

测试使用根据官方文档裁剪、适配的合成数据；开发期间未验收真实 Zoom 账号，也未创建真实会议。生产使用前，请在自己的账号中核验权限、账号策略、分页和受控测试会议。

## 官方参考

- [会议接口](https://developers.zoom.us/docs/api/meetings/)
- [用户接口](https://developers.zoom.us/docs/api/users/)

参考文档核对日期为 2026-11-04。独立运行的 Go 插件通过 `SetDoc` 上传英文指南，但 SDK v0.7.8 不会上传语言字典。完整中文翻译位于 `locales/zh-CN.json`，通过注册表安装时一同打包；仓库中的独立中文指南也可直接阅读。
