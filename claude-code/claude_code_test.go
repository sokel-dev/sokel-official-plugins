package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 行解析。夹具是**实机 Claude Code 2.1.38 的原文**（截短了噪音字段）——
// 合成语料在这里会骗人：字段名、嵌套层级、哪一行才带 session_id，都是猜不准的。
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
	// 被权限拦下的工具必须出得来：任务没做完时这是第一个要看的地方
	if len(res.DeniedTools) != 1 || res.DeniedTools[0] != "WebFetch" {
		t.Errorf("permission_denials 该解出工具名，got %+v", res.DeniedTools)
	}
	// hook 噪音行不能影响任何东西（CC 会吐一堆 hook/mcp 行）
	if len(texts) > 1 {
		t.Error("hook 行不该被当成过程文字")
	}
}

// 脏行不能把整条流判死——一行不是 JSON 就丢掉，后面的照常解析。
func TestApplyLineSurvivesGarbage(t *testing.T) {
	var res ccResult
	applyLine(`Debugger attached.`, &res, nil, nil)
	applyLine(`{"type":"result","subtype":"success","result":"ok","session_id":"s1"}`, &res, nil, nil)
	if res.Conclusion != "ok" || res.SessionID != "s1" {
		t.Fatalf("脏行之后该照常解析，got %+v", res)
	}
}

// 参数拼装。这几个开关的拼法是照本机 claude --help 核过的：
// --allowedTools 是驼峰、过滤语法用冒号、成本上限叫 --max-budget-usd。
// 文档里流传的 --bare / --max-turns 在 2.1.38 根本不存在，拼进去就是跑不起来。
func TestCCArgs(t *testing.T) {
	args := strings.Join(ccArgs(ccOptions{
		Task: "修一下", Model: "opus", Effort: "high", MaxBudgetUSD: 2.5,
		AllowedTools: []string{"Read", "Edit", "Bash(git:*)"},
		SystemPrompt: "别改 vendor/",
	}), " ")

	for _, want := range []string{
		"-p 修一下", "--output-format stream-json", "--verbose",
		"--permission-mode acceptEdits", // 没给就用这个默认，不是 bypassPermissions
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

// 令牌不能落进仓库配置：clean URL 是写进 .git/config 的那个，必须不含令牌。
// 工作树是 CC 能读的目录——令牌躺在那儿等于交出去了。
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

// 目录名：group/name 直接当路径会多挖一层目录，两个项目的工作树就会互相嵌套。
func TestSlugFlattens(t *testing.T) {
	for in, want := range map[string]string{
		"backend/server":  "backend_server",
		"cc/2026-fix":     "cc_2026-fix",
		"/leading/slash/": "leading_slash",
		"a/../../etc/pwd": "a_____etc_pwd", // 斜杠与 .. 各占一个下划线，穿不出目录
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// 代理只给 CC：git 的环境必须把代理变量剥干净，否则 clone 内网仓库会卡在代理上超时，
// 而报错长得像网络抖动，很难往代理上想。
func TestProxySplit(t *testing.T) {
	t.Setenv("http_proxy", "http://proxy:8118")
	t.Setenv("HTTPS_PROXY", "http://proxy:8118")

	for _, kv := range gitEnv() {
		k := strings.ToLower(kv[:strings.IndexByte(kv, '=')+1])
		// 只看真正的出站代理变量：GOPROXY 是 Go 模块源，剥掉它反而会让 go 命令失灵
		if k == "http_proxy=" || k == "https_proxy=" || k == "all_proxy=" {
			t.Errorf("git 环境里不该有代理: %s", kv)
		}
	}
	// CC 直接继承进程环境（它本来就读标准 http_proxy/https_proxy），
	// 不再由插件从凭证里转发——代理是「这台机器怎么出网」，是部署属性。
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

// 「接管外部目录」是插件里唯一一处让工作流入参决定 CC 在哪干活的地方——
// 等于把爆炸半径从工作区扩到整机。闸门必须真的关得住。
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
	// 文件不是目录：CC 的 cwd 只能是目录，放过去会得到一个晦涩的 exec 错误
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := externalTree(f); err == nil {
		t.Error("路径是文件该拒绝")
	}
}

// API Key 是可选的：留空就用机器上 CC 自己的登录态（订阅制常见）。
// **空值绝不能注入环境**——ANTHROPIC_API_KEY= 摆在那儿会盖掉登录态，
// 把这条合法路径堵死，而且报错很难懂。
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
	// 只有空白也算没给
	for _, kv := range ccEnv(Cred{APIKey: "   "}) {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Error("全空白该当成没给")
		}
	}
}

// 清理的两道保险：不点名分支又不给闲置天数 = 删光，必须拒绝；
// 有未提交改动的默认保护。
func TestCleanupGuards(t *testing.T) {
	if _, err := opCleanup(nil, &CleanupIn{}); err == nil {
		t.Fatal("既没分支又没闲置天数该被拒绝——那等于删光所有工作树")
	}
	if _, err := opCleanup(nil, &CleanupIn{Project: "x/y"}); err == nil {
		t.Fatal("只给项目同样是删光那个项目的全部，也该拒绝")
	}
	// 给了分支就放行（工作区是空的，删 0 个，不报错）
	out, err := opCleanup(nil, &CleanupIn{Project: "x/y", Branch: "cc/1"})
	if err != nil || out.Removed != 0 {
		t.Fatalf("点名分支该放行: %+v err=%v", out, err)
	}
}

// result 为空时用最后一段助手正文兜底（用户实报 2026-08-25）。
//
// 现场：一次 MR 评审 ok=true、turns=25、log 里 4KB 评审正文俱在，**conclusion 却是空的**。
// 因为 Claude Code 的最后一轮是工具调用（TodoWrite）而不是说话，result 字段就没有内容。
// 下游把 conclusion 贴成 MR 评论 → 评论是空的，而界面上只显示「运行成功」，
// 人完全不知道结论去哪了。
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

// 有 result 时以 result 为准——兜底不能反过来盖掉正牌结论。
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
