// Package schema declares the operation, event, and credential contract for the redis plugin.
//
// Positioning: treat Redis as **a workflow's shared memory** — counters across runs, dedup sets, status
// flags, queues and leaderboards, plus a real-time messaging channel to and from external systems
// (Pub/Sub and Stream).
//
// Four commitments that the operation design revolves around:
//
//   - **"Not found" is not an error**: a get miss returns exists=false rather than an error — on the
//     canvas that's a normal branch (recompute when the cache misses), and treating it as an exception
//     would fail the whole run.
//   - **Scanning keys only offers scan**: KEYS blocks the entire instance (it's single-threaded), and
//     running it on a production database is an incident. scan pages incrementally via a cursor; pass
//     the previous round's cursor back in, and cursor=0 means the scan is done.
//   - **Grouped by data structure rather than by command**: Redis has 240-plus commands, and putting
//     them all on the canvas would leave nobody able to pick one. Only the commonly used layer is
//     exposed here (string/hash/list/set/sorted set/Stream); everything else goes through call to issue
//     the command directly.
//   - **Writes should carry a cap wherever possible**: stream_add's max_len and set's ttl_seconds both
//     have an explicit place in the contract — an unbounded Redis write eventually fills up memory, and
//     this makes that visible at configuration time.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

func keyField() contract.FieldSpec {
	return field.String("key").Label("键").Desc("Redis 键名，如 wf:daily:count")
}

// —— String / generic keys ——

// Get retrieves a value.
type Get struct{}

func (Get) Meta() contract.Meta {
	return contract.Meta{ID: "get", Label: "取值", Desc: "读一个字符串键；键不存在不是错误（exists=false）"}
}

func (Get) Inputs() []contract.FieldSpec { return []contract.FieldSpec{keyField()} }

func (Get) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("value").Label("值").Desc("键不存在时为空串——判有没有看 exists"),
		field.Bool("exists").Label("存在"),
	}
}

// Set stores a value.
type Set struct{}

func (Set) Meta() contract.Meta {
	return contract.Meta{ID: "set", Label: "存值", Desc: "写一个字符串键，可带过期时间与写入条件"}
}

func (Set) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Text("value").Label("值"),
		field.Int("ttl_seconds").Label("过期秒数").
			Desc("留空/0 = 不过期。**能给就给**：不过期的键只增不减").Optional(),
		field.Enum("mode",
			field.Opt("overwrite", "覆盖（默认）"),
			field.Opt("nx", "仅当键不存在"),
			field.Opt("xx", "仅当键已存在"),
		).Label("写入条件").
			Desc("nx 可当分布式锁/幂等闸：抢到才回 ok=true").Optional(),
	}
}

func (Set) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("已写入").Desc("nx/xx 条件没命中时为 false（不是错误）"),
	}
}

// Del deletes keys.
type Del struct{}

func (Del) Meta() contract.Meta {
	return contract.Meta{ID: "del", Label: "删键", Desc: "删除一个或多个键，回实际删掉的个数"}
}

func (Del) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("keys").Label("键列表"),
	}
}

func (Del) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("deleted").Label("删除个数")}
}

// Exists checks whether a key exists.
type Exists struct{}

func (Exists) Meta() contract.Meta {
	return contract.Meta{ID: "exists", Label: "键是否存在", Desc: "只判存在，不取值（值很大时比 get 省）"}
}

func (Exists) Inputs() []contract.FieldSpec { return []contract.FieldSpec{keyField()} }

func (Exists) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("exists").Label("存在")}
}

// Expire sets an expiration.
type Expire struct{}

func (Expire) Meta() contract.Meta {
	return contract.Meta{ID: "expire", Label: "设过期", Desc: "给已有的键设过期秒数；键不存在回 ok=false"}
}

func (Expire) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Int("seconds").Label("过期秒数"),
	}
}

func (Expire) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("已设置")}
}

// TTL returns the remaining time to live.
type TTL struct{}

func (TTL) Meta() contract.Meta {
	return contract.Meta{ID: "ttl", Label: "剩余时间", Desc: "查键还有多久过期"}
}

func (TTL) Inputs() []contract.FieldSpec { return []contract.FieldSpec{keyField()} }

func (TTL) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("seconds").Label("剩余秒数").Desc("永久 = -1，不存在 = -2（原样透传 Redis 语义）"),
		field.String("state").Label("状态").Desc("alive 还在计时 / persistent 永久 / missing 不存在——分支判这个别判 -1"),
	}
}

