# Kubernetes

Operate Kubernetes clusters from the canvas: list pods, view container logs, perform rolling restarts, scale replicas, and inspect events and node health.
**Not tied to a cloud provider**: ACK, self-hosted clusters, and other managed clusters work the same way.

## 1. Prepare credentials

One credential represents one kubeconfig for one cluster:

- **Alibaba Cloud ACK**: use ACK · Export kubeconfig in the Alibaba Cloud plugin, choosing a private or public endpoint,
  then paste the output into this plugin's credential.
- **Self-hosted cluster**: paste `~/.kube/config`, the entire YAML document rather than only the server URL.
- Default namespace is optional. Operations use it when no namespace is supplied.

**Use a least-privilege ServiceAccount kubeconfig**, rather than an administrator's:

```yaml
# Example Role for reads plus Deployment restart/scaling; trim as needed.
rules:
- apiGroups: [""]
  resources: ["pods", "pods/log", "events", "nodes"]
  verbs: ["get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "deployments/scale"]
  verbs: ["get", "list", "patch"]
```

## 2. Common combinations

- **CrashLoop alerts**: schedule → List pods, abnormal only → count > 0 → Get the first pod's logs
  with Previous container logs enabled → LLM summary → Send to Feishu.
- **Node health**: schedule → List nodes → `not_ready > 0` → Alert.
- **Recovery after deployment**: receive an alert event → Restart Deployment → Wait two minutes → Recheck with List pods.
- **Nighttime scaling**: scale a Deployment to one replica at 22:00 daily and restore it at 08:00.
- **Deploy a service**: Deploy workload with image, environment variables, and replica count → Check readiness → Alert if not ready.
  A typical use is starting replicas of other platform plugins: use image `…/sokel-plugin-<name>:latest`
  and environment variables `SOKEL_ENDPOINT`, `SOKEL_TOKEN` (or `SOKEL_DEPLOY_KEY`), and `SOKEL_INSTANCE_ID`.
  The replica registers with the platform after starting.
- **Run a one-off task**: Run job with image and command. By default it waits for completion and returns the log tail → LLM summary or alert.
  Use this for data processing, scripts, and migrations. Failed jobs are not retried and are cleaned up one hour after completion.
- **Other resources**: for Service, ConfigMap, CronJob, Ingress, and others, use Apply YAML manifest.
  It follows kubectl apply semantics, supports multiple documents separated by `---`, and creates missing objects or updates existing ones.
  Use Delete object for any object by kind and name; when apiVersion is omitted, common groups are detected automatically.

### Additional RBAC for writes

Deploy workload, Run job, Apply manifest, and Delete object need these permissions in addition to the read-only Role:
`create/patch/delete` on `deployments`, `jobs`, and any resources you apply.
A dedicated namespace for plugin-managed deployments is recommended, with the Role restricted to it.

### Watch abnormal events: eviction, scheduling failure, and abnormal exits

**Event triggers are recommended**. Set Namespaces to watch in the credential, such as `prod,staging`,
then choose an event in the Kubernetes trigger on the canvas:

| Event | When it fires | Evidence |
|---|---|---|
| **Pod evicted** | The kubelet evicts a pod because the node lacks resources. | Event `reason=Evicted`. |
| **Scheduling failed** | A pod cannot start because no node can accommodate it. | Event `reason=FailedScheduling`. |
| **Container exited abnormally** | OOMKilled or a nonzero exit code. | **Pod status**, not an Event; see below. |
| **Node unhealthy** | Ready is not True. | Node conditions. |

Four rules to know before configuration:

- **About one-minute latency**, using polling. Kubernetes does not send outbound HTTP proactively, so there is no platform webhook relay route.
- **The first poll after startup does not replay historical abnormalities**, avoiding a flood of old alerts immediately after installation.
  Wait a minute after configuration, then create a new abnormal condition to verify it.
- **Abnormal container exits are detected from Pod status, not Events**. OOMKilled often **does not emit an Event** because the container is immediately restarted;
  the evidence is in `lastState`. Watching only Events would miss this common case.
- **Unhealthy nodes are reported only on state changes**. Reporting every poll would produce hundreds of alerts for a node unhealthy overnight.
  If it recovers and becomes unhealthy again, a new event is reported.

Leave Namespaces to watch empty to disable the event source. You can then use the scheduled approach below.

---

**Alternative without an event source**, or when you want to control the cadence:

Schedule, preferably every ten minutes or less → List events → Evaluate conditions → Notify.

Three parameters support this scenario:

| Parameter | Purpose |
|---|---|
| Only these reasons | Examples: `Evicted`, `FailedScheduling`, `BackOff`, `Unhealthy`. |
| Only after this time | Pass **the previous run's `checked_at`**; otherwise, each run reports the same alerts again. |
| Truncated | Output. If true, more matches exist than `limit` allowed and **some were not returned**. Increase the limit or shorten the interval. |

⚠️ **Kubernetes Events are retained for only one hour by default**. Poll well within that interval to avoid missing them.

**Use List pods for OOMKilled, not List events**. An OOM-killed container restarts immediately;
its current status may already be Running with no abnormal reason. The useful evidence is in these outputs:

- **Last termination reason** = `OOMKilled` → insufficient memory; adjust limits.
- **Last exit code** = `137` → killed by SIGKILL, typical of OOM.
- **Last termination time** → distinguish a recent incident from an old one.

Restart count alone shows repeated restarts but cannot distinguish memory pressure, a code crash, or eviction,
which require different remedies.

## 3. Troubleshooting

| Error | Resolution |
|---|---|
| Failed to parse kubeconfig | Paste the **entire YAML**, not just the server URL. |
| Cluster rejected the kubeconfig (401) | The certificate or token expired. Export it again; ACK kubeconfigs have expiration dates. |
| RBAC denied (403) | Grant permissions to the kubeconfig's identity; see the Role example above. |
| Failed to connect to the cluster | A private endpoint requires network connectivity between the plugin container and cluster. Use a public endpoint across networks. |
| Pod has multiple containers | Specify one in Container name. |

## 4. Security

A kubeconfig grants access to the cluster. It is used only inside the plugin, not in node inputs, outputs, or logs.
The only write operations are restart and scaling. Deletion operations are deliberately not provided; do not grant those permissions to this kubeconfig either.
