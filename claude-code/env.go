package main

// 部署配置：插件进程的环境变量。
//
// 与凭证的分工是一条线：**凭证回答「用谁的额度」，环境回答「这台机器能干什么」。**
// 仓库怎么克隆、工作区放哪、claude 在哪、走不走代理、允不允许碰工作区之外的目录——
// 这些全是后者。它们由部署这台机器的人定，而不是由在画布上选凭证的人定。
//
//	SOKEL_CC_GIT_BASE      GitLab 根地址（如 https://git.example.com）
//	SOKEL_CC_GIT_TOKEN     克隆/推送令牌。**不设就用这台机器上 git 自己的认证**
//	                       （credential helper / .netrc / SSH），这是更干净的做法
//	SOKEL_CC_WORKSPACE     仓库与工作树放哪，默认 <临时目录>/sokel-claude-code
//	SOKEL_CC_CLAUDE_BIN    claude 可执行路径，默认从 PATH 找
//	SOKEL_CC_ALLOW_EXTERNAL_DIRS=1  允许「继续任务」指定工作区之外的目录
//	http_proxy/https_proxy CC 出站代理——**不用插件转发，CC 自己就读这些标准变量**；
//	                       git 那边由 gitEnv() 主动剥掉（内网仓库不能走代理）

import (
	"os"
	"path/filepath"
	"strings"
)

type deployEnv struct {
	GitBase   string
	GitToken  string
	Workspace string
	ClaudeBin string
	AllowExt  bool
}

func loadEnv() deployEnv {
	e := deployEnv{
		GitBase:   strings.TrimRight(strings.TrimSpace(os.Getenv("SOKEL_CC_GIT_BASE")), "/"),
		GitToken:  strings.TrimSpace(os.Getenv("SOKEL_CC_GIT_TOKEN")),
		Workspace: strings.TrimSpace(os.Getenv("SOKEL_CC_WORKSPACE")),
		ClaudeBin: strings.TrimSpace(os.Getenv("SOKEL_CC_CLAUDE_BIN")),
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SOKEL_CC_ALLOW_EXTERNAL_DIRS"))) {
	case "1", "true", "yes", "on":
		e.AllowExt = true
	}
	if e.Workspace == "" {
		e.Workspace = filepath.Join(os.TempDir(), "sokel-claude-code")
	}
	if e.ClaudeBin == "" {
		e.ClaudeBin = "claude"
	}
	return e
}

// cfg 进程级只读部署配置。启动时读一次：运行中改环境变量不该让同一个进程的行为漂移。
var cfg = loadEnv()
