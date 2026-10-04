# Kubernetes

在画布上操作 K8s 集群：查 pod、看容器日志、滚动重启、扩缩副本、看事件与节点健康。
**不绑任何云**——ACK、自建、其他托管集群都一样用。

## 一、准备凭证

一条凭证 = 一个集群的一份 kubeconfig：

- **阿里云 ACK**：用「阿里云」插件的「ACK·导出 kubeconfig」操作一键取（私网/公网端点可选），
  产出直接粘进本插件凭证。
- **自建集群**：粘 `~/.kube/config`（整份 YAML，不是只粘 server 地址）。
- 「默认命名空间」选填：操作里没写命名空间时用它。

**建议用最小权限的 ServiceAccount** 出 kubeconfig，而不是 admin 的：

```yaml
# 只读 + 重启/扩缩 deployment 的 Role 示例（按需裁剪）
rules:
- apiGroups: [""]
  resources: ["pods", "pods/log", "events", "nodes"]
  verbs: ["get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "deployments/scale"]
  verbs: ["get", "list", "patch"]
```

## 二、常用组合

- **CrashLoop 告警**：定时触发 → Pod 列表（只看异常）→ count > 0 → 取第一个的
  Pod 日志（勾「上一次崩溃的日志」）→ LLM 摘要 → 发飞书。
- **节点健康**：定时触发 → 节点列表 → `not_ready > 0` → 告警。
- **发版后自愈**：收到告警事件 → 重启 Deployment → 等 2 分钟 → Pod 列表复查。
- **夜间缩容**：每天 22:00 扩缩 Deployment 到 1；早 8:00 调回。
- **部署一个服务**：部署工作负载（镜像/环境变量/副本数）→ 就绪状态 → 未就绪则告警。
  典型用法是把平台的其它插件副本拉起来：镜像给 `…/sokel-plugin-<名>:latest`，
  环境变量给 `SOKEL_ENDPOINT` / `SOKEL_TOKEN`（或 `SOKEL_DEPLOY_KEY`）/ `SOKEL_INSTANCE_ID`，
  起来后副本自动到平台报到。
- **跑一次性任务**：运行一次性任务（镜像 + 命令，默认等跑完带回日志尾部）→ LLM 摘要/告警。
  数据处理、脚本、迁移都是它；失败不重试，跑完 1 小时自动清理。
- **别的资源**：Service / ConfigMap / CronJob / Ingress… 用「应用 YAML 清单」兜底
  （kubectl apply 语义，支持 `---` 多文档，不存在则建、存在则改）；
  删除任何对象用「删除对象」（kind + 名字，apiVersion 留空自动探测常见组）。

### 写操作的 RBAC 增量

「部署工作负载 / 运行一次性任务 / 应用清单 / 删除对象」需要在只读 Role 之上加：
`deployments`、`jobs` 及你要 apply 的资源的 `create/patch/delete`。
建议单独建一个 namespace 收纳插件部署的东西，Role 限定在里面。

### 盯异常事件（驱逐 / 调度失败 / 异常退出）

**推荐用事件触发器** —— 凭证里填「盯哪些命名空间」（如 `prod,staging`），
然后在画布上用 Kubernetes 触发器选事件：

| 事件 | 什么时候触发 | 判据来自 |
|---|---|---|
| **Pod 被驱逐** | 节点资源不足，kubelet 赶走了 Pod | Event `reason=Evicted` |
| **调度失败** | Pod 起不来，没节点放得下 | Event `reason=FailedScheduling` |
| **容器异常退出** | OOMKilled / 非零退出码 | **Pod 状态**（不是 Event，见下） |
| **节点不健康** | Ready 不为 True | 节点条件 |

四条纪律（配之前先知道）：

- **约 1 分钟延迟**，轮询。k8s 不会主动往外发 HTTP，所以没有「平台代收 Webhook」那条路。
- **插件刚启动的第一轮不补发历史异常** —— 否则装上就会有几十条陈年告警涌进来。
  所以配好后要等一分钟，再去制造一个新异常来验证。
- **容器异常退出看的是 Pod 状态不是事件**：OOMKilled 多半**不发 Event**（容器被杀后立刻
  被拉起），痕迹只在 `lastState` 里。盯 Event 会把最常见的那一类整个漏掉。
- **节点只在状态变化时报一次**：一个坏了一夜的节点，每轮报一次能刷出几百条。
  恢复后再坏会重新报。

留空「盯哪些命名空间」= 不启动事件源，此时只能用下面这条定时拉的路。

---

**没有事件源时的替代**（或想自己控制节奏时）：

定时触发（建议 ≤10 分钟）→ 事件列表 → 判断 → 通知。

三个参数是为这个场景加的：

| 参数 | 干什么 |
|---|---|
| **只看这些原因** | 填 `Evicted`（被驱逐）、`FailedScheduling`（资源不够排不上）、`BackOff`、`Unhealthy` |
| **只要这个时间之后的** | 把**上一轮的 `checked_at`** 填进来，否则每轮都会把同一批告警再报一遍 |
| **被截断了** | 出参。为 true 说明命中的比 `limit` 多，**还有没返回的** —— 看到它就调大 limit 或缩短间隔 |

⚠️ **k8s 的 Event 默认只留 1 小时**，轮询间隔必须明显短于它，否则会漏。

**OOMKilled 要看 Pod 列表，不是事件列表。** 容器被 OOM 杀掉后会立刻被拉起，
当前状态已经是 Running，「异常原因」里什么都看不到；真正的线索在这三个出参：

- **上次结束原因** = `OOMKilled` → 内存不够，调 limits
- **上次退出码** = `137` → 被 SIGKILL（OOM 的典型特征）
- **上次结束时间** → 分清是刚刚发生还是上周的历史遗留

只看「重启次数」的话，你知道它在反复重启，但分不清是内存不够、代码崩了、还是被驱逐 ——
而这三者的处理完全不同。

## 三、排错

| 报错 | 处理 |
|---|---|
| kubeconfig 解析失败 | 要粘**整份 YAML**，不是 server 地址 |
| 集群拒绝了这份 kubeconfig（401） | 证书/token 过期，重新导出（ACK 的 kubeconfig 有有效期） |
| RBAC 不允许（403） | 给 kubeconfig 对应的主体加权限（见上面的 Role 示例） |
| 连接集群失败 | 私网端点要求插件容器与集群网络可达；跨网用公网端点 |
| 这个 pod 有多个容器 | 在「容器名」里指定一个 |

## 四、安全

kubeconfig 是进集群的钥匙，只在插件内部使用，不进节点入参/输出/日志。
写操作只有重启与扩缩两个；删除类操作有意不提供——也别给这份 kubeconfig 那些权限。
