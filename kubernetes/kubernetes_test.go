package main

// httptest fakes the API server to exercise every path. kubeconfig points at the fake server
// (no TLS verification); real-cluster integration testing goes through operation:test.

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

// kubeconfigFor is a minimal kubeconfig pointing at the fake server.
// Must be TLS + insecure-skip-tls-verify: client-go silently drops the token for a plain
// http:// server (it won't send credentials in the clear), so the Authorization header would be
// missing -- real clusters are always TLS so this never comes up there, but if the fake server
// used NewServer, this test would silently fail to exercise the auth header at all.
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

// pods: ready count, abnormal-reason extraction, only_abnormal filtering -- all three verified together.
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

// rollout restart = a strategic-merge PATCH that changes the template annotation; the Bearer token must be sent with the request.
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

// scale: GET /scale to read the previous value first, then PATCH; scaling to 0 is valid (stops the service), negative is rejected.
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

// Events in reverse order (most recent first) + Warning filtering pushed down to fieldSelector.
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

// An RBAC 403 must be translated into "go add permissions", not raw JSON.
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

// nodes: the not_ready count is the direct criterion for alerting workflows.
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

// When a multi-container pod doesn't specify a container name, K8s's error must be translated into plain language.
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

// The credential only gives a server address, not the full YAML -- the most common paste mistake; the error must point the way.
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

// -- Anomaly event monitoring: these correspond to the three categories users actually care about (eviction / scheduling failure / abnormal exit) --

// OOMKilled only shows up in lastState: the container is restarted immediately after being
// killed, so the current state is already Running, with nothing visible in reason. Looking only
// at restarts tells you it's restarting repeatedly, but not whether it's out of memory, crashed,
// or evicted -- and these three need completely different handling.
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
	// The current state is normal, so reason should be empty -- exactly why lastState must be checked separately
	if p.Reason != "" {
		t.Errorf("当前 state 是 Running，reason 该为空：%+v", p)
	}
}

// With multiple containers, take the most recently terminated one: reporting the earliest one would point someone at a problem that's already fixed.
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

// The crux of polling: compare by "most recently occurred", not first occurrence.
// k8s doesn't create a new entry for a repeated event, it increments count and updates
// lastTimestamp -- comparing by first occurrence would mean a failure lasting two hours only
// gets reported in the first round, and forever looks older than the cursor after that (the
// alert fires once and then never again).
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

	// Cursor is after the first occurrence but before the most recent one -> it must still be reported (the failure is ongoing)
	out, err := opEvents(ctx, &EventsIn{Since: "2026-08-28T09:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("持续中的故障必须继续报，否则告警只响一次：%+v", out)
	}
	// Cursor is after the most recent occurrence -> shouldn't be reported again
	out2, _ := opEvents(ctx, &EventsIn{Since: "2026-08-28T12:00:00Z"})
	if len(out2.Events) != 0 {
		t.Errorf("比游标旧的不该再报：%+v", out2.Events)
	}
	// checked_at must be directly usable as the next round's cursor
	if out.CheckedAt == "" {
		t.Error("要给 checked_at，否则调用方没法接着轮询")
	}
}

// Clusters on the newer events.k8s.io don't provide lastTimestamp, only eventTime.
// Recognizing only one field would make the cursor permanently empty and filtering permanently
// ineffective on that half of clusters.
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

// Truncation must be reported. It used to truncate silently, which would quietly drop alerts whenever the cluster got busy.
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

// Filtering by reason: when watching for specific anomalies, this saves fetching everything and filtering it yourself.
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
	out, _ := opEvents(ctx, &EventsIn{Reasons: []string{"Evicted", "failedscheduling"}}) // case-insensitive
	if len(out.Events) != 2 {
		t.Fatalf("应只留 Evicted 与 FailedScheduling：%+v", out.Events)
	}
	for _, e := range out.Events {
		if e.Reason == "Unhealthy" {
			t.Error("没点名的原因不该出现")
		}
	}
}

// -- Event source: turns anomalies into triggers. Every mistake here fails silently -- the result is just alerts becoming noise or disappearing entirely --

type fakeSource struct {
	*fakeCtx
	fired []string       // event ids, in order
	byEv  map[string]any // event name -> the last payload for it
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

// The first round doesn't trigger: the moment the plugin starts, the whole pile of the
// cluster's historical anomalies gets fetched in full. Sending them as-is would flood the
// workflow with dozens of stale alerts in the plugin's first minute -- the user's first
// impression would be "this thing is broken", and they'd turn the trigger off.
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
	// Only pushes on the second round (once primed)
	primed["prod"] = true
	if err := pollEvents(ctx, "prod", cursor, primed); err != nil {
		t.Fatal(err)
	}
	if len(ctx.fired) != 1 {
		t.Fatalf("第二轮该推 1 条，实际 %v", ctx.fired)
	}
}

// Dispatching by reason to different events: eviction and scheduling failure are two different
// kinds of problem, and merging them into one would leave downstream unable to handle them
// separately (one needs to look at node memory, the other at cluster capacity).
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
	// If the message names a node, it should be recognized -- it determines which machine someone goes to check
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

// Leave it blank if the node name can't be recognized -- don't guess. Guessing wrong points someone at a machine that's actually fine.
func TestEvictNodeNotGuessed(t *testing.T) {
	if got := nodeFromEvictMessage("The node was low on resource: memory."); got != "" {
		t.Errorf("原文里没有节点名时必须留空，实际猜成了 %q", got)
	}
	if got := nodeFromEvictMessage("Pod was evicted from node worker-3."); got != "worker-3" {
		t.Errorf("明确写了节点名就要认出来，实际 %q", got)
	}
}

// Crashes are recognized from Pod status, not from Event -- OOMKilled mostly doesn't emit an
// Event. And the same crash gets seen for several rounds in a row, so dedup must be by finish time.
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
	// Run another round: the same crash shouldn't be pushed again
	before := len(ctx.fired)
	_ = pollCrashes(ctx, "prod", seen, true)
	if len(ctx.fired) != before {
		t.Errorf("同一次崩溃被重复推了——每分钟一条，直到 pod 被替换：%v", ctx.fired)
	}
}

// Nodes are only reported on a status change: a node that's been down all night would flood
// hundreds of reports at one per round. And after recovery it must be reportable again --
// otherwise a second failure goes unnoticed.
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
	// Unknown (lost contact) and False (self-reported unhealthy) must be distinguishable -- they need completely different handling
	if ev.Status != "Unknown" {
		t.Errorf("要带 Ready 条件的原值而不是布尔：%+v", ev)
	}
	n := len(ctx.fired)
	_ = pollNodes(ctx, "prod", seen, true)
	if len(ctx.fired) != n {
		t.Errorf("状态没变不该重复报——坏一夜会刷出几百条：%v", ctx.fired)
	}
	// Recovers -> fails again: must be reportable again
	state = "True"
	_ = pollNodes(ctx, "prod", seen, true)
	state = "False"
	_ = pollNodes(ctx, "prod", seen, true)
	if len(ctx.fired) != n+1 {
		t.Errorf("恢复后再坏应当再报一次，实际 %v", ctx.fired)
	}
}
