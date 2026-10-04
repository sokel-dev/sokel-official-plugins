package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Line parsing. The fixture is **actual output from a real Claude Code 2.1.38 run** (with noisy fields
// trimmed) — synthetic fixtures would be misleading here: field names, nesting depth, and which line
// actually carries session_id are all things you can't just guess at.
func TestApplyLineOnRealOutput(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"hook_started","hook_name":"SessionStart:startup","session_id":"1cf78139"}`,
		`{"type":"system","subtype":"init","cwd":"/private/tmp","session_id":"1cf78139","model":"claude-opus-4-6[1m]","permissionMode":"dontAsk","claude_code_version":"2.1.38"}`,
		`{"type":"assistant","message":{"model":"claude-opus-4-6","content":[{"type":"text","text":"收到"}],"usage":{"output_tokens":6}},"session_id":"1cf78139"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}]},"session_id":"1cf78139"}`,
		`{"type":"result","subtype":"success","is_error":false,"duration_ms":4222,"num_turns":3,"result":"改完了，测试通过","session_id":"1cf78139","total_cost_usd":0.14858375,"permission_denials":[{"tool_name":"WebFetch"}]}`,
	}

	var res ccResult
	var texts, tools []string
	for _, l := range lines {
		applyLine(l, &res, func(s string) { texts = append(texts, s) }, func(s string) { tools = append(tools, s) })
	}

	if res.SessionID != "1cf78139" {
		t.Errorf("session_id 该从 init/result 行取到，got %q", res.SessionID)
	}
	if !res.OK || res.Conclusion != "改完了，测试通过" {
		t.Errorf("result 行该定终态，got ok=%v conclusion=%q", res.OK, res.Conclusion)
	}
	if res.Turns != 3 || res.CostUsdNear(0.14858375) == false {
		t.Errorf("轮数/成本该出到出参，got turns=%d cost=%v", res.Turns, res.CostUSD)
	}
	if len(texts) != 1 || texts[0] != "收到" {
		t.Errorf("只有 assistant 的 text 块算过程文字，got %+v", texts)
	}
	if len(tools) != 1 || tools[0] != "Bash" {
		t.Errorf("tool_use 块该报出工具名，got %+v", tools)
	}
	// A tool blocked by permissions must come through: when a task isn't finished, this is the first
	// thing to check
	if len(res.DeniedTools) != 1 || res.DeniedTools[0] != "WebFetch" {
		t.Errorf("permission_denials 该解出工具名，got %+v", res.DeniedTools)
	}
	// hook noise lines must not affect anything (CC emits plenty of hook/mcp lines)
	if len(texts) > 1 {
		t.Error("hook 行不该被当成过程文字")
	}
}

// A bad line must not kill the whole stream — a line that isn't JSON gets dropped, and parsing continues
// normally afterward.
func TestApplyLineSurvivesGarbage(t *testing.T) {
	var res ccResult
	applyLine(`Debugger attached.`, &res, nil, nil)
	applyLine(`{"type":"result","subtype":"success","result":"ok","session_id":"s1"}`, &res, nil, nil)
	if res.Conclusion != "ok" || res.SessionID != "s1" {
		t.Fatalf("脏行之后该照常解析，got %+v", res)
	}
}

// Argument assembly. These flag spellings were checked against the local `claude --help`:
// --allowedTools is camelCase, the filter syntax uses a colon, and the cost cap flag is --max-budget-usd.
// The --bare / --max-turns flags that float around in docs simply don't exist in 2.1.38 — using them
// just fails to run.
func TestCCArgs(t *testing.T) {
	args := strings.Join(ccArgs(ccOptions{
		Task: "修一下", Model: "opus", Effort: "high", MaxBudgetUSD: 2.5,
		AllowedTools: []string{"Read", "Edit", "Bash(git:*)"},
		SystemPrompt: "别改 vendor/",
	}), " ")

	for _, want := range []string{
		"-p 修一下", "--output-format stream-json", "--verbose",
		"--permission-mode acceptEdits", // this is the default when none is given, not bypassPermissions
		"--allowedTools Read,Edit,Bash(git:*)",
		"--model opus", "--effort high", "--max-budget-usd 2.5",
		"--append-system-prompt 别改 vendor/",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("缺 %q\n完整: %s", want, args)
		}
	}
	for _, banned := range []string{"--bare", "--max-turns"} {
		if strings.Contains(args, banned) {
			t.Errorf("%s 在 2.1.38 不存在，不该出现", banned)
		}
	}
}

func TestCCArgsResumeAndModeOverride(t *testing.T) {
	args := strings.Join(ccArgs(ccOptions{
		Task: "接着改", ResumeSession: "sess-1", PermissionMode: "dontAsk",
		DisallowedTools: []string{"WebFetch"},
	}), " ")
	if !strings.Contains(args, "--resume sess-1") {
		t.Errorf("继续任务该带 --resume，got %s", args)
	}
	if !strings.Contains(args, "--permission-mode dontAsk") || strings.Contains(args, "acceptEdits") {
		t.Errorf("显式给的权限模式该覆盖默认，got %s", args)
	}
	if !strings.Contains(args, "--disallowedTools WebFetch") {
		t.Errorf("禁用清单该带上，got %s", args)
	}
}

// The token must never land in the repo config: the clean URL is the one written into .git/config, and
// it must not contain the token. The worktree is a directory CC can read — a token sitting there is a
// token handed over.
func TestRepoURLKeepsTokenOutOfConfig(t *testing.T) {
	cfg = deployEnv{GitBase: "https://git.example.com", GitToken: "glpat-secret"}
	t.Cleanup(func() { cfg = loadEnv() })
	clean, authed, err := repoURL("backend/server")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clean, "glpat-secret") {
		t.Errorf("写进 .git/config 的地址不能带令牌: %s", clean)
	}
	if clean != "https://git.example.com/backend/server.git" {
		t.Errorf("clean 地址不对: %s", clean)
	}
	if !strings.Contains(authed, "glpat-secret") || !strings.Contains(authed, "oauth2:") {
		t.Errorf("命令行用的地址该带令牌: %s", authed)
	}
}

// Directory name: using group/name directly as a path would dig an extra directory level, nesting two
// projects' worktrees inside each other.
func TestSlugFlattens(t *testing.T) {
	for in, want := range map[string]string{
		"backend/server":  "backend_server",
		"cc/2026-fix":     "cc_2026-fix",
		"/leading/slash/": "leading_slash",
		"a/../../etc/pwd": "a_____etc_pwd", // each slash and .. gets its own underscore, can't escape the directory
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// The proxy goes only to CC: git's environment must have the proxy variables stripped clean, otherwise
// cloning an internal repo hangs on the proxy until it times out, and the error looks exactly like
// network flakiness, which makes it hard to even suspect the proxy.
func TestProxySplit(t *testing.T) {
	t.Setenv("http_proxy", "http://proxy:8118")
	t.Setenv("HTTPS_PROXY", "http://proxy:8118")

	for _, kv := range gitEnv() {
		k := strings.ToLower(kv[:strings.IndexByte(kv, '=')+1])
		// Only check the actual outbound proxy variables: GOPROXY is the Go module source, and
		// stripping it would break go commands instead
		if k == "http_proxy=" || k == "https_proxy=" || k == "all_proxy=" {
			t.Errorf("git 环境里不该有代理: %s", kv)
		}
	}
	// CC directly inherits the process environment (it already reads the standard
	// http_proxy/https_proxy on its own) — the plugin no longer forwards it from the credential, since
	// the proxy is "how this machine reaches the outside", a deployment property.
	ccHas, keyed := false, false
	for _, kv := range ccEnv(Cred{APIKey: "k"}) {
		if kv == "https_proxy=http://proxy:8118" || kv == "HTTPS_PROXY=http://proxy:8118" {
			ccHas = true
		}
		if kv == "ANTHROPIC_API_KEY=k" {
			keyed = true
		}
	}
	if !ccHas {
		t.Error("CC 该继承进程环境里的代理")
	}
	if !keyed {
		t.Error("API Key 该来自凭证")
	}
}

// "Take over an external directory" is the only place in the plugin where a workflow input decides
// where CC operates — which expands the blast radius from the workspace to the whole machine. The gate
// must actually hold.
func TestExternalTreeGate(t *testing.T) {
	dir := t.TempDir()

	cfg = deployEnv{}
	t.Cleanup(func() { cfg = loadEnv() })
	if _, err := externalTree(dir); err == nil {
		t.Fatal("这台机器没开外部目录时必须拒绝——默认值不能是放行")
	}

	cfg.AllowExt = true
	got, err := externalTree(dir)
	if err != nil {
		t.Fatalf("开了闸且目录存在该放行: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("该回绝对路径，got %s", got)
	}
	if _, err := externalTree(filepath.Join(dir, "不存在")); err == nil {
		t.Error("目录不存在该拒绝，而不是让 CC 在一个空路径上跑")
	}
	// A file is not a directory: CC's cwd must be a directory, letting this through would produce an
	// obscure exec error
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := externalTree(f); err == nil {
		t.Error("路径是文件该拒绝")
	}
}

// The API key is optional: leave it empty to use CC's own login state on the machine (common with
// subscription plans). **An empty value must never be injected into the environment** — a bare
// ANTHROPIC_API_KEY= would shadow the login state, blocking that legitimate path, with a confusing error
// on top.
func TestAPIKeyOptional(t *testing.T) {
	var injected, present int
	for _, kv := range ccEnv(Cred{}) {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			injected++
		}
	}
	if injected != 0 {
		t.Error("凭证没给 key 时不该注入这个变量（会盖掉机器登录态）")
	}
	for _, kv := range ccEnv(Cred{APIKey: "sk-ant-x"}) {
		if kv == "ANTHROPIC_API_KEY=sk-ant-x" {
			present++
		}
	}
	if present != 1 {
		t.Error("给了 key 就该注入")
	}
	// whitespace-only also counts as not given
	for _, kv := range ccEnv(Cred{APIKey: "   "}) {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Error("全空白该当成没给")
		}
	}
}

// Two safeguards for cleanup: neither naming a branch nor giving an idle-days threshold means "delete
// everything", which must be rejected; worktrees with uncommitted changes are protected by default.
func TestCleanupGuards(t *testing.T) {
	if _, err := opCleanup(nil, &CleanupIn{}); err == nil {
		t.Fatal("既没分支又没闲置天数该被拒绝——那等于删光所有工作树")
	}
	if _, err := opCleanup(nil, &CleanupIn{Project: "x/y"}); err == nil {
		t.Fatal("只给项目同样是删光那个项目的全部，也该拒绝")
	}
	// giving a branch is allowed through (workspace is empty, removes 0, no error)
	out, err := opCleanup(nil, &CleanupIn{Project: "x/y", Branch: "cc/1"})
	if err != nil || out.Removed != 0 {
		t.Fatalf("点名分支该放行: %+v err=%v", out, err)
	}
}

// When result is empty, fall back to the last assistant text block (reported by a user on 2026-08-25).
//
// Observed: an MR review with ok=true, turns=25, and the full 4KB review body sitting right there in the
// log, yet **conclusion was empty**. Because Claude Code's last turn was a tool call (TodoWrite) rather
// than text, the result field ended up with no content. Downstream pasted conclusion as the MR comment
// → the comment was blank, while the UI only showed "run succeeded", leaving the person with no idea
// where the conclusion went.
func TestConclusionFallsBackToLastText(t *testing.T) {
	var res ccResult
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"这是评审正文"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TodoWrite"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"","num_turns":25}`,
	}
	for _, l := range lines {
		applyLine(l, &res, nil, nil)
	}
	if res.Conclusion != "这是评审正文" {
		t.Errorf("result 为空时该退回最后一段正文，实得 %q", res.Conclusion)
	}
	if !res.OK {
		t.Error("这次运行本身是成功的，不该因为兜底而改变成败判定")
	}
}

// When result is present, trust it — the fallback must never override the real conclusion.
func TestConclusionPrefersResult(t *testing.T) {
	var res ccResult
	for _, l := range []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"中间的碎碎念"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"正式结论"}`,
	} {
		applyLine(l, &res, nil, nil)
	}
	if res.Conclusion != "正式结论" {
		t.Errorf("有 result 就该用 result，实得 %q", res.Conclusion)
	}
}
