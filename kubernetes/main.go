// kubernetes —— Sokel 第一方插件：通用 K8s 工作负载操作（不绑任何云）。
//
// 凭证 = kubeconfig。设计判断见 schema/schema.go 顶注。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./kubernetes
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

	// 事件源：把集群异常推成工作流触发（凭证填了 watch_namespaces 才启动）。
	// **只有轮询这一条来路**——k8s 不会主动往外发 HTTP，所以没有「平台代收 webhook」。
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
