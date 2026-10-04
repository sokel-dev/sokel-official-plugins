package main

// workload_ops.go -- five deploy/job operations (deploy_workload / deployment_status / run_job /
// apply_manifest / delete_object).
//
// All write paths uniformly use server-side apply (PATCH + application/apply-patch+yaml,
// fieldManager=sokel): creates if absent, converges to the fields given this time if present --
// naturally idempotent, so a workflow re-run won't blow up on "already exists". kind -> REST
// resource name isn't guessed by pluralization rules; it's looked up from the cluster's
// discovery endpoint (/apis/<gv> self-reports its resource table) and cached.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// -- small shared helpers --

// gvPath: group/version -> REST prefix. The core group (v1) has a special legacy path.
func gvPath(gv string) string {
	if gv == "v1" {
		return "/api/v1"
	}
	return "/apis/" + gv
}

// envList: map -> K8s env array; keys are sorted to keep the manifest stable (apply converges
// by content, so unstable ordering would look like "something changed" every time).
func envList(env map[string]string) []map[string]string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]string{"name": k, "value": env[k]})
	}
	return out
}

// applyObj: server-side-applies one object. Returns whether it was newly created (probed with
// a GET before the apply -- apply itself doesn't distinguish created/configured, but "created
// new vs. changed an existing one" is the answer the caller wants).
func (k *kubeClient) applyObj(ctx context.Context, path string, obj any) (created bool, err error) {
	if _, gerr := k.do(ctx, "GET", path, nil, nil, ""); gerr != nil {
		created = strings.Contains(gerr.Error(), "找不到资源") // kubeErr's 404 translation
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return false, err
	}
	q := url.Values{"fieldManager": {"sokel"}, "force": {"true"}}
	if _, err := k.do(ctx, "PATCH", path, q, body, "application/apply-patch+yaml"); err != nil {
		return false, err
	}
	return created, nil
}

// -- discovery: kind -> REST resource name --

type apiResourceList struct {
	Resources []struct {
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		Namespaced bool   `json:"namespaced"`
	} `json:"resources"`
}

var (
	discCacheMu sync.Mutex
	discCache   = map[string]apiResourceList{} // key: base+gv
)

// resourceFor asks the cluster "for this gv, which resource path does Kind map to, and is it
// namespaced". Subresources (things like deployments/scale with a slash) are skipped -- apply/delete only target top-level objects.
func (k *kubeClient) resourceFor(ctx context.Context, gv, kind string) (resource string, namespaced bool, err error) {
	key := k.base + "|" + gv
	discCacheMu.Lock()
	list, ok := discCache[key]
	discCacheMu.Unlock()
	if !ok {
		raw, derr := k.do(ctx, "GET", gvPath(gv), nil, nil, "")
		if derr != nil {
			return "", false, fmt.Errorf("查询 %s 的资源表失败: %w", gv, derr)
		}
		if jerr := json.Unmarshal(raw, &list); jerr != nil {
			return "", false, jerr
		}
		discCacheMu.Lock()
		discCache[key] = list
		discCacheMu.Unlock()
	}
	for _, r := range list.Resources {
		if r.Kind == kind && !strings.Contains(r.Name, "/") {
			return r.Name, r.Namespaced, nil
		}
	}
	return "", false, fmt.Errorf("%s 里没有 kind=%s——核对 apiVersion 与 Kind 拼写（区分大小写）", gv, kind)
}

// objPath is one object's REST path.
func objPath(gv, resource, ns, name string, namespaced bool) string {
	p := gvPath(gv)
	if namespaced {
		p += "/namespaces/" + url.PathEscape(ns)
	}
	return p + "/" + resource + "/" + url.PathEscape(name)
}

// -- deploy_workload --

func deployManifest(in *DeployWorkloadIn, ns string) map[string]any {
	labels := map[string]string{"app": in.Name}
	for k, v := range in.Labels {
		if k != "app" { // app is the selector anchor; it can't be overridden (the selector is immutable once created, and apply would just be rejected)
			labels[k] = v
		}
	}
	container := map[string]any{"name": in.Name, "image": in.Image}
	if len(in.Command) > 0 {
		container["command"] = in.Command
	}
	if e := envList(in.Env); e != nil {
		container["env"] = e
	}
	limits := map[string]string{}
	if in.CPULimit != "" {
		limits["cpu"] = in.CPULimit
	}
	if in.MemoryLimit != "" {
		limits["memory"] = in.MemoryLimit
	}
	if len(limits) > 0 {
		container["resources"] = map[string]any{"limits": limits}
	}
	podSpec := map[string]any{"containers": []any{container}}
	if in.ImagePullSecret != "" {
		podSpec["imagePullSecrets"] = []any{map[string]string{"name": in.ImagePullSecret}}
	}
	replicas := in.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": in.Name, "namespace": ns, "labels": labels},
		"spec": map[string]any{
			"replicas": replicas,
			"selector": map[string]any{"matchLabels": map[string]string{"app": in.Name}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec":     podSpec,
			},
		},
	}
}

