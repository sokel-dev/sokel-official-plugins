package main

// 事件源：Redis 的两条消费路，凭证填了哪条起哪条（都不填不起源，普通操作照用）。
//
//   - **Pub/Sub**（watch_channels）：实时、不留存、无消息 id。插件没连着的那段时间的
//     消息**收不到**——它是信号不是队列。
//   - **Stream**（watch_streams）：留存、可回溯、消息 id 唯一且递增，天生适合当队列。
//     从「$」（接上的那一刻）开始读，不回溯历史——与 feed/gitlab 首轮不灌历史同一约定。
//
// 两条路都跑在各自的 goroutine 里，ctx 取消时一起收摊。

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// pubsub 消息没有 id，事件 id 只能由本进程编号生成。
//
// **不能用内容哈希**：同一个频道连发两条一样的消息是合法的（"tick"、"reload"），
// 按内容去重会把第二条吃掉。代价是进程重启后重新编号——但 Pub/Sub 本来就不重投，
// 没有「重启后重放」这回事，所以代价是零。
var psSeq atomic.Int64

func runEvents(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	channels := splitList(cred.WatchChannels)
	streams := splitList(cred.WatchStreams)
	if len(channels) == 0 && len(streams) == 0 {
		return fmt.Errorf("凭证没填「事件盯哪些频道」或「事件盯哪些 Stream」，事件源不启动（普通操作不受影响）")
	}
	cli, err := clientOf(cred)
	if err != nil {
		return err
	}
	if err := cli.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("连不上 Redis: %w", err)
	}

	var what []string
	if len(channels) > 0 {
		what = append(what, fmt.Sprintf("%d 个频道", len(channels)))
		go consumeChannels(ctx, cli, channels)
	}
	if len(streams) > 0 {
		what = append(what, fmt.Sprintf("%d 个 Stream", len(streams)))
		go consumeStreams(ctx, cli, streams)
	}
	ctx.ReportStatus("ok", "监听 "+strings.Join(what, " + "))

	<-ctx.Done()
	return nil
}

// consumeChannels Pub/Sub 订阅。带通配的走 PSUBSCRIBE，其余走 SUBSCRIBE——
// 分开是为了让 pattern 出参只在真按通配订阅时才有值（否则每条消息都带个等于频道名的
// "模式"，看着像有通配其实没有）。
func consumeChannels(ctx plugin.SourceCtx, cli *redis.Client, channels []string) {
	var exact, patterns []string
	for _, c := range channels {
		if strings.ContainsAny(c, "*?[") {
			patterns = append(patterns, c)
		} else {
			exact = append(exact, c)
		}
	}
	if len(exact) > 0 {
		sub := cli.Subscribe(ctx, exact...)
		defer sub.Close()
		go pumpMessages(ctx, sub.Channel())
	}
	if len(patterns) > 0 {
		psub := cli.PSubscribe(ctx, patterns...)
		defer psub.Close()
		go pumpMessages(ctx, psub.Channel())
	}
	<-ctx.Done()
}

func pumpMessages(ctx plugin.SourceCtx, ch <-chan *redis.Message) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			id := fmt.Sprintf("ps:%d:%d", time.Now().UnixNano(), psSeq.Add(1))
			if err := TriggerMessageReceived(ctx, id, &MessageReceivedEvent{
				Key: msg.Channel, Payload: msg.Payload, Pattern: msg.Pattern,
			}); err != nil {
				log.Printf("[redis] 推送频道消息失败(%s): %v", msg.Channel, err)
			}
		}
	}
}

// consumeStreams XREAD 阻塞读。BLOCK 给 5 秒而不是 0（永久）：ctx 取消时最多再等一轮，
// 进程能干净退出；永久阻塞的连接要靠关连接才醒。
func consumeStreams(ctx plugin.SourceCtx, cli *redis.Client, streams []string) {
	last := make(map[string]string, len(streams))
	for _, s := range streams {
		last[s] = "$" // 只读接上之后的新消息，不回溯历史
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		args := make([]string, 0, len(streams)*2)
		args = append(args, streams...)
		for _, s := range streams {
			args = append(args, last[s])
		}
		res, err := cli.XRead(ctx, &redis.XReadArgs{Streams: args, Block: 5 * time.Second, Count: 100}).Result()
		if err == redis.Nil {
			continue // 本轮没有新消息
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			ctx.ReportStatus("error", fmt.Sprintf("读 Stream 失败：%v（5 秒后重试）", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, st := range res {
			for _, m := range st.Messages {
				last[st.Stream] = m.ID
				fields := make(map[string]any, len(m.Values))
				for k, v := range m.Values {
					fields[k] = asString(v)
				}
				// 事件 id 用 Stream 名 + 消息 id：XADD 的 id 唯一且递增，
				// 重启后即使重读也会被平台按同一个 id 去重。
				if err := TriggerStreamMessage(ctx, st.Stream+":"+m.ID, &StreamMessageEvent{
					Key: st.Stream, ID: m.ID, Fields: fields,
				}); err != nil {
					log.Printf("[redis] 推送 Stream 消息失败(%s %s): %v", st.Stream, m.ID, err)
				}
			}
		}
	}
}
