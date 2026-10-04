package main

// 轮询事件源：把集群异常推成工作流触发。
//
// 为什么是轮询而不是 webhook：**k8s 不会主动往外发 HTTP**。它自己的 watch 是
// 「你连上来，我把变更流给你」，方向仍是我们去连。所以这里没有「平台代收」那条路。
//
// 为什么先不上 watch：watch 要处理断线重连、以及 resourceVersion 过期（410 Gone 时必须
// 重新 list 再 watch，否则会静默漏掉这中间的全部变更）。那是这件事真正的难点，
// 而轮询能先把「事件契约 + 触发链路」跑通。watch 记在 README「没做的」里。
//
// 三条纪律与 GitHub 那份事件源同构（那边已经踩过）：
//   - **首轮不触发**：第一次拉到的是集群当前的一堆历史异常，照发的话插件一启动就把
//     几十条陈年告警全推进工作流。首轮只记游标。
//   - **游标按「最近发生」**：k8s 对重复事件不新建条目，而是 count++ 并更新 lastTimestamp。
//     按首次发生比的话，持续中的故障只会在第一轮报出来。
//   - **event_id 稳定**：让平台去重兜住重复投递。

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// pollInterval：k8s 的 Event **默认只保留 1 小时**，所以间隔必须远小于它。
// 60 秒是「够及时」与「别把 API server 打爆」之间的折中——盯十来个命名空间时
// 每分钟十来次 list，对 API server 是可以忽略的量。
const pollInterval = 60 * time.Second

func runEvents(ctx plugin.SourceCtx) error {
	cred := credOf(ctx)
	nss := splitList(cred.WatchNamespaces)
	if len(nss) == 0 {
		// 没配就报「等配置」而不是静静退出——凭证列表上那一行会亮起来，
		// 否则用户只看到一个什么都不做的源，无从判断是坏了还是没配。
		ctx.ReportStatus("idle", "凭证里没填「盯哪些命名空间」，事件源不启动")
		return nil
	}
	if strings.TrimSpace(cred.Kubeconfig) == "" {
		ctx.ReportStatus("auth_required", "凭证里没有 kubeconfig")
		return nil
	}
	log.Printf("[k8s] 事件源启动，盯 %d 个命名空间：%s", len(nss), strings.Join(nss, ", "))

	// 游标：命名空间 → 上一轮的截止时刻（RFC3339）。首轮只记不推。
	cursor := map[string]string{}
	primed := map[string]bool{}
	// 崩溃去重：pod+结束时间。同一次崩溃在被拉起前会被连着看到好几轮。
	crashSeen := map[string]bool{}
	nodeSeen := map[string]string{} // 节点 → 上次报过的状态，状态没变就不重复报

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
		// 节点是集群级的，不按命名空间轮——盯多个命名空间时只查一次。
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

// pollEvents 拉一个命名空间的 Warning 事件，按 reason 分派。
func pollEvents(ctx plugin.SourceCtx, ns string, cursor map[string]string, primed map[string]bool) error {
	out, err := opEvents(ctx, &EventsIn{
		Namespace: ns, OnlyWarning: true, Limit: 200,
		Since:   cursor[ns], // 首轮为空 = 全量，但下面 primed 拦着不推
		Reasons: []string{"Evicted", "FailedScheduling"},
	})
	if err != nil {
		return err
	}
	// 截断说明这一轮的异常比 limit 还多——那本身就值得说一声，否则会静默漏。
	if out.Truncated {
		log.Printf("[k8s] %s 的告警超过 200 条，已截断——缩短轮询间隔或分拆命名空间", ns)
	}
	if !primed[ns] {
		log.Printf("[k8s] %s 首轮记录游标（%d 条历史异常不补发）", ns, out.Count)
		return nil
	}
	for _, e := range out.Events {
		pod := strings.TrimPrefix(e.Object, "Pod/")
		// event_id 带上「最近发生」：同一条事件再次发生是**一次新的真事件**，
		// 只按对象名去重会把第二次吃掉（故障还在持续，却只响了一次）。
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

// pollCrashes 从 **Pod 状态**认异常退出。
//
// 不走 Event：OOMKilled 多半不发 Event，容器被杀掉后立刻被拉起，痕迹只在
// lastState.terminated 里。盯 Event 会把最常见的那一类整个漏掉。
func pollCrashes(ctx plugin.SourceCtx, ns string, seen map[string]bool, primed bool) error {
	out, err := opPods(ctx, &PodsIn{Namespace: ns})
	if err != nil {
		return err
	}
	for _, p := range out.Pods {
		// Completed 是正常结束（Job 跑完），不是异常。
		if p.LastTerminatedReason == "" || p.LastTerminatedReason == "Completed" {
			continue
		}
		// pod + 结束时刻 = 一次崩溃。同一次崩溃会被连着好几轮看到，去重靠它。
		key := ns + "/" + p.Name + "@" + p.LastTerminatedAt
		if seen[key] {
			continue
		}
		seen[key] = true
		if !primed {
			continue // 首轮只记不推，与事件那条同一条规矩
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

// pollNodes 节点不健康。**只在状态变化时报**——节点会一直 NotReady 下去，
// 每轮报一次的话，一个坏了一夜的节点能刷出几百条告警。
func pollNodes(ctx plugin.SourceCtx, ns string, seen map[string]string, primed bool) error {
	out, err := opNodes(ctx, &NodesIn{})
	if err != nil {
		return err
	}
	for _, n := range out.Nodes {
		// 用 Ready 条件的**原值**：False（节点自报不健康）与 Unknown（kubelet 失联）
		// 在布尔上都是「不就绪」，但一个是去看节点上发生了什么、一个是先确认机器还在不在。
		status := n.ReadyStatus
		if status == "" {
			status = boolStatus(n.Ready)
		}
		if n.Ready {
			// 恢复了也要记一笔，否则下次再坏时状态没变化、报不出来。
			delete(seen, n.Name)
			continue
		}
		if seen[n.Name] == status {
			continue // 状态没变，不重复报
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

// nodeFromEvictMessage：驱逐原文里通常带节点名。认不出就留空——
// 猜一个错的节点名比空着更糟（人会去查一台没问题的机器）。
func nodeFromEvictMessage(msg string) string {
	// kubelet 的原文形如 "The node was low on resource: memory."，多数情况下不含节点名；
	// 少数版本会写 "Pod was evicted from node xxx"。只认后一种明确的形态。
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
