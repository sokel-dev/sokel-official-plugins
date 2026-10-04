// Package schema declares the kubernetes plugin's operation and credential contracts.
//
// Scope: generic K8s workload operations -- list pods / view logs / restart and scale
// deployments / events / nodes. Not tied to any cloud: the credential is just a kubeconfig,
// used the same way whether it's ACK, self-hosted, or someone else's managed offering (ACK's
// kubeconfig can be one-click exported via the aliyun plugin's ack_kubeconfig).
//
// Implemented via kubeconfig parsing + raw REST (client-go only uses the two low-level
// packages clientcmd/rest, the typed clientset family isn't pulled in): implementing
// auth/mTLS/exec plugins ourselves isn't realistic, while resource reads/writes are themselves
// plain REST -- this follows the same SDK admission bar set by the Feishu precedent.
//
// Write operations only cover the two everyday ones: restarting a deployment (rollout restart)
// and scaling replica count. High-risk actions like deleting a pod/deployment or draining a
// node aren't made typed operations -- and RBAC shouldn't grant this kubeconfig's principal
// those permissions either (the permission control point is cluster RBAC; the plugin doesn't
// invent its own).
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// nsField is the namespace field (shared by most operations).
func nsField() contract.FieldSpec {
	return field.String("namespace").Label("命名空间").Desc("留空用凭证里的默认命名空间（再没有则 default）").Optional()
}

// Pods lists pods.
type Pods struct{}

func (Pods) Meta() contract.Meta {
	return contract.Meta{ID: "pods", Label: "Pod 列表",
		Desc: "列出命名空间里的 pod（状态/重启次数/所在节点），可按标签过滤"}
}

func (Pods) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("label_selector").Label("标签过滤").Desc("如 app=web,tier!=cache").Optional(),
		field.Bool("only_abnormal").Label("只看异常").Desc("只留非 Running/Succeeded 或有容器在重启的").Default(false),
	}
}

func (Pods) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("pods", []Pod{}).Label("Pod 列表"),
		field.Int("count").Label("个数"),
	}
}

// Pod is a single pod's summary.
type Pod struct {
	Name     string `sokel:"name" label:"名称"`
	Phase    string `sokel:"phase" label:"状态" desc:"Running/Pending/Failed/…"`
	Ready    string `sokel:"ready" label:"就绪" desc:"如 2/2"`
	Restarts int    `sokel:"restarts" label:"重启次数"`
	Node     string `sokel:"node,optional" label:"节点"`
	Age      string `sokel:"age,optional" label:"存活时长"`
	Reason   string `sokel:"reason,optional" label:"异常原因" desc:"CrashLoopBackOff / ImagePullBackOff 等；正常为空"`
	// LastTerminatedReason: how the container ended the previous time.
	//
	// This field exists on its own because OOMKilled mostly only shows up here: after being
	// OOM-killed, the container is restarted immediately, so the current state is Running (or
	// Waiting/CrashLoopBackOff), with no sign of OOM in reason at all. Looking only at restarts
	// tells you it's restarting repeatedly, but not whether it's out of memory, crashed, or
	// evicted -- and these three need completely different handling (add memory / fix code / look
	// at the node).
	LastTerminatedReason string `sokel:"last_terminated_reason,optional" label:"上次结束原因" desc:"OOMKilled / Error / Completed…；从没重启过则为空"`
	LastExitCode         int    `sokel:"last_exit_code,optional" label:"上次退出码" desc:"137 常见于 OOM 或被 SIGKILL；0 = 正常退出"`
	LastTerminatedAt     string `sokel:"last_terminated_at,optional" label:"上次结束时间" desc:"用它判断是刚刚发生还是历史遗留"`
}

// PodLogs views logs.
type PodLogs struct{}

func (PodLogs) Meta() contract.Meta {
	return contract.Meta{ID: "pod_logs", Label: "Pod 日志", TimeoutSec: 60,
		Desc: "取一个 pod 的日志尾部（多容器要指定容器名）"}
}

func (PodLogs) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("pod").Label("Pod 名"),
		field.String("container").Label("容器名").Desc("单容器 pod 留空").Optional(),
		field.Int("tail_lines").Label("最后几行").Desc("默认 200，上限 5000").Optional(),
		field.Bool("previous").Label("上一次崩溃的日志").Desc("容器刚重启过时看上一世的日志——排查 CrashLoop 用").Default(false),
	}
}

