package main

// httptest 假 API server 打穿全部路径。kubeconfig 指向假服务器（无 TLS 校验），
// 真集群联调走 operation:test。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

// kubeconfigFor 指向假服务器的最小 kubeconfig。
// **必须 TLS + insecure-skip-tls-verify**：client-go 对明文 http:// 的 server
// 会静默丢弃 token（不让凭证走明文），Authorization 头就没了——真集群全是 TLS
// 不会碰到，但假 server 用 NewServer 的话这条测试静默测不到认证头。
func kubeconfigFor(srv *httptest.Server) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: t
  cluster: {server: %q, insecure-skip-tls-verify: true}
contexts:
- name: t
  context: {cluster: t, user: t}
current-context: t
users:
- name: t
  user: {token: fake-token}
`, srv.URL)
}

func fakeAPIServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(handler)
}

// pods：ready 统计、异常原因提取、only_abnormal 过滤——三件一起验。
func TestPodsAbnormalFilter(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/pods") {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"items":[
		 {"metadata":{"name":"ok-pod"},"spec":{"nodeName":"n1"},"status":{"phase":"Running",
		   "containerStatuses":[{"ready":true,"restartCount":0,"state":{}}]}},
		 {"metadata":{"name":"crash-pod"},"spec":{"nodeName":"n2"},"status":{"phase":"Running",
		   "containerStatuses":[{"ready":false,"restartCount":7,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}
		]}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})

	out, err := opPods(ctx, &PodsIn{OnlyAbnormal: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || out.Pods[0].Name != "crash-pod" {
		t.Fatalf("只该留异常的 crash-pod: %+v", out.Pods)
	}
	if out.Pods[0].Reason != "CrashLoopBackOff" || out.Pods[0].Restarts != 7 || out.Pods[0].Ready != "0/1" {
		t.Errorf("异常信息没提全: %+v", out.Pods[0])
	}
}

// rollout restart = 改模板注解的 strategic-merge PATCH；Bearer token 要随请求带上。
func TestDeploymentRestartPatch(t *testing.T) {
	var method, ctype, auth, body string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, ctype, auth = r.Method, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv), "namespace": "prod"})

	out, err := opDeploymentRestart(ctx, &DeploymentRestartIn{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.RestartedAt == "" {
		t.Errorf("out = %+v", out)
	}
	if method != "PATCH" || ctype != "application/strategic-merge-patch+json" {
		t.Errorf("method=%s ctype=%s", method, ctype)
	}
	if auth != "Bearer fake-token" {
		t.Errorf("kubeconfig 里的 token 没带上: %q", auth)
	}
	if !strings.Contains(body, "kubectl.kubernetes.io/restartedAt") {
		t.Errorf("patch 不是 rollout restart 的形状: %s", body)
	}
}

// scale：先 GET /scale 读原值再 PATCH；缩到 0 合法（停服务），负数拒绝。
func TestDeploymentScale(t *testing.T) {
	var patched string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = io.WriteString(w, `{"spec":{"replicas":3}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		patched = string(b)
		_, _ = io.WriteString(w, `{}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})

	out, err := opDeploymentScale(ctx, &DeploymentScaleIn{Name: "web", Replicas: 0})
	if err != nil {
		t.Fatal(err)
	}
	if out.Previous != 3 || !strings.Contains(patched, `"replicas":0`) {
		t.Errorf("prev=%d patch=%s", out.Previous, patched)
	}
	if _, err := opDeploymentScale(ctx, &DeploymentScaleIn{Name: "web", Replicas: -1}); err == nil {
		t.Error("负副本数要拒绝")
	}
}

// 事件倒序（最近在前）+ Warning 过滤下推到 fieldSelector。
func TestEventsRecentFirst(t *testing.T) {
	var q string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"items":[
		 {"type":"Warning","reason":"Old","message":"1","involvedObject":{"kind":"Pod","name":"a"}},
		 {"type":"Warning","reason":"New","message":"2","involvedObject":{"kind":"Pod","name":"b"}}
		]}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	out, err := opEvents(ctx, &EventsIn{OnlyWarning: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, "type%3DWarning") && !strings.Contains(q, "type=Warning") {
		t.Errorf("Warning 过滤要下推: %q", q)
	}
	if out.Events[0].Reason != "New" {
		t.Errorf("最近的事件要在前: %+v", out.Events)
	}
}

// RBAC 403 要翻译成「去加权限」，而不是裸 JSON。
func TestForbiddenTranslated(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","message":"pods is forbidden: User \"x\" cannot list resource"}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	_, err := opPods(ctx, &PodsIn{})
	if err == nil || !strings.Contains(err.Error(), "RBAC") {
		t.Errorf("403 要点名 RBAC, got %v", err)
	}
}

// nodes：not_ready 计数是告警工作流的直接判据。
func TestNodesNotReadyCount(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"metadata":{"name":"n1"},"spec":{},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30"},"capacity":{"cpu":"8"}}},
		 {"metadata":{"name":"n2"},"spec":{},"status":{"conditions":[{"type":"Ready","status":"False"}],"nodeInfo":{},"capacity":{}}}
		]}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	out, err := opNodes(ctx, &NodesIn{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 || out.NotReady != 1 {
		t.Errorf("count=%d notReady=%d", out.Count, out.NotReady)
	}
}

// 多容器 pod 不指定容器名时，K8s 的报错要转成人话。
func TestPodLogsMultiContainerHint(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"a container name must be specified for pod web-1, choose one of: [app sidecar]"}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	_, err := opPodLogs(ctx, &PodLogsIn{Pod: "web-1"})
	if err == nil || !strings.Contains(err.Error(), "多个容器") {
		t.Errorf("要提示指定容器名, got %v", err)
	}
}

// 凭证只给 server 地址不给整份 YAML——最常见的粘错，报错要指路。
func TestBadKubeconfig(t *testing.T) {
	_, err := opPods(newFake(map[string]string{"kubeconfig": "https://1.2.3.4:6443"}), &PodsIn{})
	if err == nil || !strings.Contains(err.Error(), "整份 YAML") {
		t.Errorf("要说明粘整份 YAML, got %v", err)
	}
	var e error
	_, e = opPods(newFake(map[string]string{}), &PodsIn{})
	if e == nil || !strings.Contains(e.Error(), "kubeconfig") {
		t.Errorf("缺凭证要指路, got %v", e)
	}
}

var _ = json.Marshal

// —— 异常事件监控：这几条对应用户真正关心的三类（驱逐 / 调度失败 / 异常退出）——

// OOMKilled **只在 lastState 里**：容器被杀掉后立刻被拉起，当前 state 已经是 Running，
// reason 里什么都看不到。只看 restarts 的话，你知道它在反复重启，
// 却不知道是内存不够、崩了、还是被驱逐——而这三者的处理完全不同。
func TestPodsSurfacesLastTerminated(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"metadata":{"name":"oom-pod"},"spec":{"nodeName":"n1"},"status":{"phase":"Running",
		   "containerStatuses":[{"ready":true,"restartCount":3,"state":{"running":{}},
		     "lastState":{"terminated":{"reason":"OOMKilled","exitCode":137,"finishedAt":"2026-08-28T10:00:00Z"}}}]}}
		]}`)
	})
	defer srv.Close()
	out, err := opPods(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &PodsIn{})
	if err != nil {
		t.Fatal(err)
	}
	p := out.Pods[0]
	if p.LastTerminatedReason != "OOMKilled" {
		t.Errorf("看不出是 OOM 被杀的（只知道重启了 %d 次）：%+v", p.Restarts, p)
	}
	if p.LastExitCode != 137 {
		t.Errorf("退出码要带出来（137 = 被 SIGKILL，OOM 的典型特征）：%+v", p)
	}
	if p.LastTerminatedAt == "" {
		t.Error("要有结束时间——否则分不清是刚刚发生还是上周的历史遗留")
	}
	// 当前 state 正常，所以 reason 应当是空的——这正是为什么必须单独看 lastState
	if p.Reason != "" {
		t.Errorf("当前 state 是 Running，reason 该为空：%+v", p)
	}
}

