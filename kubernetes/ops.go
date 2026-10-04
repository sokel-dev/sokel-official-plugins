package main

// 操作实现：7 个 REST 路径。应答只解我们要的字段（K8s 对象几百个字段，
// 全量建模是 typed clientset 的事，不是这 7 个操作的事）。

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/kubernetes/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// —— pods ——

type podList struct {
	Items []struct {
		Metadata struct {
			Name              string `json:"name"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready        bool `json:"ready"`
				RestartCount int  `json:"restartCount"`
				State        struct {
					Waiting *struct {
						Reason string `json:"reason"`
					} `json:"waiting"`
					Terminated *struct {
						Reason string `json:"reason"`
					} `json:"terminated"`
				} `json:"state"`
				// lastState：**上一次**结束的样子。OOMKilled 多半只在这里——
				// 被杀掉后容器会被立刻拉起，当前 state 已经是 Running/Waiting 了。
				LastState struct {
					Terminated *struct {
						Reason     string `json:"reason"`
						ExitCode   int    `json:"exitCode"`
						FinishedAt string `json:"finishedAt"`
					} `json:"terminated"`
				} `json:"lastState"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

func opPods(ctx plugin.Ctx, in *PodsIn) (*PodsOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if ls := strings.TrimSpace(in.LabelSelector); ls != "" {
		q.Set("labelSelector", ls)
	}
	raw, err := k.do(ctx, "GET", "/api/v1/namespaces/"+nsOf(cred, in.Namespace)+"/pods", q, nil, "")
	if err != nil {
		return nil, err
	}
	var list podList
	_ = json.Unmarshal(raw, &list)
	out := &PodsOut{}
	for _, p := range list.Items {
		ready, total, restarts, reason := 0, 0, 0, ""
		lastReason, lastExit, lastAt := "", 0, ""
		for _, cs := range p.Status.ContainerStatuses {
			total++
			if cs.Ready {
				ready++
			}
			restarts += cs.RestartCount
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				reason = cs.State.Waiting.Reason
			}
			if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" && cs.State.Terminated.Reason != "Completed" {
				reason = cs.State.Terminated.Reason
			}
			// 多容器时取**最近结束的那个**：一个 pod 里两个容器先后挂过，
			// 报最早那次会把人指向已经修好的问题。
			if t := cs.LastState.Terminated; t != nil && t.Reason != "" {
				if lastAt == "" || t.FinishedAt > lastAt { // RFC3339 字典序即时间序
					lastReason, lastExit, lastAt = t.Reason, t.ExitCode, t.FinishedAt
				}
			}
		}
		abnormal := (p.Status.Phase != "Running" && p.Status.Phase != "Succeeded") || reason != ""
		if in.OnlyAbnormal && !abnormal {
			continue
		}
		out.Pods = append(out.Pods, schema.Pod{
			Name: p.Metadata.Name, Phase: p.Status.Phase,
			Ready: fmt.Sprintf("%d/%d", ready, total), Restarts: restarts,
			Node: p.Spec.NodeName, Age: ageOf(p.Metadata.CreationTimestamp), Reason: reason,
			LastTerminatedReason: lastReason, LastExitCode: lastExit, LastTerminatedAt: lastAt,
		})
	}
	out.Count = len(out.Pods)
	return out, nil
}

func ageOf(created string) string {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d > 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d > time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func opPodLogs(ctx plugin.Ctx, in *PodLogsIn) (*PodLogsOut, error) {
	pod := strings.TrimSpace(in.Pod)
	if pod == "" {
		return nil, fmt.Errorf("pod 名是空的（来自「Pod 列表」）")
	}
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	tail := in.TailLines
	if tail <= 0 {
		tail = 200
	}
	if tail > 5000 {
		tail = 5000
	}
	q := url.Values{"tailLines": {strconv.Itoa(tail)}}
	if c := strings.TrimSpace(in.Container); c != "" {
		q.Set("container", c)
	}
	if in.Previous {
		q.Set("previous", "true")
	}
	raw, err := k.do(ctx, "GET", "/api/v1/namespaces/"+nsOf(cred, in.Namespace)+"/pods/"+pod+"/log", q, nil, "")
	if err != nil {
		if strings.Contains(err.Error(), "a container name must be specified") {
			return nil, fmt.Errorf("这个 pod 有多个容器，要在「容器名」里指定一个")
		}
		return nil, err
	}
	logs := string(raw)
	return &PodLogsOut{Logs: logs, Lines: strings.Count(logs, "\n")}, nil
}

// —— deployments ——

type deployList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Replicas int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas     int `json:"readyReplicas"`
			AvailableReplicas int `json:"availableReplicas"`
		} `json:"status"`
	} `json:"items"`
}