func opDeployWorkload(ctx plugin.Ctx, in *DeployWorkloadIn) (*DeployWorkloadOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Image) == "" {
		return nil, fmt.Errorf("name 与 image 都不能为空")
	}
	ns := nsOf(cred, in.Namespace)
	created, err := k.applyObj(ctx, "/apis/apps/v1/namespaces/"+url.PathEscape(ns)+"/deployments/"+url.PathEscape(in.Name), deployManifest(in, ns))
	if err != nil {
		return nil, err
	}
	return &DeployWorkloadOut{OK: true, Created: created, Name: in.Name, Namespace: ns}, nil
}

// -- deployment_status --

func opDeploymentStatus(ctx plugin.Ctx, in *DeploymentStatusIn) (*DeploymentStatusOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	ns := nsOf(cred, in.Namespace)
	raw, err := k.do(ctx, "GET", "/apis/apps/v1/namespaces/"+url.PathEscape(ns)+"/deployments/"+url.PathEscape(in.Name), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var d struct {
		Spec struct {
			Replicas *int `json:"replicas"`
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
			Conditions        []struct {
				Type    string `json:"type"`
				Status  string `json:"status"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	desired := 1
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	image := ""
	if len(d.Spec.Template.Spec.Containers) > 0 {
		image = d.Spec.Template.Spec.Containers[0].Image
	}
	ready := desired >= 1 && d.Status.ReadyReplicas == desired
	msg := "全部就绪"
	if !ready {
		msg = fmt.Sprintf("就绪 %d/%d", d.Status.ReadyReplicas, desired)
		// When not ready, surface the most recent non-True condition -- "why it is not up" is more useful than a number.
		for _, c := range d.Status.Conditions {
			if c.Status != "True" && (c.Reason != "" || c.Message != "") {
				msg += "：" + firstNonEmpty(c.Message, c.Reason)
				break
			}
		}
	}
	return &DeploymentStatusOut{
		Ready: ready, Desired: desired, ReadyReplicas: d.Status.ReadyReplicas,
		Available: d.Status.AvailableReplicas, Image: image, Message: msg,
	}, nil
}

// -- run_job --

func opRunJob(ctx plugin.Ctx, in *RunJobIn) (*RunJobOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.NamePrefix) == "" || strings.TrimSpace(in.Image) == "" {
		return nil, fmt.Errorf("name_prefix 与 image 都不能为空")
	}
	ns := nsOf(cred, in.Namespace)
	name := in.NamePrefix + "-" + strconv.FormatInt(time.Now().Unix(), 36)
	container := map[string]any{"name": "job", "image": in.Image}
	if len(in.Command) > 0 {
		container["command"] = in.Command
	}
	if e := envList(in.Env); e != nil {
		container["env"] = e
	}
	job := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"spec": map[string]any{
			"backoffLimit":            0,
			"ttlSecondsAfterFinished": 3600,
			"template": map[string]any{
				"spec": map[string]any{"restartPolicy": "Never", "containers": []any{container}},
			},
		},
	}
	body, _ := json.Marshal(job)
	if _, err := k.do(ctx, "POST", "/apis/batch/v1/namespaces/"+url.PathEscape(ns)+"/jobs", nil, body, "application/json"); err != nil {
		return nil, err
	}
	if !in.Wait {
		return &RunJobOut{Job: name, Succeeded: false, Status: "submitted"}, nil
	}
	// Waiting: the timeout cap is bounded by the operation's TimeoutSec(600), leaving 30s of margin to fetch logs.
	limit := in.TimeoutSec
	if limit <= 0 {
		limit = 300
	}
	if limit > 570 {
		limit = 570
	}
	deadline := time.Now().Add(time.Duration(limit) * time.Second)
	status := "running"
	for time.Now().Before(deadline) {
		raw, gerr := k.do(ctx, "GET", "/apis/batch/v1/namespaces/"+url.PathEscape(ns)+"/jobs/"+url.PathEscape(name), nil, nil, "")
		if gerr != nil {
			return nil, gerr
		}
		var st struct {
			Status struct {
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"status"`
		}
		_ = json.Unmarshal(raw, &st)
		if st.Status.Succeeded > 0 {
			status = "succeeded"
			break
		}
		if st.Status.Failed > 0 {
			status = "failed"
			break
		}
		time.Sleep(3 * time.Second)
	}
	logs := k.jobLogs(ctx, ns, name)
	return &RunJobOut{Job: name, Succeeded: status == "succeeded", Status: status, Logs: logs}, nil
}

// jobLogs fetches the tail of the Job's first pod's log. Failing to get it isn't an error -- the log is a garnish, the job result is the main course.
func (k *kubeClient) jobLogs(ctx context.Context, ns, job string) string {
	raw, err := k.do(ctx, "GET", "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods",
		url.Values{"labelSelector": {"job-name=" + job}}, nil, "")
	if err != nil {
		return ""
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &pods) != nil || len(pods.Items) == 0 {
		return ""
	}
	lg, err := k.do(ctx, "GET", "/api/v1/namespaces/"+url.PathEscape(ns)+"/pods/"+url.PathEscape(pods.Items[0].Metadata.Name)+"/log",
		url.Values{"tailLines": {"200"}}, nil, "")
	if err != nil {
		return ""
	}
	return string(lg)
}

// -- apply_manifest --

func opApplyManifest(ctx plugin.Ctx, in *ApplyManifestIn) (*ApplyManifestOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	var lines []string
	applied := 0
	for _, doc := range strings.Split(in.Manifest, "\n---") {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		js, yerr := yaml.YAMLToJSON([]byte(doc))
		if yerr != nil {
			return nil, fmt.Errorf("第 %d 个文档不是合法 YAML: %w", applied+1, yerr)
		}
		var meta struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if json.Unmarshal(js, &meta) != nil || meta.APIVersion == "" || meta.Kind == "" || meta.Metadata.Name == "" {
			return nil, fmt.Errorf("第 %d 个文档缺 apiVersion/kind/metadata.name", applied+1)
		}
		resource, namespaced, rerr := k.resourceFor(ctx, meta.APIVersion, meta.Kind)
		if rerr != nil {
			return nil, rerr
		}
		// Namespace: what's written in the document takes priority over the operation input, which takes priority over the credential default.
		ns := firstNonEmpty(meta.Metadata.Namespace, nsOf(cred, in.Namespace))
		var obj map[string]any
		_ = json.Unmarshal(js, &obj)
		if namespaced {
			// The apply object must have a namespace, otherwise the server converges on default,
			// which wouldn't match the path.
			md, _ := obj["metadata"].(map[string]any)
			if md != nil && md["namespace"] == nil {
				md["namespace"] = ns
			}
		}
		created, aerr := k.applyObj(ctx, objPath(meta.APIVersion, resource, ns, meta.Metadata.Name, namespaced), obj)
		if aerr != nil {
			return nil, fmt.Errorf("%s/%s: %w", meta.Kind, meta.Metadata.Name, aerr)
		}
		verb := "configured"
		if created {
			verb = "created"
		}
		lines = append(lines, fmt.Sprintf("%s/%s → %s", meta.Kind, meta.Metadata.Name, verb))
		applied++
	}
	if applied == 0 {
		return nil, fmt.Errorf("清单里没有任何文档")
	}
	return &ApplyManifestOut{OK: true, Applied: applied, Items: strings.Join(lines, "\n")}, nil
}

// -- delete_object --

// deleteProbeGVs: the groups probed by commonality when apiVersion isn't given.
var deleteProbeGVs = []string{"apps/v1", "batch/v1", "v1", "networking.k8s.io/v1"}

func opDeleteObject(ctx plugin.Ctx, in *DeleteObjectIn) (*DeleteObjectOut, error) {
	cred := credOf(ctx)
	k, err := clientOf(cred)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Kind) == "" || strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("kind 与 name 都不能为空")
	}
	gvs := deleteProbeGVs
	if in.APIVersion != "" {
		gvs = []string{in.APIVersion}
	}
	var resource, gv string
	var namespaced bool
	for _, g := range gvs {
		r, n, rerr := k.resourceFor(ctx, g, in.Kind)
		if rerr == nil {
			resource, namespaced, gv = r, n, g
			break
		}
	}
	if resource == "" {
		return nil, fmt.Errorf("在 %s 里都找不到 kind=%s——冷门资源请显式给 api_version", strings.Join(gvs, "、"), in.Kind)
	}
	ns := nsOf(cred, in.Namespace)
	if _, err := k.do(ctx, "DELETE", objPath(gv, resource, ns, in.Name, namespaced), nil, nil, ""); err != nil {
		return nil, err
	}
	return &DeleteObjectOut{OK: true, Message: fmt.Sprintf("已删除 %s/%s（%s）", in.Kind, in.Name, ns)}, nil
}