func (PodLogs) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("logs").Label("日志"),
		field.Int("lines").Label("行数"),
	}
}

// Deployments lists deployments.
type Deployments struct{}

func (Deployments) Meta() contract.Meta {
	return contract.Meta{ID: "deployments", Label: "Deployment 列表",
		Desc: "列出命名空间里的 deployment（期望/就绪副本数、镜像）"}
}

func (Deployments) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{nsField()}
}

func (Deployments) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("deployments", []Deployment{}).Label("Deployment 列表"),
		field.Int("count").Label("个数"),
	}
}

// Deployment is a single deployment's summary.
type Deployment struct {
	Name      string   `sokel:"name" label:"名称"`
	Replicas  int      `sokel:"replicas" label:"期望副本"`
	Ready     int      `sokel:"ready" label:"就绪副本"`
	Available int      `sokel:"available,optional" label:"可用副本"`
	Images    []string `sokel:"images,optional" label:"镜像"`
}

// DeploymentRestart performs a rolling restart.
type DeploymentRestart struct{}

func (DeploymentRestart) Meta() contract.Meta {
	return contract.Meta{ID: "deployment_restart", Label: "重启 Deployment",
		Desc: "滚动重启（等价 kubectl rollout restart）：改 pod 模板注解触发滚动替换，不中断服务"}
}

func (DeploymentRestart) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("name").Label("Deployment 名"),
	}
}

func (DeploymentRestart) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("已触发"),
		field.String("restarted_at").Label("触发时间"),
	}
}

// DeploymentScale scales replica count.
type DeploymentScale struct{}

func (DeploymentScale) Meta() contract.Meta {
	return contract.Meta{ID: "deployment_scale", Label: "扩缩 Deployment",
		Desc: "把副本数调到指定值（等价 kubectl scale）。缩到 0 = 停掉这个服务，确认想清楚"}
}

func (DeploymentScale) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("name").Label("Deployment 名"),
		field.Int("replicas").Label("目标副本数"),
	}
}

func (DeploymentScale) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("previous").Label("原副本数"),
	}
}

// Events lists events.
type Events struct{}

func (Events) Meta() contract.Meta {
	return contract.Meta{ID: "events", Label: "事件列表",
		Desc: "命名空间最近的事件（调度失败/OOMKilled/探针失败…），Warning 在前"}
}

func (Events) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.Bool("only_warning").Label("只看 Warning").Default(true),
		field.Int("limit").Label("最多几条").Desc("默认 50").Optional(),
		field.String("since").Label("只要这个时间之后的").
			Desc("RFC3339（如 2026-08-28T10:00:00Z）。**轮询场景必填**：把上一轮的 checked_at 传进来，" +
				"否则每轮都会把同一批告警再查一遍。按事件的「最近发生」时间比，不是首次发生").Optional(),
		field.Strings("reasons").Label("只看这些原因").
			Desc("如 Evicted / FailedScheduling / BackOff / Unhealthy；留空 = 全部。" +
				"关注特定异常时用它，比拿回来自己过滤省一半篇幅").Optional(),
	}
}

func (Events) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("events", []Event{}).Label("事件列表"),
		field.Int("count").Label("条数"),
		field.Bool("truncated").Label("被截断了").
			Desc("true = 命中的比 limit 多，**还有没返回的**。轮询时看到它就该调大 limit 或缩短间隔——" +
				"此前静默截断，集群一忙就会悄悄漏掉告警").Optional(),
		field.String("checked_at").Label("本次查询时刻").
			Desc("RFC3339。下一轮把它填进 since，就是一条不重不漏的游标").Optional(),
	}
}

// Event is a single event.
type Event struct {
	Type      string `sokel:"type" label:"级别" desc:"Normal/Warning"`
	Reason    string `sokel:"reason" label:"原因"`
	Object    string `sokel:"object" label:"对象" desc:"如 Pod/web-abc123"`
	Message   string `sokel:"message" label:"内容"`
	Count     int    `sokel:"count,optional" label:"发生次数"`
	LastSeen  string `sokel:"last_seen,optional" label:"最近发生"`
	FirstSeen string `sokel:"first_seen,optional" label:"首次发生"`
}

