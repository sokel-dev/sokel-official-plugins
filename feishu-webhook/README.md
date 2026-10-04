# feishu-webhook — 飞书群自定义机器人（第一方自部署）

单操作插件：`webhook_send`（文本 / Markdown / 卡片）+ `health_check`。
面向用户的说明书是 [docs/feishu-webhook.md](docs/feishu-webhook.md)。

## 定位：与 feishu 主插件是两个插件

一条群 webhook 的授权范围（只能发一个群）和一个企业应用（能发全公司）**不该混在
一个凭证池里**——凭证类型是安全边界。做成主插件的可选字段的话，画布上装错凭证
要到运行时才炸。discord 插件是同构先例。设计取舍见 `schema/schema.go` 顶注。

## 坑（改代码前先读）

- **签名算法与直觉相反**：`HmacSHA256(key = timestamp+"\n"+secret, data = 空串)`。
  secret 在 key 里、被签数据是空串——飞书文档定的。`TestSignShape` 钉着一个与
  独立实现比对过的定值，谁按常规 HMAC「修好它」测试就红。
- **应答有两种壳**：新版 `{code,msg}`、老版 `{StatusCode,StatusMessage}`，都要认。
- **health_check 不发真消息**：发一个空 content 的校验请求，靠「参数错 ≠ URL 死」
  的差别判活。19021=密钥不对，9499=频控（100 条/分钟）。
- webhook URL 本身就是钥匙（拿到就能往群里发），凭证字段用 Secret 存。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
