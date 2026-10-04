package schema

// 事件契约：把集群里的**异常**推成工作流触发。
//
// 与 events 操作（「事件列表」）是两回事：那个是**你去问**集群要事件，这个是集群出事了
// **推给平台**。名字撞了但方向相反——操作能做「定时巡检」，只有事件源能做「一被驱逐就通知」。
//
// 选哪几种事件：按「出了要有人管」筛，不按「k8s 有哪些事件」铺。
// Normal 那一大堆（Scheduled/Pulled/Created/Started）没有一条需要惊动人，
// 全推上去只会让触发器变成噪音源，然后被整个关掉——那比没有更糟。
//
// **只有轮询这一条来路**（约 1 分钟延迟），没有 webhook：k8s 不会主动往外发 HTTP。
// 真正零延迟要走 API 的 watch 长连接，那要处理断线重连与 resourceVersion 过期（410 Gone
// 要重新 list 再 watch），是另一个量级的工作，先不做（README「没做的」里记了）。

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// 所有事件都带 namespace，平台会平铺到触发输入顶层，各分支共用一个变量。
//
// 叫 EventsCommon 而不是 Events：schema.go 里 Events 已经是「事件列表」**操作**的类型名了。
// 撞名的话生成器会把两者当同一个东西——而它俩恰好一个是拉、一个是推。
type EventsCommon struct{}

func (EventsCommon) CommonFields() []string { return []string{"namespace"} }

func nsEventField() contract.FieldSpec {
	return field.String("namespace").Label("命名空间")
}

func objectField() contract.FieldSpec {
	return field.String("object").Label("对象").Desc("如 Pod/web-abc123")
}

// PodEvicted Pod 被驱逐。
type PodEvicted struct{}

func (PodEvicted) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pod_evicted", Label: "Pod 被驱逐",
		Desc: "节点资源不足时 kubelet 主动赶走了 Pod（Event reason=Evicted）"}
}

func (PodEvicted) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsEventField(), objectField(),
		field.String("pod").Label("Pod 名"),
		field.Text("message").Label("原文").
			Desc("kubelet 给的原因，如 The node was low on resource: memory —— **缺哪种资源写在这里**"),
		field.String("node").Label("节点").Desc("从原文里认出来的；认不出为空").Optional(),
		field.Int("count").Label("累计发生次数").Desc("同一条事件被 k8s 累加的次数，>1 说明在反复发生").Optional(),
		field.String("last_seen").Label("最近发生").Optional(),
	}
}

// ScheduleFailed 排不上（资源不够）。
type ScheduleFailed struct{}

func (ScheduleFailed) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "schedule_failed", Label: "调度失败",
		Desc: "Pod 起不来，没有节点能放下它（Event reason=FailedScheduling）"}
}

func (ScheduleFailed) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsEventField(), objectField(),
		field.String("pod").Label("Pod 名"),
		field.Text("message").Label("原文").
			Desc("如 0/3 nodes are available: Insufficient cpu —— **差多少、差什么都在这句里**"),
		field.Int("count").Label("累计发生次数").Optional(),
		field.String("last_seen").Label("最近发生").Optional(),
	}
}

// PodCrashed 容器异常退出。
//
// **它不从 Event 来，从 Pod 状态来。** OOMKilled 多半不发 Event——容器被杀掉后立刻被
// 拉起，只在 lastState.terminated 里留下痕迹。盯 Event 的话这一类会整个漏掉，
// 而它恰恰是最常见的一种「异常退出」。
type PodCrashed struct{}

func (PodCrashed) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "pod_crashed", Label: "容器异常退出",
		Desc: "容器非正常结束（OOMKilled / 非零退出码），判据取自 Pod 状态而不是事件"}
}

func (PodCrashed) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsEventField(), objectField(),
		field.String("pod").Label("Pod 名"),
		field.String("reason").Label("结束原因").
			Desc("OOMKilled = 内存不够，调 limits；Error = 进程自己挂了，看日志"),
		field.Int("exit_code").Label("退出码").Desc("137 = 被 SIGKILL（OOM 的典型特征）").Optional(),
		field.Int("restarts").Label("累计重启次数").Optional(),
		field.String("node").Label("节点").Optional(),
		field.String("finished_at").Label("结束时间"),
		field.String("phase").Label("Pod 状态").Desc("Running 也可能挂过——它被拉起来了").Optional(),
	}
}

// NodeNotReady 节点不健康。
type NodeNotReady struct{}

func (NodeNotReady) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "node_not_ready", Label: "节点不健康",
		Desc: "节点 Ready 条件不为 True（失联 / 磁盘压力 / 内存压力）"}
}

func (NodeNotReady) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		nsEventField(),
		field.String("node").Label("节点名"),
		field.String("status").Label("Ready 状态").Desc("False = 明确不健康；Unknown = 失联（kubelet 不上报了）"),
		field.Text("message").Label("原文").Optional(),
	}
}