// Nodes lists nodes.
type Nodes struct{}

func (Nodes) Meta() contract.Meta {
	return contract.Meta{ID: "nodes", Label: "节点列表",
		Desc: "集群节点与健康状态（Ready 条件、K8s 版本、容量）"}
}

func (Nodes) Inputs() []contract.FieldSpec { return nil }

func (Nodes) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("nodes", []Node{}).Label("节点列表"),
		field.Int("count").Label("个数"),
		field.Int("not_ready").Label("异常节点数").Desc("Ready 条件非 True 的个数——告警判断直接用它"),
	}
}

// Node is a single node.
type Node struct {
	Name  string `sokel:"name" label:"名称"`
	Ready bool   `sokel:"ready" label:"就绪"`
	// ReadyStatus: the Ready condition's raw value, not a boolean.
	// False (the node self-reports unhealthy) and Unknown (kubelet simply stopped reporting,
	// usually meaning it's unreachable or down) are both "not ready" as a boolean, but need
	// completely different handling: the former means go look at what's happening on the node,
	// the latter means first confirm the machine is even there. Collapsing it to a boolean loses
	// this distinction.
	ReadyStatus string `sokel:"ready_status,optional" label:"Ready 原值" desc:"True / False / Unknown"`
	Reason      string `sokel:"reason,optional" label:"原因" desc:"如 KubeletNotReady / NodeStatusUnknown"`
	Version     string `sokel:"version,optional" label:"kubelet 版本"`
	CPU         string `sokel:"cpu,optional" label:"CPU 容量"`
	Memory      string `sokel:"memory,optional" label:"内存容量"`
	Taints      int    `sokel:"taints,optional" label:"污点数"`
}

// HealthCheck is the platform's standard credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "解析 kubeconfig 并调 /version——通了会显示集群版本"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("version").Label("集群版本"),
		field.String("message").Label("说明"),
	}
}

// DeployWorkload creates or updates a Deployment.
type DeployWorkload struct{}

func (DeployWorkload) Meta() contract.Meta {
	return contract.Meta{ID: "deploy_workload", Label: "部署工作负载", TimeoutSec: 60,
		Desc: "创建或更新一个 Deployment（server-side apply：不存在则建，存在则按给的字段改）。" +
			"典型用法：把插件副本/自有服务拉起来，配合「就绪状态」确认起没起来"}
}

func (DeployWorkload) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("name").Label("名称").Desc("Deployment 名，也是 app 标签值（选择器锚它，建后不可改）"),
		field.String("image").Label("镜像").Desc("如 registry.example.com/app:v1"),
		field.Int("replicas").Label("副本数").Default(1),
		field.Json("env", map[string]string{}).Label("环境变量").Desc("键值对；值里可绑上游变量").Optional(),
		field.Json("labels", map[string]string{}).Label("附加标签").Desc("app 标签平台自动打，别在这里覆盖").Optional(),
		field.Strings("command").Label("启动命令").Desc("覆盖镜像入口（含参数，逐项一个元素）；留空 = 用镜像默认").Optional(),
		field.String("image_pull_secret").Label("拉取密钥").Desc("私有镜像仓库的 imagePullSecret 名（需已在目标命名空间建好）").Optional(),
		field.String("cpu_limit").Label("CPU 上限").Desc("如 500m；留空不设").Optional(),
		field.String("memory_limit").Label("内存上限").Desc("如 512Mi；留空不设").Optional(),
	}
}

func (DeployWorkload) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Bool("created").Label("是否新建").Desc("false = 更新了已有的"),
		field.String("name").Label("名称"),
		field.String("namespace").Label("命名空间"),
	}
}

// DeploymentStatus is the readiness status.
type DeploymentStatus struct{}

func (DeploymentStatus) Meta() contract.Meta {
	return contract.Meta{ID: "deployment_status", Label: "就绪状态",
		Desc: "一个 Deployment 起没起来：期望/就绪/可用副本数与镜像。部署后接它判断是否就绪"}
}

func (DeploymentStatus) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("name").Label("Deployment 名"),
	}
}

