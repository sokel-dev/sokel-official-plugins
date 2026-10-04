package main

// Polling event source: turns cluster anomalies into workflow triggers.
//
// Why polling instead of webhook: k8s never initiates outbound HTTP. Its own watch mechanism
// is "you connect to me, I stream you the changes" -- the connection direction is still us
// connecting out. So there's no "platform receives it" path here.
//
// Why not watch yet: watch has to handle reconnects after disconnection, plus resourceVersion
// expiry (on 410 Gone you must re-list then re-watch, otherwise you silently miss every change
// in between). That's the real hard part of this feature, while polling lets us get the "event
// contract + trigger pipeline" working first. watch is recorded under "not done" in the README.
//
// Three disciplines shared with the GitHub event source's structure (already battle-tested there):
//   - The first round doesn't trigger: what's fetched the first time is a pile of the cluster's
//     current historical anomalies, and sending them as-is would flood the workflow with dozens
//     of stale alerts the moment the plugin starts. The first round only records the cursor.
//   - The cursor tracks "most recently occurred": k8s doesn't create a new entry for a repeated
//     event, it increments count and updates lastTimestamp. Comparing by first occurrence would
//     mean an ongoing failure only gets reported in the first round.
//   - event_id is stable: lets the platform's own dedup absorb repeated delivery.

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// pollInterval: k8s Events are only retained for 1 hour by default, so the interval must be
// much smaller than that. 60 seconds is the tradeoff between "timely enough" and "don't hammer
// the API server" -- watching a dozen-ish namespaces means a dozen-ish list calls per minute,
// a negligible load on the API server.
const pollInterval = 60 * time.Second

