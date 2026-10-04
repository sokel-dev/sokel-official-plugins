# 阿里云

把阿里云管控面接进工作流：查 SLS 日志、看 RDS 状态与慢 SQL、改 DNS 解析、
管 ACK 集群、查云监控指标。集群**内部**的操作（查 pod/看容器日志/重启服务）
用「Kubernetes」插件——本插件的「导出 kubeconfig」一键取到它要的凭证。

## 一、准备凭证（建议专用 RAM 用户）

1. [RAM 控制台](https://ram.console.aliyun.com/users) → 创建用户 → 勾「OpenAPI 调用访问」，
   拿到 AccessKey ID / Secret → 粘进本插件凭证。
2. 给该用户**授权**。别用主账号 AK，也别一上来就 AdministratorAccess——按用到的操作给：

**只读起步**（查日志/看实例/看解析/看集群/看指标，推荐从这套开始）：

```json
{
  "Version": "1",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "log:Get*", "log:List*",
      "rds:Describe*",
      "alidns:Describe*",
      "cs:Describe*", "cs:Get*",
      "cms:Describe*", "cms:Query*"
    ],
    "Resource": "*"
  }]
}
```

**按需追加的写权限**（用到对应操作才加）：

| 操作 | 加什么 |
|---|---|
| DNS 加/改/删记录 | `alidns:AddDomainRecord`、`alidns:UpdateDomainRecord`、`alidns:DeleteDomainRecord` |
| 导出 kubeconfig | `cs:DescribeClusterUserKubeconfig`（在只读集里已含） |
| 移动推送 | `push:Push`（EMAS 移动推送发送权限） |

3. 凭证里的「默认 Region」填你资源所在地（如 `cn-hangzhou`）；RDS/SLS/云监控默认查它。

## 二、常用组合

- **错误日志告警**：定时触发 → SLS·查日志（`level:ERROR | select count(*) as c`，最近 5 分钟）
  → 条件节点（c > 阈值）→ 飞书/webhook 发告警。
- **RDS 水位巡检**：定时触发 → 云监控·查指标（`acs_rds_dashboard` / `DiskUsage`，填实例 ID）
  → 超 80% 发通知。
- **慢 SQL 日报**：每天 9 点 → RDS·慢SQL（最近 1 天）→ LLM 总结 → 发群。
- **证书 DNS-01**：DNS·加记录（`_acme-challenge` TXT）→ 验证 → DNS·删记录。
- **集群下钻**：ACK·导出 kubeconfig → 粘进 Kubernetes 插件凭证 →（那边）查 pod / 看日志 / 重启。
- **App 推送**：移动推送·推送——填 EMAS 控制台的数字 AppKey，按设备/账号/别名/标签圈人或全量广播；
  「通知」弹通知栏、「消息」透传给应用代码。送达统计用 call 调 `QueryPushStatByMsg`（认 message_id）。

## 三、typed 之外的产品：通用调用

ECS/SLB/OSS 管控等所有 RPC 风格 OpenAPI 都能用「通用调用」直调：照产品 API 文档填
`endpoint`（如 `ecs.cn-hangzhou.aliyuncs.com`）、`action`、`version`、参数对象。
签名由插件管。高危操作（重启/删实例）**故意只留这条路**——要显式拼参数才做得了。

## 四、排错

| 报错 | 处理 |
|---|---|
| 阿里云不认这个 AccessKey | AK 填错或已删，RAM 控制台核对 |
| 签名校验失败 | Secret 粘错（多空格/少字符）重粘一遍 |
| RAM 用户没有 X 的权限 | 给 RAM 用户加上面表里的授权 |
| SLS project 不存在 | project 是 **region 级**资源——region 错了也报这个 |
| 云监控查不到点 | 命名空间/指标名照[指标文档](https://help.aliyun.com/document_detail/163515.html)；`Dimensions` 的实例要真在采集 |
| EMAS 不认这个 AppKey | 用 EMAS 控制台该 App 的**数字 AppKey**（不是 AccessKey），App 要属于当前账号 |
| 推送收单但 App 没弹 | iOS 核对推送环境（生产/开发要与打包证书一致）；Android 厂商通道要在 EMAS 控制台配好各厂商密钥 |

## 五、安全

AccessKey Secret 只在插件内部签名，不进节点入参/输出/日志。kubeconfig 的产出
**是能进集群的钥匙**——别把「导出 kubeconfig」的输出接到会外发或落库的节点上。
