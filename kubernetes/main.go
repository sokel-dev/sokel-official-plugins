// kubernetes -- a first-party Sokel plugin: generic K8s workload operations (not tied to any cloud).
//
// Credential = kubeconfig. Design decisions are documented at the top of schema/schema.go.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./kubernetes
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "kubernetes",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnPods(p, opPods)
	OnPodLogs(p, opPodLogs)
	OnDeployments(p, opDeployments)
	OnDeploymentRestart(p, opDeploymentRestart)
	OnDeploymentScale(p, opDeploymentScale)
	OnEvents(p, opEvents)
	OnNodes(p, opNodes)
	OnHealthCheck(p, opHealthCheck)
	OnDeployWorkload(p, opDeployWorkload)
	OnDeploymentStatus(p, opDeploymentStatus)
	OnRunJob(p, opRunJob)
	OnApplyManifest(p, opApplyManifest)
	OnDeleteObject(p, opDeleteObject)

	// Event source: turns cluster anomalies into workflow triggers (only starts if the
	// credential has watch_namespaces set). Polling is the only path here -- k8s never
	// initiates outbound HTTP, so there's no "platform receives the webhook" option.
	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "poll", Label: "K8s 异常轮询"}, runEvents)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