// 多容器时取**最近结束的那个**：报最早那次会把人指向已经修好的问题。
func TestPodsPicksMostRecentTermination(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"metadata":{"name":"multi"},"spec":{},"status":{"phase":"Running","containerStatuses":[
		   {"ready":true,"restartCount":1,"state":{},"lastState":{"terminated":{"reason":"Error","exitCode":1,"finishedAt":"2026-08-01T00:00:00Z"}}},
		   {"ready":true,"restartCount":1,"state":{},"lastState":{"terminated":{"reason":"OOMKilled","exitCode":137,"finishedAt":"2026-08-28T09:00:00Z"}}}
		 ]}}
		]}`)
	})
	defer srv.Close()
	out, _ := opPods(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &PodsIn{})
	if out.Pods[0].LastTerminatedReason != "OOMKilled" {
		t.Errorf("应取最近那次（OOMKilled），实际 %+v", out.Pods[0])
	}
}

// 轮询的命根子：按「最近发生」比，**不是首次发生**。
// k8s 对重复事件不新建条目，而是 count++ 且更新 lastTimestamp——按首次发生比的话，
// 一个持续两小时的故障只会在第一轮报出来，之后永远比游标旧（告警只响一次就没了）。
func TestEventsSinceUsesLastSeen(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"type":"Warning","reason":"Evicted","message":"low memory","count":9,
		  "firstTimestamp":"2026-08-28T08:00:00Z","lastTimestamp":"2026-08-28T11:00:00Z",
		  "involvedObject":{"kind":"Pod","name":"a"}}
		]}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})

	// 游标在首次发生之后、最近发生之前 → **必须仍然报出来**（故障还在持续）
	out, err := opEvents(ctx, &EventsIn{Since: "2026-08-28T09:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("持续中的故障必须继续报，否则告警只响一次：%+v", out)
	}
	// 游标在最近发生之后 → 不该重复报
	out2, _ := opEvents(ctx, &EventsIn{Since: "2026-08-28T12:00:00Z"})
	if len(out2.Events) != 0 {
		t.Errorf("比游标旧的不该再报：%+v", out2.Events)
	}
	// checked_at 要能直接当下一轮的游标
	if out.CheckedAt == "" {
		t.Error("要给 checked_at，否则调用方没法接着轮询")
	}
}

// 新版 events.k8s.io 的集群不给 lastTimestamp，只给 eventTime。
// 只认一个字段的话，那一半集群上游标恒为空、过滤恒不生效。
func TestEventsAcceptsEventTimeOnlyClusters(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"type":"Warning","reason":"FailedScheduling","message":"Insufficient cpu",
		  "eventTime":"2026-08-28T11:00:00Z","involvedObject":{"kind":"Pod","name":"a"}}
		]}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	out, _ := opEvents(ctx, &EventsIn{Since: "2026-08-28T09:00:00Z"})
	if len(out.Events) != 1 {
		t.Fatalf("新版 API 的集群同样要能过滤：%+v", out)
	}
	if out.Events[0].LastSeen == "" {
		t.Error("「最近发生」不能为空——调用方正是拿它当游标的")
	}
}

// 截断必须**说出来**。此前静默截断，集群一忙就会悄悄漏掉告警。
func TestEventsReportsTruncation(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"items":[`)
		for i := 0; i < 5; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"type":"Warning","reason":"Evicted","message":"m%d","involvedObject":{"kind":"Pod","name":"p%d"}}`, i, i)
		}
		b.WriteString(`]}`)
		_, _ = io.WriteString(w, b.String())
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	out, _ := opEvents(ctx, &EventsIn{Limit: 2})
	if len(out.Events) != 2 {
		t.Fatalf("limit 应生效：%d", len(out.Events))
	}
	if !out.Truncated {
		t.Error("命中的比 limit 多就必须报 truncated——静默截断 = 悄悄漏告警")
	}
	full, _ := opEvents(ctx, &EventsIn{Limit: 50})
	if full.Truncated {
		t.Error("没截断时不该报 truncated（否则这个信号就没意义了）")
	}
}

// 按原因过滤：关注特定异常时，比拿回来自己筛省一半篇幅。
func TestEventsFiltersByReason(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"type":"Warning","reason":"Evicted","message":"a","involvedObject":{"kind":"Pod","name":"a"}},
		 {"type":"Warning","reason":"Unhealthy","message":"b","involvedObject":{"kind":"Pod","name":"b"}},
		 {"type":"Warning","reason":"FailedScheduling","message":"c","involvedObject":{"kind":"Pod","name":"c"}}
		]}`)
	})
	defer srv.Close()
	ctx := newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	out, _ := opEvents(ctx, &EventsIn{Reasons: []string{"Evicted", "failedscheduling"}}) // 大小写不敏感
	if len(out.Events) != 2 {
		t.Fatalf("应只留 Evicted 与 FailedScheduling：%+v", out.Events)
	}
	for _, e := range out.Events {
		if e.Reason == "Unhealthy" {
			t.Error("没点名的原因不该出现")
		}
	}
}

