package main

// miniredis 起一个进程内 Redis 打穿全部操作路径——真实例联调走 operation:test。
//
// 值得盯的不是「命令有没有发出去」，而是几个**会悄悄骗人**的地方：
// 未命中不报错、取几条 → 下标的换算、TTL 的 -1/-2 哨兵、nx 没抢到、
// 以及凭证解析（地址补端口 / 库号 / TLS 取值）。

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

// newCtx 起一个 miniredis 并给出指向它的 ctx。每个用例一套，互不干扰。
func newCtx(t *testing.T) *fakeCtx {
	t.Helper()
	mr := miniredis.RunT(t)
	poolMu.Lock()
	pool = map[string]*redisClient{}
	poolMu.Unlock()
	return &fakeCtx{Context: context.Background(), cred: map[string]string{"addr": mr.Addr()}}
}

// 取值未命中**不是错误**：画布上那是一条正常分支（缓存没有就去算）。
// 报错的话整条流程会被判失败，这是最容易写错也最难查的一处。
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

// nx 是幂等闸：第一次抢到 ok=true，第二次没抢到 ok=false 且**不是错误**。
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

// TTL 的 -1/-2 是 Redis 的哨兵值，必须原样透出并翻成好判的 state。
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

// 设过期给非正数时 Redis 会**当场删键**——插件必须挡在前面，否则「续期」写成 0 就是删数据。
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

// 「取几条」是刻意不用 Redis 原生的「结束下标」：留空的数字字段到手就是 0，
// 而 stop=0 在 Redis 里意思是「只要第一条」——空与 0 分不开就会悄悄只回一条。
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

// 队列空回 0 条而不是报错；先进先出的方向别搞反（右入左出）。
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

// 去重靠 added：第二次加同一个成员必须回 0，否则「这条见过没有」就永远是没见过。
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

// 排行榜取 Top N：分数从高到低 + 取几名，两个开关一起才对。
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

// 哈希字段的值一律按字符串写入：数字别打成 1e+06，布尔别丢。
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

// 扫键靠 cursor 增量翻页，done 看游标回没回 0——**本轮为空不代表扫完**。
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

// Stream 写入 + 消息 id 形态；max_len 给了才裁剪。
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

// 通用命令的返回要 JSON 友好：[]byte 变字符串，嵌套数组递归。
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
	// 未命中的 GET 回空结果而不是错误（和 opGet 同一条约定）
	miss, err := opCall(ctx, &CallIn{Command: "GET", Args: []string{"ghost"}})
	if err != nil || miss.Result != nil {
		t.Fatalf("未命中该回空: %#v err=%v", miss.Result, err)
	}
}

// 体检：连不上回 ok=false + 说明，**不是** error（平台按 ok 展示，抛错会变成红叉没解释）。
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

// INFO 解析。夹具是**真实例（Redis 8.4）的原文**——miniredis 的 INFO 是残缺的，
// 拿它当夹具这条分支永远绿，而线上体检要的就是这几行。
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
		{"used_memory", "394362368"}, // 前缀相同的两行别串（used_memory vs used_memory_human）
		{"没有这行", ""},
	} {
		if got := infoField(real, c.key); got != c.want {
			t.Errorf("%s 该解析出 %q，got %q", c.key, c.want, got)
		}
	}
}

// 凭证解析：地址补端口、库号、TLS 三种取值、非法值要报清楚。
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

// 同一份凭证复用同一个 client（每条命令重连的话，密集写入时握手比命令还贵）；
// 不同凭证不能串（指纹要把地址/库/密码都算进去）。
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
