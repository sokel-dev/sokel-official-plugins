package main

// Claude Code subprocess driver: runs `claude -p … --output-format stream-json` and parses it line by line.
//
// Why a subprocess instead of the SDK: the Claude Agent SDK only has Python / TypeScript bindings, no Go one.
// For a Go host, the CLI is the official path. The CLI has no --cwd, but Go's cmd.Dir fills that gap nicely.
//
// Line protocol (observed on 2.1.38; we only parse three kinds and ignore the rest — CC also emits
// hook/mcp-type noise lines, and hard-coding an exhaustive list of types would break on the next version anyway):
//
//	{"type":"system","subtype":"init",...}      session start: session_id / model / permission mode
//	{"type":"assistant","message":{content:[…]}} assistant message: text blocks are progress text, tool_use blocks are what it's calling
//	{"type":"result",...}                        final: result / num_turns / total_cost_usd / permission_denials
//
// **The result line is the only trustworthy final state.** The process exit code only tells you "how the
// process ended": 0 normal, 2 hit the cost budget, 130/143 killed by a signal — but hitting the budget still
// produced real output, and treating it as a failure would throw away money that was already spent.

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
	LastText    string // last assistant text block (fallback when result is empty, see applyLine)
	OK          bool
	Turns       int
	CostUSD     float64
	DeniedTools []string
	Subtype     string
}

// ccLine only declares the fields we need — each CC line has far more fields than this, and the rest are
// left for json to ignore.
// CostUsdNear does an approximate float comparison (for tests; cost is a float, and an exact comparison
// would false-fail on trailing-digit jitter).
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
		mode = "acceptEdits" // default: can edit files, but not the fully-open bypassPermissions mode
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

// ccEnv builds the subprocess environment: the API key is passed explicitly (otherwise CC would read
// whatever user's login state happens to be on this machine), and the proxy is given only to CC (the
// git side does it the other way around, see the top comment in workspace.go).
// ccEnv subprocess environment: the API key comes from the credential (whose quota to use), while the
// proxy and other variables are simply inherited from the plugin process's own environment (CC already
// reads the standard http_proxy/https_proxy).
func ccEnv(c Cred) []string {
	e := os.Environ()
	// Only inject it if one was actually given. **An empty value must not be injected**: a bare
	// ANTHROPIC_API_KEY= would shadow CC's own login state on the machine, blocking the legitimate
	// "use the subscription quota" path with a confusing error.
	if k := strings.TrimSpace(c.APIKey); k != "" {
		e = append(e, "ANTHROPIC_API_KEY="+k)
	}
	return e
}

func claudeBin() string { return cfg.ClaudeBin }

// runClaude runs one task. onText receives progress text, onTool receives tool call names (both may be nil).
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
	// Use a Reader instead of a Scanner: the init line alone can be tens of KB (it carries the whole
	// tools/skills/plugins inventory), and Scanner's default 64KB limit would kill the whole stream
	// outright with "token too long".
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
		// Not a single valid output line: most likely a bad argument or an auth failure, and stderr
		// is where the real explanation lives.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && werr != nil {
			msg = werr.Error()
		}
		return res, fmt.Errorf("Claude Code 没有产出: %s", clip(msg, 400))
	}
	// If there's a result line, trust it: hitting the cost budget (exit code 2) is still a run that
	// produced output.
	if werr != nil && res.Subtype == "" {
		return res, fmt.Errorf("Claude Code 异常退出: %v: %s", werr, clip(strings.TrimSpace(stderr.String()), 300))
	}
	return res, nil
}

func applyLine(line string, res *ccResult, onText, onTool func(string)) {
	var l ccLine
	if json.Unmarshal([]byte(line), &l) != nil {
		return // not a JSON line (rare noise) — drop it; one bad line shouldn't break the whole stream
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
					// Record the last text block: **result can be empty** — if the last turn is a
					// tool call rather than text (observed: the review was already written, then it
					// called TodoWrite once more), Claude Code's result field ends up empty even
					// though the actual text was already said. Keep this as a fallback.
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
		// Fall back to the last assistant text block when result is empty.
		//
		// Observed: an MR review with ok=true, turns=25, and a full 4KB review body sitting right
		// there in the log, yet **conclusion was empty** — because the last turn was a TodoWrite call,
		// not text. Downstream pastes conclusion as the MR comment, so the comment ends up blank while
		// the person only sees "run succeeded" in the UI, with no idea where the conclusion went.
		// Better to give them the text body than a blank.
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

// denialName extracts the tool name from a permission-denial record. The field name has changed across
// versions, so we try each candidate in turn; if none matches, fall back to the raw text — **better to
// give a readable raw snippet than to silently lose the fact that something got blocked**.
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

// claudeVersion is for health checks: claude --version.
func claudeVersion(ctx context.Context, c Cred) (string, error) {
	cmd := exec.CommandContext(ctx, claudeBin(), "--version")
	cmd.Env = ccEnv(c)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(clip(string(out), 200)))
	}
	return strings.TrimSpace(string(out)), nil
}