// —— 事件源：把异常推成触发。这里每条错了都不报错，只是**告警变噪音或整个消失** ——

type fakeSource struct {
	*fakeCtx
	fired []string       // 事件 id，按顺序
	byEv  map[string]any // 事件名 → 最后一次的 payload
}

func newSrc(cred map[string]string) *fakeSource {
	return &fakeSource{fakeCtx: newFake(cred), byEv: map[string]any{}}
}

func (f *fakeSource) Trigger(event, id string, payload any) error {
	f.fired = append(f.fired, event+"|"+id)
	f.byEv[event] = payload
	return nil
}
func (f *fakeSource) UpdateCredential(map[string]string) error { return nil }
func (f *fakeSource) ReportStatus(string, string)              {}

func firedNames(f *fakeSource) []string {
	var out []string
	for k := range f.byEv {
		out = append(out, k)
	}
	return out
}

// **首轮不触发**：插件一启动，集群里那堆历史异常会被全量拉到。
// 照发的话，装上插件的第一分钟就有几十条陈年告警涌进工作流——
// 用户的第一印象是「这东西疯了」，然后把触发器关掉。
func TestSourceFirstRoundDoesNotFire(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/events") {
			_, _ = io.WriteString(w, `{"items":[
			 {"type":"Warning","reason":"Evicted","message":"The node was low on resource: memory",
			  "lastTimestamp":"2026-08-28T10:00:00Z","involvedObject":{"kind":"Pod","name":"old"}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"items":[]}`)
	})
	defer srv.Close()
	ctx := newSrc(map[string]string{"kubeconfig": kubeconfigFor(srv)})

	cursor, primed := map[string]string{}, map[string]bool{}
	if err := pollEvents(ctx, "prod", cursor, primed); err != nil {
		t.Fatal(err)
	}
	if len(ctx.fired) != 0 {
		t.Fatalf("首轮不该推任何东西，实际推了 %v", ctx.fired)
	}
	// 第二轮（已 primed）才推
	primed["prod"] = true
	if err := pollEvents(ctx, "prod", cursor, primed); err != nil {
		t.Fatal(err)
	}
	if len(ctx.fired) != 1 {
		t.Fatalf("第二轮该推 1 条，实际 %v", ctx.fired)
	}
}

