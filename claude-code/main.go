// claude-code — a first-party Sokel plugin: wires the local Claude Code CLI into a workflow.
//
// Give it a GitLab project and a task description, and it works in a real worktree; progress streams
// back live, and the conclusion/changed-files list/cost are passed downstream as outputs. See the top
// comment in schema/schema.go for the design rationale.
//
// **Deployment constraint**: this plugin must run on a machine that has access to the internal GitLab,
// has claude installed, and has disk space.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./claude-code
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
