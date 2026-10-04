# gmail — 读 Gmail + 新邮件触发（第一方自部署插件）

三个操作（列邮件 / 读单封 / 取附件）+ 一个事件（收到新邮件）。
面向用户的说明书是 [docs/gmail.md](docs/gmail.md)。

## 三条设计判断

1. **首发只读**（`gmail.readonly`）。标已读、发信要 `gmail.modify` / `gmail.send`，
   而 Gmail 属 Google 的 **restricted scope**——要得越多，安全评估越难过。
   等真有场景再加，别为了「以后可能用到」把整个应用卡在审核里。
2. **凭证是插件自有的**（和别的插件一样归接入组），只是获取方式是 OAuth：用户点一次「授权」→
   同意页 → refresh_token 落**平台**。插件拿到的只是平台现换的 access_token，
   **既没有 client_secret 也不经手 refresh_token**——这条边界是所有 OAuth 类插件的通例
   （见 `docs/dev-playbook.md` §4.4）。
3. **事件源用 History API 而不是轮询收件箱**（`history.go`）：拿上次的 historyId 问增量，
   比每轮拉一遍列表省得多，也不会漏掉「读完又来一封」。

## 文件

| 文件 | 干什么 |
|---|---|
| `schema/` | 操作/事件/凭证契约（**事实源**，改完 `go generate`） |
| `api.go` | Gmail REST 出站（鉴权、错误翻译、分页） |
| `message.go` | 邮件对象归一化（MIME 解析、正文/附件抽取） |
| `history.go` | 事件源：按 historyId 拉增量 |

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

## 没做的

发信、标已读、改标签——都要更大的作用域（见判断 1）。真要发信，用 SMTP 那条路更省事。