// Incr increments atomically.
type Incr struct{}

func (Incr) Meta() contract.Meta {
	return contract.Meta{ID: "incr", Label: "自增计数", Desc: "原子自增；键不存在按 0 起算。计数、限流、发号都用它"}
}

func (Incr) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Int("by").Label("增量").Desc("默认 1；负数即自减").Optional(),
	}
}

func (Incr) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("value").Label("自增后的值")}
}

// Scan scans keys.
type Scan struct{}

func (Scan) Meta() contract.Meta {
	return contract.Meta{ID: "scan", Label: "扫键", Desc: "按模式增量扫键（不阻塞实例；**别用 KEYS**）"}
}

func (Scan) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("pattern").Label("匹配模式").Desc("glob 形态，如 wf:job:*；留空 = 全部").Optional(),
		field.Int("cursor").Label("游标").Desc("第一次留空/0；翻页把上一轮回的 cursor 传回来").Optional(),
		field.Int("count").Label("每轮条数").Desc("给 Redis 的提示值，默认 100。实际条数可能多于或少于它").Optional(),
	}
}

func (Scan) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("keys").Label("本轮键"),
		field.Int("cursor").Label("下一轮游标"),
		field.Bool("done").Label("扫完了").Desc("cursor 回到 0 即扫完。**本轮可能是空的但没扫完**，看这个别看条数"),
	}
}

// —— Hash ——

// HashGetAll retrieves an entire hash.
type HashGetAll struct{}

func (HashGetAll) Meta() contract.Meta {
	return contract.Meta{ID: "hash_get_all", Label: "取哈希", Desc: "读一个哈希的全部字段（对象型状态就存这儿）"}
}

func (HashGetAll) Inputs() []contract.FieldSpec { return []contract.FieldSpec{keyField()} }

func (HashGetAll) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Object("fields", "哈希的字段键值对，键名由业务定（Redis 侧值一律是字符串）").Label("字段"),
		field.Int("count").Label("字段数").Desc("0 = 哈希不存在或为空"),
	}
}

// HashSet writes hash fields.
type HashSet struct{}

func (HashSet) Meta() contract.Meta {
	return contract.Meta{ID: "hash_set", Label: "写哈希", Desc: "写入若干字段（已有字段覆盖，其余不动）"}
}

func (HashSet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Object("fields", "要写入的字段键值对，如 {\"status\":\"running\",\"host\":\"a1\"}").Label("字段"),
	}
}

func (HashSet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("added").Label("新增字段数").Desc("覆盖已有字段不计入")}
}

// HashDel deletes hash fields.
type HashDel struct{}

func (HashDel) Meta() contract.Meta {
	return contract.Meta{ID: "hash_del", Label: "删哈希字段", Desc: "删掉哈希里的若干字段"}
}

func (HashDel) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Strings("fields").Label("字段名"),
	}
}

func (HashDel) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("deleted").Label("删除字段数")}
}

// —— List (queue) ——

// ListPush pushes onto a list.
type ListPush struct{}

func (ListPush) Meta() contract.Meta {
	return contract.Meta{ID: "list_push", Label: "入列表", Desc: "把值追加进列表；当队列用时配 list_pop"}
}

func (ListPush) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Strings("values").Label("值列表"),
		field.Enum("side", field.Opt("right", "右侧（默认，先进先出配左出）"), field.Opt("left", "左侧")).
			Label("推入端").Optional(),
	}
}

func (ListPush) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("length").Label("入队后长度")}
}

// ListPop pops from a list.
type ListPop struct{}

func (ListPop) Meta() contract.Meta {
	return contract.Meta{ID: "list_pop", Label: "出列表", Desc: "弹出并删除；列表空回空数组（不是错误）"}
}

func (ListPop) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Int("count").Label("弹出个数").Desc("默认 1").Optional(),
		field.Enum("side", field.Opt("left", "左侧（默认，配右入即 FIFO）"), field.Opt("right", "右侧")).
			Label("弹出端").Optional(),
	}
}

func (ListPop) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("values").Label("弹出的值"),
		field.Int("count").Label("实际弹出个数").Desc("0 = 队列空"),
	}
}

// ListRange reads a list without removing anything.
type ListRange struct{}

func (ListRange) Meta() contract.Meta {
	return contract.Meta{ID: "list_range", Label: "看列表", Desc: "只看不删（排查/展示用）"}
}

