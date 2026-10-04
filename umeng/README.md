# umeng — 友盟推送插件（第一方自部署）

4 个操作：push（单播/列播/广播）、task_status、cancel、health_check。
说明书 [docs/umeng.md](docs/umeng.md)。接口细节参照一套已在生产运行的推送服务整理。

## 坑（改代码前先读）

- **签名是 MD5("POST" + 完整URL + body + master_secret) 拼在 ?sign=**——
  URL 里不含 ?sign= 自身；body 是序列化后的原文（改一个字节签名就错）。
- **Android 与 iOS 的 payload 形状完全不同**：Android 是 {display_type, body:{title,text}}
  自有格式；iOS 是 APNs 的 {aps:{alert:{}}}，自定义键与 aps 平级。
- **单播是消息类**：友盟不给任务统计，返回 msg_id 而非 task_id；查它的状态会 2000。
- health_check 拿不存在的任务号问状态：钥匙错回 1002/1003，钥匙对回「任务不存在」
  ——不发真推送就验完了凭证。
- cast type 按 token 数自动定：1 个=unicast，>1=listcast，0 个=broadcast。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
