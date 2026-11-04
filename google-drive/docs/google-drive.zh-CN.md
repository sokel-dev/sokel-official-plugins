# Google Drive

搜索或列出文件，上传小文件、下载二进制文件、导出 Google 文档，并轮询文件变更。

## 操作与边界

| 操作 | 用法 |
|---|---|
| `list_files` | query 是 Drive v3 q 语法，如 `name contains '研报' and trashed = false`；默认排除回收站；page_size 1–1000；把 next_page_token 原样传给 page_token |
| `upload_file` | 平台 file、可选 name / parent_id；multipart 新建文件，最多 5 MiB；不覆盖已有文件、不支持断点续传 |
| `download_file` | file_id，下载普通二进制文件，最多 32 MiB；返回平台文件引用 |
| `export_file` | Google 文档 file_id、mime_type、filename；Google 导出上限 10 MiB；例如文档导出 application/pdf |
| `health_check` | 验证身份授权 |

权限采用 drive.readonly + drive.file：可读用户授权范围内的文件；写入用于本应用创建的文件。上传到既有父文件夹还需要该文件夹的写权限和应用访问权，否则可能 403；默认上传到根目录。不实现删除、分享、移动或权限修改。共享云端硬盘的权限仍按 Google 账号与文件权限执行，不提供按 driveId 选择单独磁盘的界面。

文件字节通过平台文件层传入/返回，不把平台文件 URL 带到第三方下载接口。Google Docs/Sheets/Slides 必须用 export_file；普通 download_file 明确拒绝它们。Google Vids 的长任务下载尚未支持。

## 文件变更事件

凭证 watch_changes 填 true 启用每 60 秒的 `file_changed`；初次调用 changes.getStartPageToken，只记录当前位置，不推历史。覆盖新增、修改、回收站变化和失去访问/删除；`removed=true` 不等于永久删除，也可能只是失去访问，文件名可能为空。

逐页读取 changes.list，最终取得 newStartPageToken 且所有事件发布调用成功后才发布游标更新；单轮最多 1000 页，超限明确失败并保留旧游标。失败可能重发，平台按稳定事件 ID 去重。HTTP 410 不静默跳到现在：检查告警后手动清空 poll_cursor，期间变更可能无法补回。

SDK v0.7.7 的事件和凭证更新是 fire-and-forget：调用成功仅表示本地发布被受理，不证明平台已经接收或持久化。事件采用尽力交付，断线可能丢失或重发；稳定 ID 供平台去重，但不保证恰好一次。端到端交付与持久化仍需平台实机验收。自动管理的 next_poll_at 防止源重启绕过 60 秒间隔，请勿手动修改。

## 配置与运行

管理员先在平台配置 Google OAuth 应用及精确回调地址，并在同一 Google Cloud 项目启用本插件对应的 API。创建插件凭证后点击「授权」。本插件只使用平台注入的短期 access_token；client secret、换码和刷新都由平台负责，不会在插件里保存令牌文件。还会请求 userinfo.email 用于显示授权账号。

```sh
SOKEL_ENDPOINT=https://sokel.example.com SOKEL_TOKEN=skp_xxx ./google-drive
```

凭证中的 refresh_token 是平台授权流程使用的密文字段，不要手填。插件不接受自定义 Google API 地址，也不跟随 HTTP 重定向；请求错误不回传上游响应正文、访问令牌或完整 URL。读写都不自动重试；写入成功后网络中断可能造成结果不确定，请先核对上游再重试。

`health_check` 只验证 Google 身份授权，不证明目标 API 已启用、具体文件可访问或具有写权限。

## 验证状态

已用依据官方文档编写的合成协议样本验证请求字节、分页、失败分支和事件游标。样本不是真实账号抓包；本次没有 Google 实机凭证，未做 OAuth 授权、线上文件/日程读写或部署验收。正式发布前仍需测试账号完成验证。
