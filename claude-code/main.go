// claude-code —— Sokel 第一方插件：把本机的 Claude Code 接进工作流。
//
// 给它一个 GitLab 项目 + 一句任务，它在真实工作树里干活；过程实时回传，
// 结论/改动清单/成本作为出参进下游节点。设计判断见 schema/schema.go 顶注。
//
// **部署约束**：这个插件必须跑在「能访问内网 GitLab + 装了 claude + 有磁盘」的机器上。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./claude-code
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
		Name:     "claude-code",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnRunTask(p, opRunTask)
	OnResumeTask(p, opResumeTask)
	OnListWorktrees(p, opListWorktrees)
	OnCleanup(p, opCleanup)
	OnHealthCheck(p, opHealthCheck)

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
