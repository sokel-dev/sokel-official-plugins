package schema

// Event contracts: turn cluster anomalies into workflow triggers.
//
// Different from the events operation ("event list"): that one is you asking the cluster for
// events, this one is the cluster pushing to the platform when something goes wrong. The names
// are similar but the direction is opposite -- the operation can do "scheduled inspection",
// only the event source can do "notify the moment something gets evicted".
//
// Which event kinds were chosen: filtered by "this needs someone to act on it", not laid out by
// "whatever events k8s happens to have". The whole pile of Normal events
// (Scheduled/Pulled/Created/Started) has nothing that needs to wake anyone up; pushing all of
// them would just turn the trigger into a noise source that gets turned off entirely -- worse
// than not having it.
//
// Polling is the only path here (about 1 minute of latency), no webhook: k8s never initiates
// outbound HTTP. True zero latency would require the API's watch long-lived connection, which
// means handling reconnects after disconnection and resourceVersion expiry (on 410 Gone you
// must re-list then re-watch) -- a different order of work, not done yet (recorded under "not
// done" in the README).

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Every event carries namespace, which the platform flattens to the top level of the trigger
// input; all branches share one variable.
//
// Named EventsCommon rather than Events: Events in schema.go is already the type name for the
// events-list operation. A name collision would make the generator treat the two as the same
// thing -- and they happen to be, respectively, a pull and a push.
type EventsCommon struct{}

func (EventsCommon) CommonFields() []string { return []string{"namespace"} }

func nsEventField() contract.FieldSpec {
	return field.String("namespace").Label("命名空间")
}

func objectField() contract.FieldSpec {
	return field.String("object").Label("对象").Desc("如 Pod/web-abc123")
}

// PodEvicted fires when a Pod is evicted.
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

// ScheduleFailed fires when a Pod can't be scheduled (insufficient resources).
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

// PodCrashed fires when a container exits abnormally.
//
// It doesn't come from Event, it comes from Pod status. OOMKilled mostly doesn't emit an Event
// -- the container is restarted immediately after being killed, leaving a trace only in
// lastState.terminated. Watching Events would miss this category entirely, and it happens to be
// the most common kind of "abnormal exit"
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

// NodeNotReady fires when a node is unhealthy.
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
