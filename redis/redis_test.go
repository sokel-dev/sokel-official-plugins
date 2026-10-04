package main

// miniredis spins up an in-process Redis to exercise every operation path — integration testing against
// a real instance goes through operation:test.
//
// What's worth watching isn't "did the command get sent", but a handful of places that **can quietly
// mislead**: a miss not being an error, the "how many to take" → index conversion, TTL's -1/-2
// sentinels, an nx that didn't win, and credential parsing (address gets a default port / db number /
// TLS value).

import (
	"context"
	"io"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

// newCtx spins up a miniredis and returns a ctx pointing to it. Each test case gets its own, so they
// don't interfere with each other.
func newCtx(t *testing.T) *fakeCtx {
	t.Helper()
	mr := miniredis.RunT(t)
	poolMu.Lock()
	pool = map[string]*redisClient{}
	poolMu.Unlock()
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"addr": mr.Addr()}}
}

// A get miss is **not an error**: on the canvas it's a normal branch (recompute when the cache misses).
// Reporting it as an error would fail the whole run, and this is the easiest place to get wrong and the
// hardest to debug.
func TestGetMissIsNotError(t *testing.T) {
	ctx := newCtx(t)
	out, err := opGet(ctx, &GetIn{Key: "nope"})
	if err != nil {
		t.Fatalf("未命中不该报错: %v", err)
	}
	if out.Exists || out.Value != "" {
		t.Fatalf("未命中该回 exists=false 空值，got %+v", out)
	}
	if _, err := opSet(ctx, &SetIn{Key: "k", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	out, err = opGet(ctx, &GetIn{Key: "k"})
	if err != nil || !out.Exists || out.Value != "v" {
		t.Fatalf("命中该回值: %+v err=%v", out, err)
	}
}

// nx is an idempotency gate: winning the first time gives ok=true, not winning the second time gives
// ok=false, and that **is not an error**.
func TestSetNXIsGateNotError(t *testing.T) {
	ctx := newCtx(t)
	first, err := opSet(ctx, &SetIn{Key: "lock", Value: "1", Mode: "nx"})
	if err != nil || !first.OK {
		t.Fatalf("首次该抢到: %+v err=%v", first, err)
	}
	second, err := opSet(ctx, &SetIn{Key: "lock", Value: "2", Mode: "nx"})
	if err != nil {
		t.Fatalf("没抢到不该报错: %v", err)
	}
	if second.OK {
		t.Fatal("键已存在，nx 不该写入")
	}
	got, _ := opGet(ctx, &GetIn{Key: "lock"})
	if got.Value != "1" {
		t.Fatalf("值不该被覆盖，got %q", got.Value)
	}
}

// TTL's -1/-2 are Redis sentinel values, and must be passed through as-is and translated into an
// easy-to-check state.
func TestTTLSentinels(t *testing.T) {
	ctx := newCtx(t)
	if _, err := opSet(ctx, &SetIn{Key: "forever", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := opSet(ctx, &SetIn{Key: "temp", Value: "v", TTLSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ key, state string }{
		{"forever", "persistent"}, {"temp", "alive"}, {"ghost", "missing"},
	} {
		out, err := opTTL(ctx, &TTLIn{Key: c.key})
		if err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if out.State != c.state {
			t.Errorf("%s 状态该是 %s，got %s (seconds=%d)", c.key, c.state, out.State, out.Seconds)
		}
	}
}

// Giving expire a non-positive number makes Redis **delete the key immediately** — the plugin must block
// this upfront, otherwise a "renewal" written as 0 ends up deleting data.
func TestExpireRejectsNonPositive(t *testing.T) {
	ctx := newCtx(t)
	if _, err := opSet(ctx, &SetIn{Key: "k", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := opExpire(ctx, &ExpireIn{Key: "k", Seconds: 0}); err == nil {
		t.Fatal("0 秒该被拒（Redis 收到会删键）")
	}
	if got, _ := opGet(ctx, &GetIn{Key: "k"}); !got.Exists {
		t.Fatal("键不该被删掉")
	}
}

// "How many to take" deliberately avoids Redis's native "end index": an empty numeric field arrives as
// 0, and stop=0 in Redis means "just the first one" — if empty can't be told apart from 0, it would
// quietly return only one item.
func TestListRangeCountSemantics(t *testing.T) {
	ctx := newCtx(t)
	if _, err := opListPush(ctx, &ListPushIn{Key: "q", Values: []string{"a", "b", "c", "d", "e"}}); err != nil {
		t.Fatal(err)
	}
	all, err := opListRange(ctx, &ListRangeIn{Key: "q"})
	if err != nil || all.Count != 5 {
		t.Fatalf("留空该回全部 5 条，got %+v err=%v", all, err)
	}
	two, _ := opListRange(ctx, &ListRangeIn{Key: "q", Count: 2})
	if two.Count != 2 || two.Values[0] != "a" {
		t.Fatalf("取 2 条该是 a,b，got %+v", two.Values)
	}
	tail, _ := opListRange(ctx, &ListRangeIn{Key: "q", Start: -2, Count: 2})
	if len(tail.Values) != 2 || tail.Values[0] != "d" {
		t.Fatalf("倒数两条该是 d,e，got %+v", tail.Values)
	}
}

// An empty queue returns 0 items rather than an error; don't get the FIFO direction backwards (push
// right, pop left).
func TestListPopEmptyAndFIFO(t *testing.T) {
	ctx := newCtx(t)
	empty, err := opListPop(ctx, &ListPopIn{Key: "q"})
	if err != nil {
		t.Fatalf("空队列不该报错: %v", err)
	}
	if empty.Count != 0 || len(empty.Values) != 0 {
		t.Fatalf("空队列该回 0 条，got %+v", empty)
	}
	if _, err := opListPush(ctx, &ListPushIn{Key: "q", Values: []string{"first", "second"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := opListPop(ctx, &ListPopIn{Key: "q"})
	if len(got.Values) != 1 || got.Values[0] != "first" {
		t.Fatalf("右入左出该先弹 first，got %+v", got.Values)
	}
}

// Deduplication relies on added: adding the same member a second time must return 0, otherwise "has this
// been seen before" would always say no.
func TestSetAddDedupSignal(t *testing.T) {
	ctx := newCtx(t)
	first, err := opSetAdd(ctx, &SetAddIn{Key: "seen", Members: []string{"a", "b"}})
	if err != nil || first.Added != 2 {
		t.Fatalf("首次该新增 2 个: %+v err=%v", first, err)
	}
	again, _ := opSetAdd(ctx, &SetAddIn{Key: "seen", Members: []string{"a"}})
	if again.Added != 0 {
		t.Fatalf("重复成员该回 added=0，got %d", again.Added)
	}
}

// Leaderboard Top N: descending score + how many to take only work correctly together.
func TestZsetTopN(t *testing.T) {
	ctx := newCtx(t)
	for _, it := range []struct {
		m string
		s float64
	}{{"low", 1}, {"high", 100}, {"mid", 50}} {
		if _, err := opZsetAdd(ctx, &ZsetAddIn{Key: "rank", Member: it.m, Score: it.s}); err != nil {
			t.Fatal(err)
		}
	}
	top, err := opZsetRange(ctx, &ZsetRangeIn{Key: "rank", Count: 2, Desc: true})
	if err != nil {
		t.Fatal(err)
	}
	if top.Count != 2 || top.Items[0].Member != "high" || top.Items[0].Score != 100 {
		t.Fatalf("Top2 该是 high(100), mid(50)，got %+v", top.Items)
	}
}

// Hash field values are always written as strings: a number shouldn't come out as 1e+06, a bool
// shouldn't get lost.
func TestHashValuesStringified(t *testing.T) {
	ctx := newCtx(t)
	if _, err := opHashSet(ctx, &HashSetIn{Key: "h", Fields: map[string]any{
		"count": float64(1000000), "ratio": 0.5, "done": true, "name": "x",
	}}); err != nil {
		t.Fatal(err)
	}
	out, err := opHashGetAll(ctx, &HashGetAllIn{Key: "h"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"count": "1000000", "ratio": "0.5", "done": "true", "name": "x"}
	for k, v := range want {
		if got := out.Fields[k]; got != v {
			t.Errorf("字段 %s 该是 %q，got %v", k, v, got)
		}
	}
	if out.Count != 4 {
		t.Errorf("字段数该是 4，got %d", out.Count)
	}
}

// Scanning keys pages incrementally via cursor, and done is decided by whether the cursor comes back to
// 0 — **an empty round doesn't mean the scan is finished**.
func TestScanCursor(t *testing.T) {
	ctx := newCtx(t)
	for i := 0; i < 30; i++ {
		if _, err := opSet(ctx, &SetIn{Key: "job:" + string(rune('a'+i%26)) + string(rune('0'+i/26)), Value: "v"}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor, rounds := 0, 0
	for {
		out, err := opScan(ctx, &ScanIn{Pattern: "job:*", Cursor: cursor, Count: 7})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range out.Keys {
			seen[k] = true
		}
		cursor = out.Cursor
		if rounds++; out.Done {
			break
		}
		if rounds > 50 {
			t.Fatal("游标没收敛")
		}
	}
	if len(seen) != 30 {
		t.Fatalf("该扫到 30 个键，got %d", len(seen))
	}
}

// Stream write + message id shape; trimming only happens when max_len is given.
func TestStreamAdd(t *testing.T) {
	ctx := newCtx(t)
	out, err := opStreamAdd(ctx, &StreamAddIn{Stream: "s", Fields: map[string]any{"type": "order"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || !contains(out.ID, "-") {
		t.Fatalf("消息 id 该是 <毫秒>-<序号>，got %q", out.ID)
	}
}

func contains(s, sub string) bool { return len(s) > 0 && len(sub) > 0 && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// The generic command's return value must be JSON-friendly: []byte becomes a string, nested arrays are
// recursed into.
func TestCallNormalizesReply(t *testing.T) {
	ctx := newCtx(t)
	if _, err := opCall(ctx, &CallIn{Command: "SET", Args: []string{"k", "v"}}); err != nil {
		t.Fatal(err)
	}
	out, err := opCall(ctx, &CallIn{Command: "GET", Args: []string{"k"}})
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := out.Result.(string); !ok || s != "v" {
		t.Fatalf("该回字符串 v，got %#v", out.Result)
	}
	// A missed GET returns an empty result rather than an error (the same convention as opGet)
	miss, err := opCall(ctx, &CallIn{Command: "GET", Args: []string{"ghost"}})
	if err != nil || miss.Result != nil {
		t.Fatalf("未命中该回空: %#v err=%v", miss.Result, err)
	}
}

// Health check: being unable to connect returns ok=false + a message, **not** an error (the platform
// displays by ok; throwing an error would become an unexplained red X).
func TestHealthCheckReportsInsteadOfErroring(t *testing.T) {
	ctx := newCtx(t)
	ok, err := opHealthCheck(ctx, &HealthCheckIn{})
	if err != nil || !ok.OK {
		t.Fatalf("本地实例该体检通过: %+v err=%v", ok, err)
	}
	if ok.LatencyMs < 0 {
		t.Error("耗时该有值")
	}
	dead := &fakeCtx{Context: context.Background(), cred: map[string]string{"addr": "127.0.0.1:1"}}
	bad, err := opHealthCheck(dead, &HealthCheckIn{})
	if err != nil {
		t.Fatalf("连不上该回 ok=false 而不是报错: %v", err)
	}
	if bad.OK || bad.Message == "" {
		t.Fatalf("该给出不可用的说明: %+v", bad)
	}
}

// INFO parsing. The fixture is **actual output from a real instance (Redis 8.4)** — miniredis's INFO is
// incomplete, and using it as the fixture would leave this branch permanently green while production
// health checks need exactly these lines.
func TestInfoFieldParsesRealOutput(t *testing.T) {
	const real = "# Server\r\n" +
		"redis_version:8.4.0\r\n" +
		"redis_git_sha1:00000000\r\n" +
		"redis_mode:standalone\r\n" +
		"os:Linux 6.19.13-orbstack aarch64\r\n" +
		"# Memory\r\n" +
		"used_memory:394362368\r\n" +
		"used_memory_human:376.09M\r\n"
	for _, c := range []struct{ key, want string }{
		{"redis_version", "8.4.0"},
		{"redis_mode", "standalone"},
		{"used_memory_human", "376.09M"},
		// don't let two lines with a shared prefix get crossed (used_memory vs used_memory_human)
		{"used_memory", "394362368"},
		{"没有这行", ""},
	} {
		if got := infoField(real, c.key); got != c.want {
			t.Errorf("%s 该解析出 %q，got %q", c.key, c.want, got)
		}
	}
}

// Credential parsing: address gets a default port, db number, the three TLS values, and invalid values
// must be reported clearly.
func TestOptionsOf(t *testing.T) {
	cases := []struct {
		name    string
		cred    Cred
		addr    string
		db      int
		tls     bool
		wantErr bool
	}{
		{name: "补默认端口", cred: Cred{Addr: "redis.internal"}, addr: "redis.internal:6379"},
		{name: "带 scheme 也认", cred: Cred{Addr: "redis://cache:6380"}, addr: "cache:6380"},
		{name: "库号", cred: Cred{Addr: "h:1", DB: "3"}, addr: "h:1", db: 3},
		{name: "TLS on", cred: Cred{Addr: "h:1", TLS: "on"}, addr: "h:1", tls: true},
		{name: "TLS 跳过校验", cred: Cred{Addr: "h:1", TLS: "on_insecure"}, addr: "h:1", tls: true},
		{name: "地址空", cred: Cred{}, wantErr: true},
		{name: "库号非法", cred: Cred{Addr: "h:1", DB: "abc"}, wantErr: true},
		{name: "TLS 取值不认识", cred: Cred{Addr: "h:1", TLS: "yes"}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opt, err := optionsOf(c.cred)
			if c.wantErr {
				if err == nil {
					t.Fatal("该报错")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if opt.Addr != c.addr || opt.DB != c.db || (opt.TLSConfig != nil) != c.tls {
				t.Errorf("got addr=%s db=%d tls=%v，want addr=%s db=%d tls=%v",
					opt.Addr, opt.DB, opt.TLSConfig != nil, c.addr, c.db, c.tls)
			}
		})
	}
}

// The same credential reuses the same client (reconnecting on every command would make the handshake
// cost more than the command itself under heavy write load); different credentials must never collide
// (the fingerprint has to factor in address/db/password).
func TestClientPooling(t *testing.T) {
	poolMu.Lock()
	pool = map[string]*redisClient{}
	poolMu.Unlock()
	a1, err := clientOf(Cred{Addr: "h:6379"})
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := clientOf(Cred{Addr: "h:6379"})
	if a1 != a2 {
		t.Error("同一份凭证该复用同一个 client")
	}
	b, _ := clientOf(Cred{Addr: "h:6379", DB: "1"})
	if a1 == b {
		t.Error("库号不同必须是两个 client")
	}
	c, _ := clientOf(Cred{Addr: "h:6379", Password: "p"})
	if a1 == c {
		t.Error("密码不同必须是两个 client")
	}
}