func (DeploymentStatus) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ready").Label("已就绪").Desc("就绪副本数 = 期望副本数且 ≥1"),
		field.Int("desired").Label("期望副本"),
		field.Int("ready_replicas").Label("就绪副本"),
		field.Int("available").Label("可用副本"),
		field.String("image").Label("当前镜像"),
		field.String("message").Label("说明").Desc("未就绪时给出最近的 condition 原因"),
	}
}

// RunJob runs a one-off task.
type RunJob struct{}

func (RunJob) Meta() contract.Meta {
	return contract.Meta{ID: "run_job", Label: "运行一次性任务", TimeoutSec: 600,
		Desc: "起一个 Job 跑一次性任务（数据处理/脚本/迁移）。默认等它跑完并带回日志尾部；" +
			"失败不重试（backoffLimit=0），跑完 1 小时后自动清理"}
}

func (RunJob) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("name_prefix").Label("任务名前缀").Desc("实际 Job 名 = 前缀-时间戳（避免重名）"),
		field.String("image").Label("镜像"),
		field.Strings("command").Label("命令").Desc("含参数，逐项一个元素；留空 = 用镜像默认入口").Optional(),
		field.Json("env", map[string]string{}).Label("环境变量").Optional(),
		field.Bool("wait").Label("等待完成").Default(true).Desc("关掉 = 提交就返回，不带回结果与日志"),
		field.Int("timeout_sec").Label("等待上限（秒）").Default(300).Desc("最大 570；超时不杀任务，只是不再等"),
	}
}

func (RunJob) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("job").Label("Job 名"),
		field.Bool("succeeded").Label("成功").Desc("wait 关掉时恒为 false，表示「未等到结果」而非失败"),
		field.String("status").Label("状态").Desc("succeeded / failed / running / submitted"),
		field.Text("logs").Label("日志尾部").Desc("等待完成时带回（最后 200 行）"),
	}
}

// ApplyManifest is the generic YAML catch-all.
type ApplyManifest struct{}

func (ApplyManifest) Meta() contract.Meta {
	return contract.Meta{ID: "apply_manifest", Label: "应用 YAML 清单", TimeoutSec: 60,
		Desc: "kubectl apply 的通用兜底：Service/ConfigMap/CronJob/Ingress…专用操作没覆盖的资源都从这走。" +
			"支持 --- 分隔的多文档；server-side apply（不存在则建，存在则改）"}
}

func (ApplyManifest) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("manifest").Label("YAML 清单").Desc("整份 YAML，可含多个 --- 分隔的文档"),
		nsField(),
	}
}

func (ApplyManifest) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("成功"),
		field.Int("applied").Label("应用数"),
		field.Text("items").Label("明细").Desc("每行一条：kind/name → created|configured"),
	}
}

// DeleteObject is the generic delete.
type DeleteObject struct{}

func (DeleteObject) Meta() contract.Meta {
	return contract.Meta{ID: "delete_object", Label: "删除对象", TimeoutSec: 60,
		Desc: "删除一个资源对象（Deployment/Job/Service/ConfigMap…）。**不可逆**，名字与命名空间核对清楚"}
}

func (DeleteObject) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsField(),
		field.String("kind").Label("资源类型").Desc("如 Deployment / Job / Service / ConfigMap（区分大小写）"),
		field.String("name").Label("名称"),
		field.String("api_version").Label("apiVersion").Desc("留空自动探测（apps/v1 → batch/v1 → v1 → networking.k8s.io/v1）").Optional(),
	}
}

func (DeleteObject) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("已删除"),
		field.String("message").Label("说明"),
	}
}

// Credential: one credential = one cluster's kubeconfig.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("kubeconfig").Label("kubeconfig").
			Desc("整份 YAML 粘进来（ACK 用 aliyun 插件的「导出 kubeconfig」一键取；自建集群 ~/.kube/config）。" +
				"它就是进集群的钥匙，只在插件内部使用"),
		field.Text("namespace").Label("默认命名空间").
			Desc("操作里没写命名空间时用它；留空 = default").Optional(),
		field.Text("watch_namespaces").Label("盯哪些命名空间").
			Desc("逗号分隔（如 prod,staging）。**填了才启动事件源**：Pod 被驱逐 / 调度失败 / " +
				"容器异常退出 / 节点不健康 会触发工作流（轮询，约 1 分钟延迟）。" +
				"留空 = 不盯，只能用「事件列表」操作自己定时拉").Optional(),
	}
}