func (ListRange) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Int("start").Label("起始下标").Desc("默认 0（第一条）；负数从尾部算，如 -5 = 倒数第五条").Optional(),
		// Deliberately uses "how many to take" rather than Redis's native "end index": an empty numeric
		// field arrives as 0, and stop=0 in Redis means "just the first one" — if empty can't be told
		// apart from 0, it would quietly return only one item. count=0 has no ambiguity (taking 0 items
		// is meaningless), so treating it as "unlimited" is safe.
		field.Int("count").Label("取几条").Desc("默认 0 = 全部").Optional(),
	}
}

func (ListRange) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("values").Label("值"),
		field.Int("count").Label("条数"),
	}
}

// —— Set ——

// SetAdd adds to a set.
type SetAdd struct{}

func (SetAdd) Meta() contract.Meta {
	return contract.Meta{ID: "set_add", Label: "集合添加",
		Desc: "加入集合并回新增个数——**去重判重就用它**：added=0 说明这条见过了"}
}

func (SetAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Strings("members").Label("成员"),
	}
}

func (SetAdd) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("added").Label("新增个数").Desc("已存在的成员不计入")}
}

// SetMembers lists a set's members.
type SetMembers struct{}

func (SetMembers) Meta() contract.Meta {
	return contract.Meta{ID: "set_members", Label: "集合成员", Desc: "取集合全部成员（大集合先想想量）"}
}

func (SetMembers) Inputs() []contract.FieldSpec { return []contract.FieldSpec{keyField()} }

func (SetMembers) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("members").Label("成员"),
		field.Int("count").Label("个数"),
	}
}

// SetRemove removes from a set.
type SetRemove struct{}

func (SetRemove) Meta() contract.Meta {
	return contract.Meta{ID: "set_remove", Label: "集合移除", Desc: "从集合里移除若干成员"}
}

func (SetRemove) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Strings("members").Label("成员"),
	}
}

func (SetRemove) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("removed").Label("移除个数")}
}

// —— Sorted set ——

// ZItem is one sorted-set member.
type ZItem struct {
	Member string  `sokel:"member" label:"成员"`
	Score  float64 `sokel:"score" label:"分数"`
}

// ZsetAdd adds to a sorted set.
type ZsetAdd struct{}

func (ZsetAdd) Meta() contract.Meta {
	return contract.Meta{ID: "zset_add", Label: "排行榜写入",
		Desc: "按分数写入有序集合（排行榜、优先队列、按时间排的窗口）"}
}

func (ZsetAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.String("member").Label("成员"),
		field.Number("score").Label("分数").Desc("排序依据；时间窗口场景常用时间戳"),
	}
}

func (ZsetAdd) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("added").Label("新增个数").Desc("成员已存在则只更新分数，added=0"),
	}
}

// ZsetRange reads a range from a sorted set.
type ZsetRange struct{}

func (ZsetRange) Meta() contract.Meta {
	return contract.Meta{ID: "zset_range", Label: "排行榜取段", Desc: "按名次取一段（含分数）"}
}

func (ZsetRange) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		keyField(),
		field.Int("start").Label("起始名次").Desc("默认 0（第一名）").Optional(),
		field.Int("count").Label("取几名").Desc("默认 0 = 全部。取前十就填 10（与 list_range 同一约定）").Optional(),
		field.Bool("desc").Label("分数从高到低").Desc("默认从低到高；排行榜取 Top N 要打开").Optional(),
	}
}

func (ZsetRange) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []ZItem{}).Label("成员与分数"),
		field.Int("count").Label("条数"),
	}
}

// —— Messaging ——

// Publish publishes a message.
type Publish struct{}

func (Publish) Meta() contract.Meta {
	return contract.Meta{ID: "publish", Label: "发布消息",
		Desc: "往频道发一条 Pub/Sub 消息。**没订阅者就没人收**（不留存），要留存用 stream_add"}
}

func (Publish) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("channel").Label("频道"),
		field.Text("message").Label("消息内容"),
	}
}

func (Publish) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("receivers").Label("收到的订阅者数")}
}

// StreamAdd appends to a Stream.
type StreamAdd struct{}

func (StreamAdd) Meta() contract.Meta {
	return contract.Meta{ID: "stream_add", Label: "写入 Stream",
		Desc: "追加一条 Stream 消息（留存、可回溯，配事件源消费）"}
}

func (StreamAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("stream").Label("Stream 名"),
		field.Object("fields", "消息字段键值对，如 {\"type\":\"order\",\"id\":\"1001\"}").Label("字段"),
		field.Int("max_len").Label("保留条数上限").
			Desc("超出丢最老的（近似裁剪，性能更好）。留空 = 不裁剪——**Stream 会一直涨**").Optional(),
	}
}

