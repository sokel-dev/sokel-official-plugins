package main

// 部署/任务五操作的假 server 测试。断言的是**发出去的请求长什么样**
//（路径/apply 语义/清单内容）——那才是这些操作的全部契约。

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func readAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }

func notFoundJSON(w http.ResponseWriter) {
	w.WriteHeader(404)
	fmt.Fprint(w, `{"message":"not found","reason":"NotFound"}`)
}

func TestDeployWorkloadCreatesWithApply(t *testing.T) {
	var patched []byte
	var patchQuery, patchCT string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/deployments/myapp"):
			notFoundJSON(w) // 不存在 → created
		case r.Method == "PATCH" && strings.HasSuffix(r.URL.Path, "/deployments/myapp"):
			patched, _ = readAll(r)
			patchQuery = r.URL.RawQuery
			patchCT = r.Header.Get("Content-Type")
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("意外请求 %s %s", r.Method, r.URL.Path)
			notFoundJSON(w)
		}
	})
	defer srv.Close()
	out, err := opDeployWorkload(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &DeployWorkloadIn{
		Namespace: "prod", Name: "myapp", Image: "reg.example.com/app:v2",
		Env: map[string]string{"B": "2", "A": "1"}, Replicas: 2, ImagePullSecret: "acr-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || !out.Created || out.Namespace != "prod" {
		t.Fatalf("应为新建成功: %+v", out)
	}
	if !strings.Contains(patchCT, "apply-patch+yaml") || !strings.Contains(patchQuery, "fieldManager=sokel") {
		t.Fatalf("必须走 server-side apply: ct=%q q=%q", patchCT, patchQuery)
	}
	body := string(patched)
	// env 键排序（清单稳定）；app 标签锚选择器；拉取密钥入 podSpec。
	if !strings.Contains(body, `"image":"reg.example.com/app:v2"`) ||
		strings.Index(body, `"name":"A"`) > strings.Index(body, `"name":"B"`) ||
		!strings.Contains(body, `"matchLabels":{"app":"myapp"}`) ||
		!strings.Contains(body, `"imagePullSecrets"`) ||
		!strings.Contains(body, `"replicas":2`) {
		t.Fatalf("清单不对: %s", body)
	}
}

func TestDeployWorkloadUpdateNotCreated(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`) // GET 命中 → 已存在；PATCH 也 200
	})
	defer srv.Close()
	out, err := opDeployWorkload(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &DeployWorkloadIn{Name: "x", Image: "img"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Created {
		t.Fatal("已存在的应为更新, created 必须是 false")
	}
}

func TestDeploymentStatusNotReadyCarriesReason(t *testing.T) {
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"spec":{"replicas":3,"template":{"spec":{"containers":[{"image":"app:v1"}]}}},
			"status":{"readyReplicas":1,"availableReplicas":1,
			"conditions":[{"type":"Available","status":"False","reason":"MinimumReplicasUnavailable","message":"Deployment does not have minimum availability."}]}}`)
	})
	defer srv.Close()
	out, err := opDeploymentStatus(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &DeploymentStatusIn{Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Ready || out.Desired != 3 || out.ReadyReplicas != 1 || out.Image != "app:v1" {
		t.Fatalf("状态解析不对: %+v", out)
	}
	if !strings.Contains(out.Message, "1/3") || !strings.Contains(out.Message, "minimum availability") {
		t.Fatalf("未就绪要带原因: %q", out.Message)
	}
}

func TestRunJobWaitsAndFetchesLogs(t *testing.T) {
	var jobName string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/jobs"):
			var j struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Spec struct {
					BackoffLimit int `json:"backoffLimit"`
				} `json:"spec"`
			}
			raw, _ := readAll(r)
			_ = json.Unmarshal(raw, &j)
			jobName = j.Metadata.Name
			if j.Spec.BackoffLimit != 0 {
				t.Errorf("失败不重试: backoffLimit 应为 0, 实际 %d", j.Spec.BackoffLimit)
			}
			fmt.Fprint(w, `{}`)
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/jobs/"):
			fmt.Fprint(w, `{"status":{"succeeded":1}}`)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/pods"):
			if !strings.Contains(r.URL.RawQuery, "job-name") {
				t.Errorf("找 pod 要按 job-name 标签: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"items":[{"metadata":{"name":"pj-1"}}]}`)
		case strings.HasSuffix(r.URL.Path, "/log"):
			fmt.Fprint(w, "done line")
		default:
			t.Errorf("意外请求 %s %s", r.Method, r.URL.Path)
			notFoundJSON(w)
		}
	})
	defer srv.Close()
	out, err := opRunJob(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &RunJobIn{
		NamePrefix: "clean", Image: "busybox", Wait: true, TimeoutSec: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Succeeded || out.Status != "succeeded" || !strings.HasPrefix(out.Job, "clean-") || out.Job != jobName {
		t.Fatalf("任务结果不对: %+v (提交名 %q)", out, jobName)
	}
	if !strings.Contains(out.Logs, "done line") {
		t.Fatalf("要带回日志尾部: %q", out.Logs)
	}
}

func TestApplyManifestMultiDocResolvesResources(t *testing.T) {
	var paths []string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis/apps/v1":
			fmt.Fprint(w, `{"resources":[{"name":"deployments","kind":"Deployment","namespaced":true},{"name":"deployments/scale","kind":"Scale","namespaced":true}]}`)
		case r.URL.Path == "/api/v1":
			fmt.Fprint(w, `{"resources":[{"name":"services","kind":"Service","namespaced":true}]}`)
		case r.Method == "GET":
			notFoundJSON(w)
		case r.Method == "PATCH":
			paths = append(paths, r.URL.Path)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("意外请求 %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	discCache = map[string]apiResourceList{} // 测试间隔离缓存
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata: {name: web}
spec: {}
---
apiVersion: v1
kind: Service
metadata: {name: web-svc, namespace: infra}
spec: {}`
	out, err := opApplyManifest(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv), "namespace": "prod"}), &ApplyManifestIn{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	if out.Applied != 2 || !strings.Contains(out.Items, "Deployment/web → created") {
		t.Fatalf("应用结果不对: %+v", out)
	}
	// 无 ns 的文档吃凭证默认 prod;写了 ns 的按文档来。
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "/apis/apps/v1/namespaces/prod/deployments/web") ||
		!strings.Contains(joined, "/api/v1/namespaces/infra/services/web-svc") {
		t.Fatalf("路径不对: %v", paths)
	}
}

func TestDeleteObjectProbesGroups(t *testing.T) {
	var deleted string
	srv := fakeAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/apps/v1":
			fmt.Fprint(w, `{"resources":[{"name":"deployments","kind":"Deployment","namespaced":true}]}`)
		case "/apis/batch/v1":
			fmt.Fprint(w, `{"resources":[{"name":"jobs","kind":"Job","namespaced":true}]}`)
		default:
			if r.Method == "DELETE" {
				deleted = r.URL.Path
				fmt.Fprint(w, `{}`)
				return
			}
			t.Errorf("意外请求 %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	discCache = map[string]apiResourceList{}
	out, err := opDeleteObject(newFake(map[string]string{"kubeconfig": kubeconfigFor(srv)}), &DeleteObjectIn{Kind: "Job", Name: "clean-1", Namespace: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || deleted != "/apis/batch/v1/namespaces/prod/jobs/clean-1" {
		t.Fatalf("删除路径不对: %q (%+v)", deleted, out)
	}
}
