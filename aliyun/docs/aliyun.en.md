# Alibaba Cloud

Connect the Alibaba Cloud control plane to workflows: query SLS logs, inspect RDS status and slow SQL, change DNS records,
manage ACK clusters, and query CloudMonitor metrics. For operations **inside** a cluster (listing pods, viewing container logs,
or restarting services), use the Kubernetes plugin. This plugin's Export kubeconfig operation retrieves the credential it needs.

## 1. Prepare credentials (a dedicated RAM user is recommended)

1. Open the [RAM console](https://ram.console.aliyun.com/users) → Create User → enable OpenAPI access.
   Copy the AccessKey ID and Secret into this plugin's credential.
2. **Grant permissions** to that user. Avoid the root account's AccessKey and do not start with AdministratorAccess; grant permissions for the operations you use.

**Read-only starting policy** (logs, instances, DNS records, clusters, and metrics; recommended initially):

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

**Additional write permissions as needed** (add only when using the corresponding operation):

| Operation | Permission to add |
|---|---|
| Add/update/delete DNS records | `alidns:AddDomainRecord`, `alidns:UpdateDomainRecord`, `alidns:DeleteDomainRecord` |
| Export kubeconfig | `cs:DescribeClusterUserKubeconfig` (already included in the read-only policy) |
| Mobile push | `push:Push` (EMAS mobile push sending permission) |

3. Set Default Region in the credential to the region containing your resources, such as `cn-hangzhou`. RDS, SLS, and CloudMonitor use it by default.

## 2. Common combinations

- **Error log alerts**: schedule → SLS · Query logs (`level:ERROR | select count(*) as c`, last five minutes)
  → condition node (c > threshold) → send an alert through Feishu or a webhook.
- **RDS capacity checks**: schedule → CloudMonitor · Query metrics (`acs_rds_dashboard` / `DiskUsage`, with an instance ID)
  → notify when usage exceeds 80%.
- **Daily slow SQL report**: every day at 09:00 → RDS · Slow SQL (last day) → summarize with an LLM → send to a group.
- **Certificate DNS-01 verification**: DNS · Add record (`_acme-challenge` TXT) → verify → DNS · Delete record.
- **Inspect a cluster**: ACK · Export kubeconfig → paste it into a Kubernetes plugin credential → list pods, view logs, or restart there.
- **App push**: Mobile push · Push. Enter the numeric AppKey from the EMAS console and target devices, accounts, aliases, tags, or all users.
  A notification appears in the notification tray; a message is passed to application code. For delivery statistics, use the generic call operation with `QueryPushStatByMsg` and the `message_id`.

## 3. Products without typed operations: generic calls

Use Generic call for RPC-style OpenAPIs, including ECS, SLB, and the OSS control plane. Follow the product's API documentation
and supply `endpoint` (for example, `ecs.cn-hangzhou.aliyuncs.com`), `action`, `version`, and a parameter object.
The plugin handles signing. High-risk operations such as restarting or deleting instances **are deliberately available only through this route**,
requiring explicit parameters.

## 4. Troubleshooting

| Error | Resolution |
|---|---|
| Alibaba Cloud does not recognize the AccessKey | Check the RAM console: the key may be incorrect or deleted. |
| Signature verification failed | Paste the Secret again; check for extra spaces or missing characters. |
| The RAM user lacks permission for X | Add the permission shown above to the RAM user. |
| SLS project does not exist | Projects are **regional** resources; the wrong region produces this error too. |
| CloudMonitor returns no data points | Use the namespace and metric name from the [metric documentation](https://help.aliyun.com/document_detail/163515.html); the instance in `Dimensions` must actually be monitored. |
| EMAS does not recognize the AppKey | Use the application's **numeric AppKey** from the EMAS console, not an AccessKey. The app must belong to the current account. |
| Push accepted, but the app shows nothing | For iOS, check that the production/development push environment matches the signing certificate. For Android vendor channels, configure each vendor's keys in the EMAS console. |

## 5. Security

The AccessKey Secret is used only for signing inside the plugin; it is not included in node inputs, outputs, or logs.
An exported kubeconfig **grants access to the cluster**. Do not connect its output to nodes that transmit it externally or persist it in a database.