// 按 reason 分派到不同事件：驱逐与调度失败是两类问题，混成一个的话
// 下游没法分别处理（一个要看节点内存，一个要看集群余量）。
func TestSourceRoutesByReason(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/events") {
			_, _ = io.WriteString(w, `{"items":[
			 {"type":"Warning","reason":"Evicted","message":"Pod was evicted from node worker-3",
			  "lastTimestamp":"2026-08-28T10:00:00Z","involvedObject":{"kind":"Pod","name":"a"}},
			 {"type":"Warning","reason":"FailedScheduling","message":"0/3 nodes are available: Insufficient cpu",
			  "lastTimestamp":"2026-08-28T10:01:00Z","involvedObject":{"kind":"Pod","name":"b"}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"items":[]}`)
	})
	defer srv.Close()
	ctx := newSrc(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	_ = pollEvents(ctx, "prod", map[string]string{}, map[string]bool{"prod": true})

	ev, ok := ctx.byEv["pod_evicted"].(*PodEvictedEvent)
	if !ok {
		t.Fatalf("应推 pod_evicted，实际 %v", firedNames(ctx))
	}
	if ev.Pod != "a" {
		t.Errorf("pod 名应从 Object 里剥出来：%+v", ev)
	}
	// 原文里带了节点名就该认出来——它决定人去查哪台机器
	if ev.Node != "worker-3" {
		t.Errorf("原文含 from node worker-3，应认出节点：%+v", ev)
	}
	sf, ok := ctx.byEv["schedule_failed"].(*ScheduleFailedEvent)
	if !ok {
		t.Fatalf("应推 schedule_failed，实际 %v", firedNames(ctx))
	}
	if !strings.Contains(sf.Message, "Insufficient cpu") {
		t.Errorf("原文要原样带上——差什么资源全在这句里：%+v", sf)
	}
}

