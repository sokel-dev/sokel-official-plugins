# submail — SUBMAIL 赛邮短信插件（第一方自部署）

7 个操作：国内短信 发送/模板发送/查发送状态、国际短信 发送/模板发送、查余额、health_check。
面向用户的说明书是 [docs/submail.md](docs/submail.md)。纯 HTTP 表单接口，无 SDK。

## 设计判断

- **凭证是两组钥匙**：SUBMAIL 控制台里国内「短信」与「国际短信」是两个应用、
  两对 appid/appkey。凭证四个字段全选填，`appOf()` 按操作取对应组，缺的那组
  给指路报错——而不是把国内钥匙发给国际接口换来一个 101。
- **鉴权用明文 appkey 模式**（signature = appkey）：全程 HTTPS，摘要签名防的
  「传输中窥视」不成立，而它换来时间戳对表的脆弱性。
- **两类错提前拦**：国内内容缺【签名】（运营商拒收要等回执才知道）、国际号码
  缺 + 国家码。都在插件里挡下，不打到 SUBMAIL 才发现。

## 坑

- 应答里 balance 是**字符串数字**（"12345"），用 json.Number 接。
- 错误码 101-104 都是「钥匙不对」，最常见的实际原因是**把国内的钥匙填进了国际组**，
  翻译里点了这句。
- health_check 配了哪组查哪组，坏了要说清是哪组坏。
- **查发送状态只在 v4 网关**（api-v4.mysubmail.com/sms/log），老网关回 Unknown method；
  国际短信没有对应接口（实测）。「收单成功≠到手机」，dropped+report 才是没收到的真相。
- **余额端点国内外是两个**：`/balance/sms`（按条）与 `/balance/internationalsms`（按金额）。
  拿国际钥匙打国内端点会把有效凭证误判成坏的——上过一次当，测试钉住了。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
