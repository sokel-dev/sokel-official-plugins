package main

// Operation implementations: 7 REST paths. Responses only parse the fields we need (K8s
// objects have hundreds of fields; modeling all of them is typed clientset's job, not these 7
// operations').

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

// -- pods --

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
				// lastState: what the previous termination looked like. OOMKilled mostly only
				// shows up here -- after being killed, the container is immediately restarted, and
				// the current state is already Running/Waiting.
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
			// With multiple containers, take the most recently terminated one: if two containers
			// in a pod crashed one after another, reporting the earliest one would point someone
			// at a problem that's already fixed.
			if t := cs.LastState.Terminated; t != nil && t.Reason != "" {
				if lastAt == "" || t.FinishedAt > lastAt { // RFC3339 lexical order is time order
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

// -- deployments --

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
	// kubectl rollout restart is implemented by changing this annotation: the template changed -> rolling replacement.
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
	// Read the previous value first -- "scaled from N to M" is information needed for both auditing and rollback.
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

// -- events --

type eventList struct {
	Items []eventItem `json:"items"`
}

// eventItem is a single event. All three time fields must be kept: the old API gives
// first/lastTimestamp, the newer events.k8s.io gives eventTime, and different cluster versions
// provide different ones (see eventNewerThan).
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
	// checked_at is taken after the request completes: taking it before would let events that
	// happen in between fall into the gap [checked_at, actual response], and using it as since
	// on the next round would never find them -- a silent missed alert.
	out := &EventsOut{CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	want := map[string]bool{}
	for _, r := range in.Reasons {
		if r = strings.TrimSpace(r); r != "" {
			want[strings.ToLower(r)] = true
		}
	}
	since := strings.TrimSpace(in.Since)
	// Iterate in reverse: K8s returns events in ascending time order, most recent last, but someone looking at events wants the recent ones.
	for i := len(list.Items) - 1; i >= 0; i-- {
		e := list.Items[i]
		if len(want) > 0 && !want[strings.ToLower(e.Reason)] {
			continue
		}
		// Compare by most recent occurrence, not first occurrence: a Warning keeps getting
		// accumulated by k8s (count++, lastTimestamp updated); comparing by first occurrence would
		// mean a failure lasting two hours only gets reported once, in the first round.
		if since != "" && !eventNewerThan(e, since) {
			continue
		}
		// More matches than limit -> say explicitly that it was truncated. It used to truncate silently, which would quietly drop alerts whenever the cluster got busy.
		if len(out.Events) >= limit {
			out.Truncated = true
			break
		}
		out.Events = append(out.Events, schema.Event{
			Type: e.Type, Reason: e.Reason,
			Object:  e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
			Message: e.Message, Count: e.Count,
			// Fall back to eventTime: clusters on the newer events.k8s.io don't provide
			// lastTimestamp, and taking it directly would leave "most recently occurred" empty --
			// which is exactly the field the caller uses as a cursor.
			LastSeen: firstNonEmpty(e.LastTimestamp, e.EventTime), FirstSeen: firstNonEmpty(e.FirstTimestamp, e.EventTime),
		})
	}
	out.Count = len(out.Events)
	return out, nil
}

// -- nodes --

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

// -- health --

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

// eventNewerThan reports whether this event is newer than the cursor.
//
// Uses "most recently occurred" rather than "first occurred" -- k8s doesn't create a new entry
// for a repeated event, it increments that entry's count and advances lastTimestamp. Comparing
// by first occurrence would mean a failure lasting two hours only gets reported in the first
// round, and forever looks older than the cursor after that (the user-visible symptom: the
// alert fires once and never again).
//
// Both time field forms must be recognized: the old lastTimestamp and the newer
// events.k8s.io's series/eventTime -- different cluster versions provide different ones, and
// recognizing only one would make it permanently "not newer" on the other half of clusters.
func eventNewerThan(e eventItem, since string) bool {
	for _, t := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp} {
		if t != "" {
			return t > since // RFC3339 lexical order is time order
		}
	}
	return true // no time at all: better to over-report than to swallow it
}