// 认不出节点名就**留空**，不能猜。猜错会把人指向一台没问题的机器。
func TestEvictNodeNotGuessed(t *testing.T) {
	if got := nodeFromEvictMessage("The node was low on resource: memory."); got != "" {
		t.Errorf("原文里没有节点名时必须留空，实际猜成了 %q", got)
	}
	if got := nodeFromEvictMessage("Pod was evicted from node worker-3."); got != "worker-3" {
		t.Errorf("明确写了节点名就要认出来，实际 %q", got)
	}
}

// 崩溃从 **Pod 状态**认，不从 Event 认——OOMKilled 多半不发 Event。
// 而且同一次崩溃会被连着好几轮看到，必须按「结束时刻」去重。
func TestSourceCrashFromPodStatusAndDedups(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
		 {"metadata":{"name":"oom"},"spec":{"nodeName":"n1"},"status":{"phase":"Running",
		   "containerStatuses":[{"ready":true,"restartCount":4,"state":{"running":{}},
		     "lastState":{"terminated":{"reason":"OOMKilled","exitCode":137,"finishedAt":"2026-08-28T10:00:00Z"}}}]}},
		 {"metadata":{"name":"job-done"},"spec":{},"status":{"phase":"Succeeded",
		   "containerStatuses":[{"ready":false,"restartCount":0,"state":{},
		     "lastState":{"terminated":{"reason":"Completed","exitCode":0,"finishedAt":"2026-08-28T10:00:00Z"}}}]}}
		]}`)
	})
	defer srv.Close()
	ctx := newSrc(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	seen := map[string]bool{}

	if err := pollCrashes(ctx, "prod", seen, true); err != nil {
		t.Fatal(err)
	}
	ev, ok := ctx.byEv["pod_crashed"].(*PodCrashedEvent)
	if !ok {
		t.Fatalf("OOMKilled 应推 pod_crashed，实际 %v", firedNames(ctx))
	}
	if ev.Reason != "OOMKilled" || ev.ExitCode != 137 {
		t.Errorf("原因与退出码要带上（区分内存不够 vs 代码崩了）：%+v", ev)
	}
	if len(ctx.fired) != 1 {
		t.Errorf("Completed 是正常结束（Job 跑完），不该报：%v", ctx.fired)
	}
	// 再跑一轮：同一次崩溃不该重复推
	before := len(ctx.fired)
	_ = pollCrashes(ctx, "prod", seen, true)
	if len(ctx.fired) != before {
		t.Errorf("同一次崩溃被重复推了——每分钟一条，直到 pod 被替换：%v", ctx.fired)
	}
}

// 节点只在**状态变化**时报：一个坏了一夜的节点，每轮报一次能刷出几百条。
// 而且恢复后要能再报——否则第二次坏掉时静悄悄。
func TestSourceNodeReportsOnChangeOnly(t *testing.T) {
	state := "Unknown"
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"items":[{"metadata":{"name":"worker-1"},"spec":{},"status":{
		  "conditions":[{"type":"Ready","status":%q,"reason":"NodeStatusUnknown"}],
		  "nodeInfo":{},"capacity":{}}}]}`, state)
	})
	defer srv.Close()
	ctx := newSrc(map[string]string{"kubeconfig": kubeconfigFor(srv)})
	seen := map[string]string{}

	_ = pollNodes(ctx, "prod", seen, true)
	ev, ok := ctx.byEv["node_not_ready"].(*NodeNotReadyEvent)
	if !ok {
		t.Fatalf("应推 node_not_ready，实际 %v", firedNames(ctx))
	}
	// Unknown（失联）与 False（自报不健康）必须分得开——处理方式完全不同
	if ev.Status != "Unknown" {
		t.Errorf("要带 Ready 条件的原值而不是布尔：%+v", ev)
	}
	n := len(ctx.fired)
	_ = pollNodes(ctx, "prod", seen, true)
	if len(ctx.fired) != n {
		t.Errorf("状态没变不该重复报——坏一夜会刷出几百条：%v", ctx.fired)
	}
	// 恢复 → 再坏：必须能再报出来
	state = "True"
	_ = pollNodes(ctx, "prod", seen, true)
	state = "False"
	_ = pollNodes(ctx, "prod", seen, true)
	if len(ctx.fired) != n+1 {
		t.Errorf("恢复后再坏应当再报一次，实际 %v", ctx.fired)
	}
}
