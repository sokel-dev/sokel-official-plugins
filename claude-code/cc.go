package main

// Claude Code 子进程驱动：跑 `claude -p … --output-format stream-json`，逐行解析。
//
// 为什么是子进程而不是 SDK：Claude Agent SDK 只有 Python / TypeScript 绑定，没有 Go 的。
// 对 Go 宿主，CLI 就是官方路径。CLI 没有 --cwd，但 Go 的 cmd.Dir 正好补上这个缺口。
//
// 行协议（实测 2.1.38，只解三类，其余忽略——CC 还会吐 hook/mcp 之类的噪音行，
// 硬性穷举类型早晚会被新版本的新类型打断）：
//
//	{"type":"system","subtype":"init",...}      会话开始：session_id / 模型 / 权限模式
//	{"type":"assistant","message":{content:[…]}} 助手消息：text 块是过程文字，tool_use 块是它在调什么
//	{"type":"result",...}                        最终：result / num_turns / total_cost_usd / permission_denials
//
// **result 行是唯一可信的终态**。进程退出码只说明「进程怎么没的」：0 正常、2 撞成本上限、
// 130/143 被信号打断——撞上限那次其实是有产出的，当失败处理就把已经花掉的钱扔了。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type ccOptions struct {
	Task            string
	Model           string
	Effort          string
	SystemPrompt    string
	PermissionMode  string
	AllowedTools    []string
	DisallowedTools []string
	MaxBudgetUSD    float64
	ResumeSession   string
}

type ccResult struct {
	SessionID   string
	Conclusion  string
	LastText    string // 最后一段助手正文（result 为空时的兜底，见 applyLine）
	OK          bool
	Turns       int
	CostUSD     float64
	DeniedTools []string
	Subtype     string
}

// ccLine 只声明我们要的字段——CC 每行的字段远不止这些，多余的交给 json 忽略。
// CostUsdNear 浮点近似比较（测试用；成本是浮点，直等会因末位抖动假红）。
func (r ccResult) CostUsdNear(want float64) bool {
	d := r.CostUSD - want
	return d < 1e-9 && d > -1e-9
}

type ccLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message"`
	SessionID        string            `json:"session_id"`
	Result           string            `json:"result"`
	IsError          bool              `json:"is_error"`
	NumTurns         int               `json:"num_turns"`
	TotalCostUSD     float64           `json:"total_cost_usd"`
	PermissionDenied []json.RawMessage `json:"permission_denials"`
}

func ccArgs(o ccOptions) []string {
	args := []string{"-p", o.Task, "--output-format", "stream-json", "--verbose"}
	mode := strings.TrimSpace(o.PermissionMode)
	if mode == "" {
		mode = "acceptEdits" // 默认：能改文件，但不是 bypassPermissions 那种全放开
	}
	args = append(args, "--permission-mode", mode)
	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}
	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(o.DisallowedTools, ","))
	}
	if s := strings.TrimSpace(o.Model); s != "" {
		args = append(args, "--model", s)
	}
	if s := strings.TrimSpace(o.Effort); s != "" {
		args = append(args, "--effort", s)
	}
	if s := strings.TrimSpace(o.SystemPrompt); s != "" {
		args = append(args, "--append-system-prompt", s)
	}
	if o.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(o.MaxBudgetUSD, 'f', -1, 64))
	}
	if s := strings.TrimSpace(o.ResumeSession); s != "" {
		args = append(args, "--resume", s)
	}
	return args
}

// ccEnv 子进程环境：API Key 显式给（否则 CC 会去读这台机器上某个人的登录态），
// 代理只给 CC（git 那边反过来，见 workspace.go 顶注）。
// ccEnv 子进程环境：API Key 来自凭证（用谁的额度），代理等其余变量
// 直接继承插件进程自己的环境（CC 本来就读标准的 http_proxy/https_proxy）。
func ccEnv(c Cred) []string {
	e := os.Environ()
	// 只有真给了才注入。**空值不能注入**：ANTHROPIC_API_KEY= 摆在那儿会盖掉机器上
	// CC 自己的登录态，把「用订阅额度」这条合法路径堵死，报错还很难懂。
	if k := strings.TrimSpace(c.APIKey); k != "" {
		e = append(e, "ANTHROPIC_API_KEY="+k)
	}
	return e
}

func claudeBin() string { return cfg.ClaudeBin }

