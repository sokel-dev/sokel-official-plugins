# Google Sheets

读取电子表格元数据和单元格，更新、追加与批量写入，并监听只追加的表格末尾新行。

## 操作

| 操作 | 输入与输出 |
|---|---|
| `get_spreadsheet` | spreadsheet_id → 标题、时区、工作表 ID/名称/网格大小 |
| `read_values` | spreadsheet_id、A1 range → 二维 values；读取范围自行决定，无页令牌 |
| `update_values` | 在明确 A1 range 写入二维 values |
| `append_values` | 在 A1 范围内发现的逻辑表后追加，使用 INSERT_ROWS |
| `batch_update_values` | data 为 `{range, values}` 数组，一次 1–100 个范围 |
| `health_check` | 检查身份授权 |

表格 ID 是 URL 中 `/spreadsheets/d/` 后的一段；带空格的工作表名在 A1 引用中加单引号，例如 `'Sales 2026'!A1:D1000`。单元格只接受字符串、数字、布尔、null；null 跳过该单元格，清空请用空字符串。每次写入最多 10000 行、每行 10000 单元格，整个 JSON 最多 16 MiB。

写入默认 RAW，`=SUM(A1:A2)` 会作为文本保存；明确选择 USER_ENTERED 才让 Google 解析公式/日期。读取默认 FORMATTED_VALUE，可改 UNFORMATTED_VALUE 或 FORMULA。没有创建表格、格式/图表操作或任意 spreadsheets.batchUpdate；批量操作只更新单元格值。

## 末尾新增行事件

在凭证中填写 watch_spreadsheet_id 和 watch_range 后每 60 秒轮询一次，最多读取 10000 个非空尾部裁剪后的行。首次连接只建立基线，不推历史。`row_added` 的 row_offset 是相对监听范围从 1 开始的行序号，**不是工作表绝对行号**。

此模式只适合 append-only 范围：每轮校验旧行摘要，旧行修改、删除、重排或中间插入都会报告错误并停止推进游标；请检查后手动清空 poll_cursor 建立新基线。尾部全空行不会被 API 返回；填充早先空行也不属于末尾追加。更改监听目标会重新建基线，不补历史。请使用范围明确的业务表，避免全表大范围扫描。

一次轮询在全部事件发布调用成功后才发布游标更新；失败可能重发，事件 ID 稳定供平台去重。每份凭证只监听一个范围。权限范围为 spreadsheets；读写权限由 Google 的文件共享权限决定。

SDK v0.7.8 的事件和凭证更新是 fire-and-forget：调用成功仅表示本地发布被受理，不证明平台已经接收或持久化。事件采用尽力交付，断线可能丢失或重发；稳定 ID 供平台去重，但不保证恰好一次。端到端交付与持久化仍需平台实机验收。自动管理的 next_poll_at 防止源重启绕过 60 秒间隔，请勿手动修改。

## 配置与运行

管理员先在平台配置 Google OAuth 应用及精确回调地址，并在同一 Google Cloud 项目启用本插件对应的 API。创建插件凭证后点击「授权」。本插件只使用平台注入的短期 access_token；client secret、换码和刷新都由平台负责，不会在插件里保存令牌文件。还会请求 userinfo.email 用于显示授权账号。

```sh
SOKEL_ENDPOINT=https://sokel.example.com SOKEL_TOKEN=skp_xxx ./google-sheets
```

凭证中的 refresh_token 是平台授权流程使用的密文字段，不要手填。插件不接受自定义 Google API 地址，也不跟随 HTTP 重定向；请求错误不回传上游响应正文、访问令牌或完整 URL。读写都不自动重试；写入成功后网络中断可能造成结果不确定，请先核对上游再重试。

`health_check` 只验证 Google 身份授权，不证明目标 API 已启用、具体文件可访问或具有写权限。

## 验证状态

已用依据官方文档编写的合成协议样本验证请求字节、分页、失败分支和事件游标。样本不是真实账号抓包；本次没有 Google 实机凭证，未做 OAuth 授权、线上文件/日程读写或部署验收。生产使用前仍需测试账号完成验证。