func (StreamAdd) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("id").Label("消息 ID").Desc("Redis 生成的 <毫秒>-<序号>")}
}

// —— Fallback ——

// Call issues a generic command.
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "通用命令",
		Desc: "直接下任意 Redis 命令（上面没覆盖的：BITCOUNT/GEO/PFADD/EVAL…）"}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("command").Label("命令").Desc("命令名，如 SETRANGE、PFADD、OBJECT"),
		field.Strings("args").Label("参数").Desc("按命令文档的顺序逐个给（含 key）").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Any("result", "Redis 应答，形状随命令而变（字符串/整数/数组/嵌套数组）").Label("返回"),
	}
}

// HealthCheck is the platform-mandated credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 20,
		Desc: "PING 一下并报实例版本、部署形态、内存与键数"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("version").Label("版本"),
		field.String("mode").Label("部署形态").Desc("standalone / sentinel / cluster"),
		field.String("used_memory").Label("已用内存"),
		field.Int("keys").Label("当前库键数"),
		field.Int("latency_ms").Label("往返耗时(ms)"),
		field.String("message").Label("说明"),
	}
}

// —— Events (event source, triggers a workflow) ——
//
// Two consumption paths; whichever the credential fills in is the one that starts (neither filled in
// means no event source, while normal operations keep working as usual):
//
//   - **Pub/Sub** (watch_channels): real-time, not retained. Messages sent while the plugin was
//     disconnected **are simply not received**, and there's no message id — event_id can only be
//     generated as "the Nth message in this process", resetting on restart. Use it as a real-time
//     signal, never for anything that can't be lost.
//   - **Stream** (watch_streams): retained, can be replayed, message ids are naturally unique, and a
//     restart won't cause a replay thanks to id-based dedup. Reading starts from "the moment it
//     connects" (no history replay), matching the same first-run convention as other plugins.
type Events struct{}

// CommonFields are the fields shared by all events: both event kinds carry key (channel name / Stream
// name).
func (Events) CommonFields() []string { return []string{"key"} }

func eventKeyField() contract.FieldSpec {
	return field.String("key").Label("来源").Desc("Pub/Sub 是频道名，Stream 是 Stream 名")
}

// MessageReceived is received for a channel message.
type MessageReceived struct{}

func (MessageReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "message_received", Label: "收到频道消息"}
}

func (MessageReceived) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventKeyField(),
		field.Text("payload").Label("消息内容").Desc("原样字符串；JSON 要在流程里自己解析"),
		field.String("pattern").Label("命中的模式").Desc("按通配订阅时才有").Optional(),
	}
}

// StreamMessage is received for a Stream message.
type StreamMessage struct{}

func (StreamMessage) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "stream_message", Label: "收到 Stream 消息"}
}

func (StreamMessage) Fields() []contract.FieldSpec {
	return []contract.FieldSpec{
		eventKeyField(),
		field.String("id").Label("消息 ID").Desc("<毫秒>-<序号>，唯一且递增"),
		field.Object("fields", "消息字段键值对，键名由写入方决定").Label("字段"),
	}
}

// —— Credential ——

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("addr").Label("地址").
			Desc("host:port，如 redis.internal:6379。**插件进程要能连到它**——" +
				"云托管实例记得放行来源 IP"),
		field.Text("username").Label("用户名").
			Desc("Redis 6+ 的 ACL 用户；用传统 requirepass 密码就留空").Optional(),
		field.Secret("password").Label("密码").Desc("没设密码就留空").Optional(),
		field.Text("db").Label("库号").Desc("默认 0。集群模式只有 0 号库").Optional(),
		field.Select("tls", "off", "on", "on_insecure").Label("TLS").
			Desc("off 明文（默认，内网常见）/ on 走 TLS / on_insecure 走 TLS 但不校验证书（自签证书用）").
			Optional(),
		field.Text("watch_channels").Label("事件盯哪些频道").
			Desc("Pub/Sub 频道，逗号分隔，支持通配（如 orders.*）。**填了才启动事件源**：" +
				"每条消息触发一次工作流。实时但不留存——插件没连着的那段时间收不到").Optional(),
		field.Text("watch_streams").Label("事件盯哪些 Stream").
			Desc("Stream 名，逗号分隔。**填了才启动事件源**：从接上的那一刻开始读新消息，" +
				"消息 id 唯一，重启不会重放").Optional(),
	}
}