// runClaude 跑一次任务。onText 收过程文字，onTool 收工具调用名（都可为 nil）。
func runClaude(ctx context.Context, c Cred, dir string, o ccOptions, onText, onTool func(string)) (ccResult, error) {
	cmd := exec.CommandContext(ctx, claudeBin(), ccArgs(o)...)
	cmd.Dir = dir
	cmd.Env = ccEnv(c)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ccResult{}, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return ccResult{}, fmt.Errorf("起不动 %s: %w（这台机器装 Claude Code 了吗）", claudeBin(), err)
	}

	var res ccResult
	// 用 Reader 而不是 Scanner：init 那行动辄几十 KB（工具/技能/插件清单全在里面），
	// Scanner 默认 64KB 上限会直接 "token too long" 把整条流判死。
	rd := bufio.NewReader(stdout)
	for {
		line, rerr := rd.ReadString('\n')
		if s := strings.TrimSpace(line); s != "" {
			applyLine(s, &res, onText, onTool)
		}
		if rerr != nil {
			if rerr != io.EOF {
				_ = cmd.Wait()
				return res, fmt.Errorf("读 Claude Code 输出失败: %w", rerr)
			}
			break
		}
	}

	werr := cmd.Wait()
	if res.SessionID == "" && res.Conclusion == "" {
		// 一行有效输出都没有：多半是参数错或认证失败，stderr 才有真话
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && werr != nil {
			msg = werr.Error()
		}
		return res, fmt.Errorf("Claude Code 没有产出: %s", clip(msg, 400))
	}
	// 有 result 行就以它为准：撞成本上限（退出码 2）也是一次有产出的运行。
	if werr != nil && res.Subtype == "" {
		return res, fmt.Errorf("Claude Code 异常退出: %v: %s", werr, clip(strings.TrimSpace(stderr.String()), 300))
	}
	return res, nil
}

func applyLine(line string, res *ccResult, onText, onTool func(string)) {
	var l ccLine
	if json.Unmarshal([]byte(line), &l) != nil {
		return // 不是 JSON 的行（极少数噪音）直接丢，不该让整条流因为一行脏数据断掉
	}
	switch l.Type {
	case "system":
		if l.Subtype == "init" && l.SessionID != "" {
			res.SessionID = l.SessionID
		}
	case "assistant":
		for _, b := range l.Message.Content {
			switch b.Type {
			case "text":
				if b.Text != "" {
					// 记下最后一段正文：**result 有可能是空的**——最后一轮若是工具调用
					// 而不是文字（实报：评审写完又调了一次 TodoWrite），Claude Code 的
					// result 字段就没有内容，而正文其实早就说完了。留着它兜底。
					res.LastText = b.Text
					if onText != nil {
						onText(b.Text)
					}
				}
			case "tool_use":
				if b.Name != "" && onTool != nil {
					onTool(b.Name)
				}
			}
		}
	case "result":
		res.Subtype = l.Subtype
		res.OK = l.Subtype == "success" && !l.IsError
		res.Conclusion = l.Result
		// result 为空就退回最后一段助手正文。
		//
		// 实报：一次 MR 评审 ok=true、turns=25、log 里 4KB 的评审正文俱在，
		// **conclusion 却是空的**——因为最后一轮是 TodoWrite 而不是说话。
		// 下游把 conclusion 贴成 MR 评论，于是评论是空的，而人从界面上只看到
		// 「运行成功」，完全不知道结论去哪了。宁可给正文，也别给一片空白。
		if strings.TrimSpace(res.Conclusion) == "" {
			res.Conclusion = res.LastText
		}
		res.Turns = l.NumTurns
		res.CostUSD = l.TotalCostUSD
		if l.SessionID != "" {
			res.SessionID = l.SessionID
		}
		for _, d := range l.PermissionDenied {
			if name := denialName(d); name != "" {
				res.DeniedTools = append(res.DeniedTools, name)
			}
		}
	}
}

// denialName 从一条权限拒绝记录里取工具名。字段名在版本间变过，挨个试；
// 都取不到就把原文塞进去——**宁可给一段看得懂的原文，也别悄悄丢掉「它想做什么被拦了」**。
func denialName(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return strings.TrimSpace(clip(string(raw), 120))
	}
	for _, k := range []string{"tool_name", "toolName", "name", "tool"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return strings.TrimSpace(clip(string(raw), 120))
}

var _ = os.Getenv

// claudeVersion 体检用：claude --version。
func claudeVersion(ctx context.Context, c Cred) (string, error) {
	cmd := exec.CommandContext(ctx, claudeBin(), "--version")
	cmd.Env = ccEnv(c)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(clip(string(out), 200)))
	}
	return strings.TrimSpace(string(out)), nil
}
