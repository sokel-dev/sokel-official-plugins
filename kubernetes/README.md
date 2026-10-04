# kubernetes — 通用 K8s 插件（第一方自部署）

8 个操作：pods（含只看异常）、pod 日志（含上一世）、deployments、滚动重启、扩缩副本、
事件（Warning 优先）、节点（含 not_ready 计数）、health_check（/version）。
面向用户的说明书是 [docs/kubernetes.md](docs/kubernetes.md)。

## 架构判断

- **不绑云**：凭证就是一份 kubeconfig。ACK 用 aliyun 插件的 ack_kubeconfig 一键导出；
  自建/其他托管集群同样能用。
- **client-go 只用底层两包**：`clientcmd`（解析 kubeconfig：证书/token/exec 插件）+
  `rest.TransportFor`（拿配好 mTLS/Bearer 的 RoundTripper）。资源读写是普通 REST，
  7 个路径自己拼——不引 typed clientset 全家桶（几百个生成类型 vs 我们 7 个路径）。
  这符合「飞书先例」的 SDK 放行标准：只把裸写不现实的部分（认证）交给 SDK。
- **写操作只做两个日常的**：滚动重启（= kubectl rollout restart，改模板注解）与扩缩副本
  （PATCH /scale，先读原值——审计与回滚都要「从几到几」）。删 pod/drain 这类高危不做，
  权限控制点在集群 RBAC。

## 坑（改代码前先读）

- **client-go 对明文 http:// 的 server 会静默丢 token**（不让凭证走明文）。
  真集群全是 TLS 碰不到；**测试的假 API server 必须 NewTLSServer + insecure-skip-tls-verify**，
  否则「认证头带上了吗」这条测试静默测不到东西——踩过。
- 多容器 pod 不指定容器名，K8s 报 "a container name must be specified"——已翻译成人话。
- 事件 K8s 按时间正序给，最近的在最后——输出前倒序，看事件的人要的是最近的。
- kubeconfig 按内容哈希缓存 client：TLS 握手与 exec 凭证插件都不便宜。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

### 异常事件监控：为什么是这三个字段

`pods` 的 `last_terminated_reason` / `last_exit_code` / `last_terminated_at` 单独存在，
是因为 **OOMKilled 只出现在 `lastState` 里**：容器被杀掉后会被立刻拉起，
当前 `state` 已经是 Running，`reason` 里看不到任何东西。只报 `restarts` 的话，
调用方知道它在反复重启，却分不清内存不够 / 代码崩了 / 被驱逐——三者处理完全不同。

多容器时取**最近结束的那个**：报最早那次会把人指向一个已经修好的问题。

`events` 的 `since` 按 **lastTimestamp**（最近发生）比，**不是 firstTimestamp**。
k8s 对重复事件不新建条目，而是 `count++` 并更新 `lastTimestamp`——按首次发生比的话，
一个持续两小时的故障只会在第一轮被报出来，之后永远比游标旧（症状：告警只响一次就没了）。

三个时间字段都要认（`lastTimestamp` / `eventTime` / `firstTimestamp`）：
新版 `events.k8s.io` 的集群只给 `eventTime`，只认一个会让那一半集群上的过滤恒不生效、
且「最近发生」为空——而那正是调用方拿去当游标的字段。

`truncated` 是因为此前静默截断：命中比 `limit` 多时直接丢掉且不吭声，集群一忙就会悄悄漏告警。

### 事件源的四条纪律

与 GitHub 那份事件源同构（那边先踩过），每条对应一种「不报错但没用」：

1. **首轮不触发**。第一次拉到的是集群当前所有历史异常，照发的话装上插件的第一分钟
   就有几十条陈年告警涌进工作流——用户的第一反应是把触发器关掉，那比没有更糟。
2. **崩溃按「结束时刻」去重**。同一次崩溃在 Pod 被替换前会被连着好几轮看到，
   不去重就是每分钟一条。
3. **节点只在状态变化时报**。它会一直 NotReady 下去，每轮报一次能刷出几百条。
   恢复时要清掉记录，否则第二次坏掉静悄悄。
4. **报 Ready 条件的原值而不是布尔**。`False`（节点自报不健康）与 `Unknown`（kubelet
   失联）压成布尔就没区别了，可一个是去看节点上发生了什么、一个是先确认机器还在不在。

**三个 poll 函数收的是接口 `plugin.SourceCtx`，不是 `sokel.SourceCtx` 那个结构体。**
结构体假不出来，收结构体的话轮询路径会一行测不到——GitHub 那份正是如此，
它只测到了 webhook（那个收的恰好是接口）。收窄到接口是为了能装齿。

**崩溃判据取自 Pod 状态而不是 Event**：OOMKilled 多半不发 Event。

## 部署与任务五操作（workload_ops.go，2026-08-29 增）

- 写路径统一 **server-side apply**（PATCH + `application/apply-patch+yaml`，fieldManager=sokel）：
  幂等，工作流重跑不炸「已存在」；created/configured 靠 apply 前探一次 GET 区分。
- kind → REST 资源名**不猜复数**，问 `/apis/<gv>` 的 discovery 资源表并按 client+gv 缓存
  （`deployments/scale` 这类子资源要跳过）。
- deploy_workload 的 `app` 标签是选择器锚点：用户 labels 里的 `app` 会被丢弃——
  选择器建后不可改，改了 apply 直接被拒。
- run_job：`backoffLimit=0`（失败不重试，重试语义交给工作流层）、`ttlSecondsAfterFinished=3600`、
  等待上限压到 570s（操作 TimeoutSec 600 留 30s 取日志）；日志拿不到不算错（佐料不是主菜）。

## 没做的

- **watch 长连接**：秒级且不漏，但要处理断线重连与 410 Gone（resourceVersion 过期时
  必须重新 list 再 watch，否则静默漏掉中间的全部变更）。轮询先跑通链路。
