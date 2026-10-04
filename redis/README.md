# redis — Redis 插件（第一方内置目录）

23 个操作覆盖六类数据结构（字符串/哈希/列表/集合/有序集合/Stream）+ `call` 保底 +
`health_check`（PING + INFO），外加事件源（Pub/Sub 订阅、Stream 消费）。
自建与云托管通吃（凭证里填地址）。说明书 [docs/redis.md](docs/redis.md)。

客户端是 `github.com/redis/go-redis/v9`（RESP 协议，没法拿 HTTP 凑）。

## 连接层（client.go）

**一份凭证一个 client，全进程复用**：go-redis 自带连接池，每次调用新建 client
等于每条命令前先握手 + AUTH（+TLS），密集写入时开销比命令本身还大。
缓存键是凭证指纹（sha256 前 8 字节）而不是明文——map 的键会在崩溃转储里露脸。
指纹算 addr/user/pass/db/tls 五项，少算一项就会串到别人的库上（测试钉着）。

## 事件源（events.go）

两条路并存，凭证填了哪条起哪条：

- **Pub/Sub**（watch_channels）：实时、不留存、**没有消息 id**。event_id 只能按
  「本进程第几条」生成——**不能用内容哈希**：同频道连发两条一样的消息是合法的
  （`tick`、`reload`），按内容去重会把第二条吃掉。重启后重新编号无所谓，
  Pub/Sub 本来就不重投。带通配的走 PSUBSCRIBE、其余走 SUBSCRIBE，分开是为了让
  `pattern` 出参只在真按通配订阅时才有值。
- **Stream**（watch_streams）：XREAD 阻塞读，`BLOCK 5s` 而不是 0（永久阻塞的连接
  要靠关连接才醒，ctx 取消时进程退不干净）。游标起点 `$`＝只读接上之后的新消息，
  不回溯历史（与 feed/gitlab 首轮约定一致）。event_id = `<stream>:<消息id>`，
  XADD 的 id 唯一且递增，重启重读也会被平台去重。

## 坑（改代码前先读）

- **「没有」不是错误**：取值未命中、出列表队列空、nx 没抢到——回出参（exists/count/ok），
  不回 error。抛错的话画布上整条流程被判失败，而那本该是一条正常分支。测试钉着。
- **契约给「取几条」而不是 Redis 原生的「结束下标」**：留空的数字字段到手就是 0，
  而 `stop=0` 在 Redis 里意思是「只要第一条」——空与 0 分不开就会悄悄只回一条。
  `count=0` 没有歧义（取 0 条无意义），拿它当「不限」是安全的。zset_range 同理。
- **EXPIRE 给非正数会当场删键**：插件挡在前面并让人改用「删键」，否则「续期」写成 0 就是删数据。
- **`INFO server memory` 是 Redis 7.0 才支持的多 section 形态**，6.x 上直接报错——
  体检不带参数取默认全量。INFO 解析的夹具用**真实例原文**（miniredis 的 INFO 残缺，
  拿它当夹具那条分支永远绿）。
- **TTL 走原始 `Do("TTL")`**：go-redis 的 Duration 形态把 -1/-2 表达成负纳秒，
  换算回哨兵值容易出错，而 -1（永久）/-2（不存在）正是这个操作要的东西。
- **写入的值一律字符串化**（`asString`）：JSON 数字 1000000 别打成 `1e+06`，
  这是 float64 直接 `%v` 的默认行为。
- 集群模式只有 0 号库，跨槽位多键命令可能被拒——说明书里写明。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

测试用 miniredis 起进程内实例打穿全部操作路径，不依赖外部服务。