func runEvents(ctx plugin.SourceCtx) error {
	cred := credOf(ctx)
	nss := splitList(cred.WatchNamespaces)
	if len(nss) == 0 {
		// If unconfigured, report "waiting for config" instead of quietly exiting -- that row
		// lights up on the credential list; otherwise the user just sees a source doing nothing,
		// with no way to tell whether it's broken or unconfigured.
		ctx.ReportStatus("idle", "凭证里没填「盯哪些命名空间」，事件源不启动")
		return nil
	}
	if strings.TrimSpace(cred.Kubeconfig) == "" {
		ctx.ReportStatus("auth_required", "凭证里没有 kubeconfig")
		return nil
	}
	log.Printf("[k8s] 事件源启动，盯 %d 个命名空间：%s", len(nss), strings.Join(nss, ", "))

	// cursor: namespace -> the previous round's cutoff time (RFC3339). The first round only
	// records it, doesn't push.
	cursor := map[string]string{}
	primed := map[string]bool{}
	// Crash dedup: pod+finish time. The same crash gets seen for several rounds in a row before
	// the container is restarted.
	crashSeen := map[string]bool{}
	nodeSeen := map[string]string{} // node -> last reported status; don't re-report if unchanged

	for {
		for _, ns := range nss {
			if ctx.Err() != nil {
				return nil
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if err := pollEvents(ctx, ns, cursor, primed); err != nil {
				log.Printf("[k8s] 轮询 %s 的事件失败：%v", ns, err)
			} else {
				cursor[ns] = now
			}
			if err := pollCrashes(ctx, ns, crashSeen, primed[ns]); err != nil {
				log.Printf("[k8s] 轮询 %s 的崩溃失败：%v", ns, err)
			}
		}
		// Nodes are cluster-scoped, not per-namespace -- query once even when watching multiple namespaces.
		if err := pollNodes(ctx, nss[0], nodeSeen, primed[nss[0]]); err != nil {
			log.Printf("[k8s] 轮询节点失败：%v", err)
		}
		for _, ns := range nss {
			primed[ns] = true
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pollEvents fetches a namespace's Warning events and dispatches by reason.
func pollEvents(ctx plugin.SourceCtx, ns string, cursor map[string]string, primed map[string]bool) error {
	out, err := opEvents(ctx, &EventsIn{
		Namespace: ns, OnlyWarning: true, Limit: 200,
		Since:   cursor[ns], // empty on the first round = everything, but primed below blocks it from being pushed
		Reasons: []string{"Evicted", "FailedScheduling"},
	})
	if err != nil {
		return err
	}
	// Truncation means this round had more anomalies than the limit -- worth reporting on its own, otherwise it would be a silent loss.
	if out.Truncated {
		log.Printf("[k8s] %s 的告警超过 200 条，已截断——缩短轮询间隔或分拆命名空间", ns)
	}
	if !primed[ns] {
		log.Printf("[k8s] %s 首轮记录游标（%d 条历史异常不补发）", ns, out.Count)
		return nil
	}
	for _, e := range out.Events {
		pod := strings.TrimPrefix(e.Object, "Pod/")
		// event_id includes "most recently occurred": the same event recurring is a genuinely new
		// event; deduping on object name alone would swallow the second one (the failure is still
		// ongoing, but it only fired once).
		id := ns + ":" + e.Object + ":" + e.LastSeen
		switch e.Reason {
		case "Evicted":
			_ = TriggerPodEvicted(ctx, "evict:"+id, &PodEvictedEvent{
				Namespace: ns, Object: e.Object, Pod: pod, Message: e.Message,
				Node: nodeFromEvictMessage(e.Message), Count: e.Count, LastSeen: e.LastSeen,
			})
		case "FailedScheduling":
			_ = TriggerScheduleFailed(ctx, "sched:"+id, &ScheduleFailedEvent{
				Namespace: ns, Object: e.Object, Pod: pod, Message: e.Message,
				Count: e.Count, LastSeen: e.LastSeen,
			})
		}
	}
	return nil
}

// pollCrashes recognizes abnormal exits from Pod status.
//
// Doesn't go through Event: OOMKilled mostly doesn't emit an Event -- the container is
// restarted immediately after being killed, and the only trace is in lastState.terminated.
// Watching Events would miss this most common category entirely.
func pollCrashes(ctx plugin.SourceCtx, ns string, seen map[string]bool, primed bool) error {
	out, err := opPods(ctx, &PodsIn{Namespace: ns})
	if err != nil {
		return err
	}
	for _, p := range out.Pods {
		// Completed is a normal finish (a Job ran to completion), not an anomaly.
		if p.LastTerminatedReason == "" || p.LastTerminatedReason == "Completed" {
			continue
		}
		// pod + finish time = one crash. The same crash gets seen for several rounds in a row; this is the dedup key.
		key := ns + "/" + p.Name + "@" + p.LastTerminatedAt
		if seen[key] {
			continue
		}
		seen[key] = true
		if !primed {
			continue // first round only records, doesn't push -- same rule as the events path
		}
		_ = TriggerPodCrashed(ctx, "crash:"+key, &PodCrashedEvent{
			Namespace: ns, Object: "Pod/" + p.Name, Pod: p.Name,
			Reason: p.LastTerminatedReason, ExitCode: p.LastExitCode,
			Restarts: p.Restarts, Node: p.Node,
			FinishedAt: p.LastTerminatedAt, Phase: p.Phase,
		})
	}
	return nil
}

// pollNodes reports unhealthy nodes. Only reports on a status change -- a node can stay
// NotReady indefinitely, and reporting every round would flood hundreds of alerts for a node
// that's been down all night.
func pollNodes(ctx plugin.SourceCtx, ns string, seen map[string]string, primed bool) error {
	out, err := opNodes(ctx, &NodesIn{})
	if err != nil {
		return err
	}
	for _, n := range out.Nodes {
		// Use the Ready condition's raw value: False (the node reports itself unhealthy) and
		// Unknown (kubelet lost contact) are both "not ready" as a boolean, but one means go look
		// at what's happening on the node and the other means first confirm the machine is even there.
		status := n.ReadyStatus
		if status == "" {
			status = boolStatus(n.Ready)
		}
		if n.Ready {
			// Record the recovery too, otherwise next time it fails the status looks unchanged and won't be reported.
			delete(seen, n.Name)
			continue
		}
		if seen[n.Name] == status {
			continue // status unchanged, don't re-report
		}
		seen[n.Name] = status
		if !primed {
			continue
		}
		_ = TriggerNodeNotReady(ctx, "node:"+n.Name+":"+status, &NodeNotReadyEvent{
			Namespace: ns, Node: n.Name, Status: status, Message: n.Reason,
		})
	}
	return nil
}

// nodeFromEvictMessage: the eviction message usually names the node. Leave it blank if it
// can't be recognized -- guessing a wrong node name is worse than leaving it blank (someone
// would go check a machine that's actually fine).
func nodeFromEvictMessage(msg string) string {
	// kubelet's raw message typically looks like "The node was low on resource: memory.", which
	// usually doesn't name the node; a few versions say "Pod was evicted from node xxx". Only
	// that explicit form is recognized.
	const marker = "from node "
	if i := strings.Index(msg, marker); i >= 0 {
		rest := msg[i+len(marker):]
		if j := strings.IndexAny(rest, " .,"); j > 0 {
			return rest[:j]
		}
		return rest
	}
	return ""
}

func boolStatus(ready bool) string {
	if ready {
		return "True"
	}
	return "False"
}

var _ = http.MethodGet