func opDeployments(ctx plugin.Ctx, in *DeploymentsIn) (*DeploymentsOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	raw, err := k.do(ctx, "GET", "/apis/apps/v1/namespaces/"+nsOf(cred, in.Namespace)+"/deployments", nil, nil, "")
	if err != nil {
		return nil, err
	}
	var list deployList
	_ = json.Unmarshal(raw, &list)
	out := &DeploymentsOut{}
	for _, d := range list.Items {
		var images []string
		for _, c := range d.Spec.Template.Spec.Containers {
			images = append(images, c.Image)
		}
		out.Deployments = append(out.Deployments, schema.Deployment{
			Name: d.Metadata.Name, Replicas: d.Spec.Replicas,
			Ready: d.Status.ReadyReplicas, Available: d.Status.AvailableReplicas, Images: images,
		})
	}
	out.Count = len(out.Deployments)
	return out, nil
}

func opDeploymentRestart(ctx plugin.Ctx, in *DeploymentRestartIn) (*DeploymentRestartOut, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("deployment 名是空的")
	}
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	// kubectl rollout restart 的实现就是改这个注解：模板变了 → 滚动替换。
	now := time.Now().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, now)
	if _, err := k.do(ctx, "PATCH",
		"/apis/apps/v1/namespaces/"+nsOf(cred, in.Namespace)+"/deployments/"+name,
		nil, []byte(patch), "application/strategic-merge-patch+json"); err != nil {
		return nil, err
	}
	return &DeploymentRestartOut{OK: true, RestartedAt: now}, nil
}

func opDeploymentScale(ctx plugin.Ctx, in *DeploymentScaleIn) (*DeploymentScaleOut, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("deployment 名是空的")
	}
	if in.Replicas < 0 {
		return nil, fmt.Errorf("副本数不能是负数")
	}
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	path := "/apis/apps/v1/namespaces/" + nsOf(cred, in.Namespace) + "/deployments/" + name + "/scale"
	// 先读原值——「从几调到几」是审计与回滚都要的信息。
	prev := 0
	if raw, gerr := k.do(ctx, "GET", path, nil, nil, ""); gerr == nil {
		var sc struct {
			Spec struct {
				Replicas int `json:"replicas"`
			} `json:"spec"`
		}
		_ = json.Unmarshal(raw, &sc)
		prev = sc.Spec.Replicas
	}
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, in.Replicas)
	if _, err := k.do(ctx, "PATCH", path, nil, []byte(patch), "application/merge-patch+json"); err != nil {
		return nil, err
	}
	return &DeploymentScaleOut{OK: true, Previous: prev}, nil
}

// —— events ——

type eventList struct {
	Items []eventItem `json:"items"`
}

// eventItem 一条事件。**三个时间字段都要留着**：老 API 给 first/lastTimestamp，
// 新版 events.k8s.io 给 eventTime，集群版本不同给的不一样（见 eventNewerThan）。
type eventItem struct {
	Type           string `json:"type"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Count          int    `json:"count"`
	FirstTimestamp string `json:"firstTimestamp"`
	LastTimestamp  string `json:"lastTimestamp"`
	EventTime      string `json:"eventTime"`
	InvolvedObject struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"involvedObject"`
}

