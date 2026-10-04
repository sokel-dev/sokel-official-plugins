package main

// 全部操作实现。
//
// 一条贯穿始终的约定：**「没有」不是错误**。get 未命中、list_pop 队列空、
// nx 没抢到——都回一个说明状态的出参（exists/count/ok），让画布用分支处理。
// 真正的错误只留给「连不上 / 命令用错 / 类型不对」。

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sokel-dev/sokel-official-plugins/redis/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// —— 字符串 / 通用键 ——

func opGet(ctx plugin.Ctx, in *GetIn) (*GetOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := cli.Get(ctx, in.Key).Result()
	if errors.Is(err, redis.Nil) {
		return &GetOut{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("取值失败: %w", err)
	}
	return &GetOut{Value: v, Exists: true}, nil
}

func opSet(ctx plugin.Ctx, in *SetIn) (*SetOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	ttl := time.Duration(in.TTLSeconds) * time.Second
	switch strings.ToLower(strings.TrimSpace(in.Mode)) {
	case "", "overwrite":
		if err := cli.Set(ctx, in.Key, in.Value, ttl).Err(); err != nil {
			return nil, fmt.Errorf("写入失败: %w", err)
		}
		return &SetOut{OK: true}, nil
	case "nx", "xx":
		mode := strings.ToUpper(strings.TrimSpace(in.Mode))
		err := cli.SetArgs(ctx, in.Key, in.Value, redis.SetArgs{Mode: mode, TTL: ttl}).Err()
		if errors.Is(err, redis.Nil) {
			return &SetOut{}, nil // 条件没命中：不是错误
		}
		if err != nil {
			return nil, fmt.Errorf("写入失败: %w", err)
		}
		return &SetOut{OK: true}, nil
	default:
		return nil, fmt.Errorf("写入条件 %q 不认识（overwrite / nx / xx）", in.Mode)
	}
}

func opDel(ctx plugin.Ctx, in *DelIn) (*DelOut, error) {
	if len(in.Keys) == 0 {
		return nil, fmt.Errorf("没给键——至少给一个")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cli.Del(ctx, in.Keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("删除失败: %w", err)
	}
	return &DelOut{Deleted: int(n)}, nil
}

func opExists(ctx plugin.Ctx, in *ExistsIn) (*ExistsOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cli.Exists(ctx, in.Key).Result()
	if err != nil {
		return nil, fmt.Errorf("判断失败: %w", err)
	}
	return &ExistsOut{Exists: n > 0}, nil
}

func opExpire(ctx plugin.Ctx, in *ExpireIn) (*ExpireOut, error) {
	if in.Seconds <= 0 {
		// EXPIRE 给非正数会**立刻删键**——把它挡在这儿，要删就明说用 del。
		return nil, fmt.Errorf("过期秒数要大于 0（给 0 或负数 Redis 会当场删键，真要删请用「删键」操作）")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	ok, err := cli.Expire(ctx, in.Key, time.Duration(in.Seconds)*time.Second).Result()
	if err != nil {
		return nil, fmt.Errorf("设置过期失败: %w", err)
	}
	return &ExpireOut{OK: ok}, nil
}

func opTTL(ctx plugin.Ctx, in *TTLIn) (*TTLOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	// 直接下 TTL 拿原始秒数：go-redis 的 Duration 形态把 -1/-2 表达成负的纳秒，
	// 换算回来容易出错，这里要的就是那两个哨兵值。
	secs, err := cli.Do(ctx, "TTL", in.Key).Int64()
	if err != nil {
		return nil, fmt.Errorf("查询剩余时间失败: %w", err)
	}
	state := "alive"
	switch secs {
	case -1:
		state = "persistent"
	case -2:
		state = "missing"
	}
	return &TTLOut{Seconds: int(secs), State: state}, nil
}

func opIncr(ctx plugin.Ctx, in *IncrIn) (*IncrOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	by := int64(in.By)
	if by == 0 {
		by = 1 // 留空即 1；自增 0 没有意义，不必区分「没填」与「填了 0」
	}
	v, err := cli.IncrBy(ctx, in.Key, by).Result()
	if err != nil {
		return nil, fmt.Errorf("自增失败（键里存的不是整数？）: %w", err)
	}
	return &IncrOut{Value: int(v)}, nil
}

func opScan(ctx plugin.Ctx, in *ScanIn) (*ScanOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	count := int64(in.Count)
	if count <= 0 {
		count = 100
	}
	keys, next, err := cli.Scan(ctx, uint64(in.Cursor), strings.TrimSpace(in.Pattern), count).Result()
	if err != nil {
		return nil, fmt.Errorf("扫描失败: %w", err)
	}
	return &ScanOut{Keys: keys, Cursor: int(next), Done: next == 0}, nil
}

// —— 哈希 ——

func opHashGetAll(ctx plugin.Ctx, in *HashGetAllIn) (*HashGetAllOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	m, err := cli.HGetAll(ctx, in.Key).Result()
	if err != nil {
		return nil, fmt.Errorf("取哈希失败: %w", err)
	}
	return &HashGetAllOut{Fields: strMap(m), Count: len(m)}, nil
}

func opHashSet(ctx plugin.Ctx, in *HashSetIn) (*HashSetOut, error) {
	if len(in.Fields) == 0 {
		return nil, fmt.Errorf("没给字段——至少给一个键值对")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cli.HSet(ctx, in.Key, pairs(in.Fields)...).Result()
	if err != nil {
		return nil, fmt.Errorf("写哈希失败: %w", err)
	}
	return &HashSetOut{Added: int(n)}, nil
}

func opHashDel(ctx plugin.Ctx, in *HashDelIn) (*HashDelOut, error) {
	if len(in.Fields) == 0 {
		return nil, fmt.Errorf("没给字段名")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cli.HDel(ctx, in.Key, in.Fields...).Result()
	if err != nil {
		return nil, fmt.Errorf("删哈希字段失败: %w", err)
	}
	return &HashDelOut{Deleted: int(n)}, nil
}

// —— 列表 ——

func opListPush(ctx plugin.Ctx, in *ListPushIn) (*ListPushOut, error) {
	if len(in.Values) == 0 {
		return nil, fmt.Errorf("没给值——至少给一个")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	vals := make([]any, 0, len(in.Values))
	for _, v := range in.Values {
		vals = append(vals, v)
	}
	var n int64
	if strings.EqualFold(strings.TrimSpace(in.Side), "left") {
		n, err = cli.LPush(ctx, in.Key, vals...).Result()
	} else {
		n, err = cli.RPush(ctx, in.Key, vals...).Result()
	}
	if err != nil {
		return nil, fmt.Errorf("入列表失败: %w", err)
	}
	return &ListPushOut{Length: int(n)}, nil
}

func opListPop(ctx plugin.Ctx, in *ListPopIn) (*ListPopOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	count := in.Count
	if count <= 0 {
		count = 1
	}
	var vals []string
	if strings.EqualFold(strings.TrimSpace(in.Side), "right") {
		vals, err = cli.RPopCount(ctx, in.Key, count).Result()
	} else {
		vals, err = cli.LPopCount(ctx, in.Key, count).Result()
	}
	if errors.Is(err, redis.Nil) {
		return &ListPopOut{Values: []string{}}, nil // 队列空：正常分支
	}
	if err != nil {
		return nil, fmt.Errorf("出列表失败: %w", err)
	}
	return &ListPopOut{Values: vals, Count: len(vals)}, nil
}

func opListRange(ctx plugin.Ctx, in *ListRangeIn) (*ListRangeOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	start := int64(in.Start)
	stop := int64(-1) // count 留空 = 全部
	if in.Count > 0 {
		stop = start + int64(in.Count) - 1
		if stop < 0 {
			stop = -1 // 负数起点取到尾部：如 start=-5 count=5
		}
	}
	vals, err := cli.LRange(ctx, in.Key, start, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("读列表失败: %w", err)
	}
	return &ListRangeOut{Values: vals, Count: len(vals)}, nil
}

// —— 集合 ——

func opSetAdd(ctx plugin.Ctx, in *SetAddIn) (*SetAddOut, error) {
	if len(in.Members) == 0 {
		return nil, fmt.Errorf("没给成员")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	members := make([]any, 0, len(in.Members))
	for _, m := range in.Members {
		members = append(members, m)
	}
	n, err := cli.SAdd(ctx, in.Key, members...).Result()
	if err != nil {
		return nil, fmt.Errorf("集合添加失败: %w", err)
	}
	return &SetAddOut{Added: int(n)}, nil
}

func opSetMembers(ctx plugin.Ctx, in *SetMembersIn) (*SetMembersOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	ms, err := cli.SMembers(ctx, in.Key).Result()
	if err != nil {
		return nil, fmt.Errorf("取集合失败: %w", err)
	}
	return &SetMembersOut{Members: ms, Count: len(ms)}, nil
}

func opSetRemove(ctx plugin.Ctx, in *SetRemoveIn) (*SetRemoveOut, error) {
	if len(in.Members) == 0 {
		return nil, fmt.Errorf("没给成员")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	members := make([]any, 0, len(in.Members))
	for _, m := range in.Members {
		members = append(members, m)
	}
	n, err := cli.SRem(ctx, in.Key, members...).Result()
	if err != nil {
		return nil, fmt.Errorf("集合移除失败: %w", err)
	}
	return &SetRemoveOut{Removed: int(n)}, nil
}

// —— 有序集合 ——

func opZsetAdd(ctx plugin.Ctx, in *ZsetAddIn) (*ZsetAddOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cli.ZAdd(ctx, in.Key, redis.Z{Score: in.Score, Member: in.Member}).Result()
	if err != nil {
		return nil, fmt.Errorf("写入有序集合失败: %w", err)
	}
	return &ZsetAddOut{Added: int(n)}, nil
}

func opZsetRange(ctx plugin.Ctx, in *ZsetRangeIn) (*ZsetRangeOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	start := int64(in.Start)
	stop := int64(-1)
	if in.Count > 0 {
		stop = start + int64(in.Count) - 1
		if stop < 0 {
			stop = -1
		}
	}
	var zs []redis.Z
	if in.Desc {
		zs, err = cli.ZRevRangeWithScores(ctx, in.Key, start, stop).Result()
	} else {
		zs, err = cli.ZRangeWithScores(ctx, in.Key, start, stop).Result()
	}
	if err != nil {
		return nil, fmt.Errorf("读有序集合失败: %w", err)
	}
	out := &ZsetRangeOut{Count: len(zs)}
	for _, z := range zs {
		out.Items = append(out.Items, schema.ZItem{Member: asString(z.Member), Score: z.Score})
	}
	return out, nil
}

// —— 消息 ——

func opPublish(ctx plugin.Ctx, in *PublishIn) (*PublishOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := cli.Publish(ctx, in.Channel, in.Message).Result()
	if err != nil {
		return nil, fmt.Errorf("发布失败: %w", err)
	}
	return &PublishOut{Receivers: int(n)}, nil
}

func opStreamAdd(ctx plugin.Ctx, in *StreamAddIn) (*StreamAddOut, error) {
	if len(in.Fields) == 0 {
		return nil, fmt.Errorf("没给字段——Stream 消息至少要有一个键值对")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	args := &redis.XAddArgs{Stream: in.Stream, Values: pairs(in.Fields)}
	if in.MaxLen > 0 {
		args.MaxLen = int64(in.MaxLen)
		args.Approx = true // ~ 裁剪：按节点边界裁，省 CPU，条数是近似值
	}
	id, err := cli.XAdd(ctx, args).Result()
	if err != nil {
		return nil, fmt.Errorf("写入 Stream 失败: %w", err)
	}
	return &StreamAddOut{ID: id}, nil
}

// —— 保底 ——

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	cmd := strings.TrimSpace(in.Command)
	if cmd == "" {
		return nil, fmt.Errorf("没给命令名")
	}
	cli, err := clientFor(ctx)
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, len(in.Args)+1)
	args = append(args, cmd)
	for _, a := range in.Args {
		args = append(args, a)
	}
	v, err := cli.Do(ctx, args...).Result()
	if errors.Is(err, redis.Nil) {
		return &CallOut{}, nil // 空结果（如 GET 未命中）不是错误
	}
	if err != nil {
		return nil, fmt.Errorf("命令 %s 执行失败: %w", strings.ToUpper(cmd), err)
	}
	return &CallOut{Result: normalize(v)}, nil
}

// normalize Redis 应答 → JSON 友好形态（[]byte → string，嵌套数组递归）。
func normalize(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, e := range x {
			out[asString(k)] = normalize(e)
		}
		return out
	default:
		return v
	}
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cli, err := clientFor(ctx)
	if err != nil {
		// 凭证本身就配错了（地址空、库号非法）——体检的答案是「不可用」，不是抛错。
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	start := time.Now()
	if err := cli.Ping(ctx).Err(); err != nil {
		return &HealthCheckOut{Message: fmt.Sprintf("连不上：%v（检查地址/密码/网络放行）", err)}, nil
	}
	out := &HealthCheckOut{OK: true, LatencyMs: int(time.Since(start).Milliseconds())}
	// 不带 section 取默认全量：`INFO server memory` 这种多 section 形态是 Redis 7.0
	// 才支持的，6.x 上会直接报错——体检要在老实例上也能用。
	if info, err := cli.Info(ctx).Result(); err == nil {
		out.Version = infoField(info, "redis_version")
		out.Mode = infoField(info, "redis_mode")
		out.UsedMemory = infoField(info, "used_memory_human")
	}
	if n, err := cli.DBSize(ctx).Result(); err == nil {
		out.Keys = int(n)
	}
	out.Message = fmt.Sprintf("连接正常（%s，%s 模式，当前库 %d 个键）",
		orDash(out.Version), orDash(out.Mode), out.Keys)
	return out, nil
}

// infoField 从 INFO 文本里取一行的值（形如 redis_version:7.2.4）。
func infoField(info, key string) string {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if name, val, ok := strings.Cut(line, ":"); ok && name == key {
			return val
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