func opEvents(ctx plugin.Ctx, in *EventsIn) (*EventsOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if in.OnlyWarning {
		q.Set("fieldSelector", "type=Warning")
	}
	raw, err := k.do(ctx, "GET", "/api/v1/namespaces/"+nsOf(cred, in.Namespace)+"/events", q, nil, "")
	if err != nil {
		return nil, err
	}
	var list eventList
	_ = json.Unmarshal(raw, &list)
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	// checked_at 在**发请求之后**取：取在前面的话，这中间发生的事件会落进
	// [checked_at, 实际返回] 这个缝里，下一轮用它当 since 就永远查不到——静默漏告警。
	out := &EventsOut{CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	want := map[string]bool{}
	for _, r := range in.Reasons {
		if r = strings.TrimSpace(r); r != "" {
			want[strings.ToLower(r)] = true
		}
	}
	since := strings.TrimSpace(in.Since)
	// 倒着取：K8s 按时间正序给，最近的在最后，而看事件的人要的是最近的。
	for i := len(list.Items) - 1; i >= 0; i-- {
		e := list.Items[i]
		if len(want) > 0 && !want[strings.ToLower(e.Reason)] {
			continue
		}
		// 按**最近发生**比，不是首次发生：一条 Warning 会被 k8s 反复累加（count++、
		// lastTimestamp 更新），拿首次发生比的话，一个持续两小时的故障只在第一轮报一次。
		if since != "" && !eventNewerThan(e, since) {
			continue
		}
		// 命中的比 limit 多 → 明说被截了。此前静默截断，集群一忙就会悄悄漏掉告警。
		if len(out.Events) >= limit {
			out.Truncated = true
			break
		}
		out.Events = append(out.Events, schema.Event{
			Type: e.Type, Reason: e.Reason,
			Object:  e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Message: e.Message, Count: e.Count,
			// 回落到 eventTime：新版 events.k8s.io 的集群不给 lastTimestamp，
			// 照直取会让「最近发生」为空——而它正是调用方拿去当游标的那个字段。
			LastSeen: firstNonEmpty(e.LastTimestamp, e.EventTime), FirstSeen: firstNonEmpty(e.FirstTimestamp, e.EventTime),
		})
	}
	out.Count = len(out.Events)
	return out, nil
}

// —— nodes ——

type nodeList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Taints []any `json:"taints"`
		} `json:"spec"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
				Reason string `json:"reason"`
			} `json:"conditions"`
			NodeInfo struct {
				KubeletVersion string `json:"kubeletVersion"`
			} `json:"nodeInfo"`
			Capacity map[string]string `json:"capacity"`
		} `json:"status"`
	} `json:"items"`
}

func opNodes(ctx plugin.Ctx, _ *NodesIn) (*NodesOut, error) {
	k, err := clientOf(credOf(ctx))
	if err != nil {
		return nil, err
	}
	raw, err := k.do(ctx, "GET", "/api/v1/nodes", nil, nil, "")
	if err != nil {
		return nil, err
	}
	var list nodeList
	_ = json.Unmarshal(raw, &list)
	out := &NodesOut{}
	for _, n := range list.Items {
		ready, status, reason := false, "", ""
		for _, c := range n.Status.Conditions {
			if c.Type != "Ready" {
				continue
			}
			status, reason = c.Status, c.Reason
			ready = c.Status == "True"
		}
		if !ready {
			out.NotReady++
		}
		out.Nodes = append(out.Nodes, schema.Node{
			Name: n.Metadata.Name, Ready: ready, ReadyStatus: status, Reason: reason,
			Version: n.Status.NodeInfo.KubeletVersion,
			CPU:     n.Status.Capacity["cpu"], Memory: n.Status.Capacity["memory"],
			Taints: len(n.Spec.Taints),
		})
	}
	out.Count = len(out.Nodes)
	return out, nil
}

// —— health ——

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	k, err := clientOf(credOf(ctx))
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	raw, err := k.do(ctx, "GET", "/version", nil, nil, "")
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	_ = json.Unmarshal(raw, &v)
	return &HealthCheckOut{OK: true, Version: v.GitVersion, Message: "集群可达：" + v.GitVersion}, nil
}

// eventNewerThan：这条事件比游标新吗。
//
// 取「最近发生」而不是「首次发生」——k8s 对重复事件不是每次新建一条，而是把同一条的
// count 加一、lastTimestamp 往前推。按首次发生比的话，一个持续两小时的故障只会在
// 第一轮被报出来，之后永远比游标旧（用户实感：告警只响一次就再也不响了）。
//
// 两种时间字段都要认：老的 lastTimestamp 与新版 events.k8s.io 的 series/eventTime，
// 集群版本不同给的不一样，只认一个会在另一半集群上恒为「不比它新」。
func eventNewerThan(e eventItem, since string) bool {
	for _, t := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp} {
		if t != "" {
			return t > since // RFC3339 字典序即时间序
		}
	}
	return true // 一个时间都没有：宁可多报一次，也不吞掉
}
