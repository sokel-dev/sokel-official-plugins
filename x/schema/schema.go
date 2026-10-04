// Package schema 声明 x 插件的操作、事件与凭证契约。
//
// 五条贯穿全篇的判断（都是别家插件踩出来的，不是设计偏好）：
//
//  1. **读的形状按平台的增量流来，不照抄 X 的分页**。X 的翻页是 next_token（一小时就失效，
//     且只在一次搜索会话内有效），而工作流是「每小时跑一次，接着上次的往下拉」。
//     所以对外给的是 since_id 游标 + next_cursor/has_more——与其它增量流插件一个形状，
//     游标存数据表。next_token 只在一次调用内部自动翻页时用。
//
//  2. **转推与回复要能一眼分掉**。搜索结果里大半是转推，`kind` 字段是插件从
//     referenced_tweets 推出来的（X 不给），没有它下游只能靠 "RT @" 猜。
//
//  3. **推串是一个操作，不是让人在画布上摆 N 个发推**。X 没有推串接口，靠逐条
//     in_reply_to 串起来；摆在画布上就是 N 个节点 + N 条连线，改一次文案要动 N 处，
//     而且中间断了没有任何东西记得断在哪。
//
//  4. **媒体上传独立成操作**。它是 INIT/APPEND/FINALIZE 三段式 + 处理轮询，塞进发推里
//     会让「发一条纯文本」也背上一套分片逻辑；拆开之后画布上是显式的一步，失败可单独重试。
//
//  5. **写操作一律回 id 与链接**。发完推拿不到链接的话，下游想发个通知都得自己拼字符串。
//
// 认证只有一条路：平台侧 OAuth 2.0 授权（provider=x，PKCE 由平台处理）。
// X 的 API 是按次计费的（2026-02 起 pay-per-use），说明书里写清每个操作的开销。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— 发布 ——

// PostCreate 发一条推文：原创、回复、引用、带图、带投票都是它。
type PostCreate struct{}

func (PostCreate) Meta() contract.Meta {
	return contract.Meta{ID: "x_post_create", Label: "发推文",
		Desc: "发原创/回复/引用推文，可带图片视频与投票", TimeoutSec: 60}
}

func (PostCreate) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").
			Desc("普通账号上限 280 字符（中日韩字算 2 个权重的规则由 X 判定）；带投票或纯媒体时可留空"),
		field.String("reply_to_id").Label("回复给（推文 id）").
			Desc("填了就是回复。想回复某条推文的话，从「搜索」等操作的 id 字段绑过来").Optional(),
		field.String("quote_id").Label("引用（推文 id）").
			Desc("引用转推。⚠️ X 把带引用的发推划进了 Enterprise 档，自助档会被拒——先确认你的档位").Optional(),
		field.Strings("media_ids").Label("媒体 id").
			Desc("来自「上传媒体」操作，最多 4 个（视频/GIF 只能 1 个）").Optional(),
		field.Strings("poll_options").Label("投票选项").
			Desc("2-4 项，每项最多 25 字符；填了就是投票推文，不能同时带媒体").Optional(),
		field.Int("poll_duration_minutes").Label("投票时长（分钟）").
			Desc("5 - 10080（7 天）").Default(1440),
		field.Enum("reply_settings",
			field.Opt("everyone", "所有人可回复"), field.Opt("following", "仅我关注的人"),
			field.Opt("mentionedUsers", "仅被提及的人"), field.Opt("subscribers", "仅订阅者"),
			field.Opt("verified", "仅认证账号")).
			Label("谁能回复").Default("everyone"),
	}
}

func (PostCreate) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("id").Label("推文 id"),
		field.String("url").Label("链接").Desc("可直接点开，也可以直接发进通知里"),
		field.String("text").Label("最终正文").Desc("X 回显的正文（链接会被替换成 t.co 短链）"),
	}
}

// PostThread 发一串推文（thread）。
//
// X 没有推串接口：这里逐条发，把上一条的 id 当作下一条的 in_reply_to。
// **中断了要能接着来**——所以出参给的是「已发出的全部 id」，而不是只给根 id。
type PostThread struct{}

func (PostThread) Meta() contract.Meta {
	return contract.Meta{ID: "x_post_thread", Label: "发推串",
		Desc: "把多段文本发成一串首尾相连的推文；第一段可带媒体", TimeoutSec: 300}
}

func (PostThread) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("texts").Label("每条正文").
			Desc("按顺序逐条发出，后一条自动回复前一条。**一条一段**，别把整篇文章塞进一项——本操作不替你断句"),
		field.String("reply_to_id").Label("接在谁后面（推文 id）").
			Desc("填了则整串挂在这条推文下；留空则第一条是原创").Optional(),
		field.Strings("media_ids").Label("首条的媒体 id").Desc("只挂在第一条上，最多 4 个").Optional(),
		field.Int("interval_ms").Label("每条之间停顿（毫秒）").
			Desc("默认 1000。X 对连续发推有速率限制，间隔太短整串会发一半断在中途").Default(1000),
	}
}

func (PostThread) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("ids").Label("各条推文 id").Desc("按发出顺序；中途失败时这里是**已经发出去的那几条**"),
		field.String("root_id").Label("首条 id"),
		field.String("root_url").Label("首条链接"),
		field.Int("count").Label("发出条数"),
	}
}

// PostDelete 删一条推文。
type PostDelete struct{}

func (PostDelete) Meta() contract.Meta {
	return contract.Meta{ID: "x_post_delete", Label: "删推文", Desc: "只能删授权账号自己发的", TimeoutSec: 30}
}

func (PostDelete) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("post_id").Label("推文 id")}
}

func (PostDelete) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("deleted").Label("已删除")}
}

// —— 媒体 ——

// MediaUpload 上传一个文件，拿到 media_id。
type MediaUpload struct{}

func (MediaUpload) Meta() contract.Meta {
	return contract.Meta{ID: "x_media_upload", Label: "上传媒体",
		Desc: "图片/GIF/视频 → media_id，供发推与私信使用。视频自动分片并等待转码", TimeoutSec: 600}
}

func (MediaUpload) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.File("file").Label("文件").
			Desc("图片 ≤5MB、GIF ≤15MB、视频 ≤512MB。视频只收 MP4(H.264/AAC)，其它格式 X 会在转码阶段拒掉"),
		field.String("alt_text").Label("替代文本").
			Desc("给读屏用户的图片描述，≤1000 字符。视频不支持").Optional(),
		field.Enum("purpose", field.Opt("post", "发推用"), field.Opt("dm", "私信用")).
			Label("用途").Desc("决定 media_category；用途填错 X 会在发布时拒绝这个 media_id").Default("post"),
	}
}

func (MediaUpload) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("media_id").Label("媒体 id").Desc("绑到「发推文」的媒体 id 上"),
		field.String("media_key").Label("媒体键"),
		field.Int("size").Label("字节数"),
		field.String("state").Label("处理状态").Desc("succeeded 才能用；视频转码由本操作等到结束"),
	}
}

// —— 互动 ——
//
// 点赞/转推/收藏/关注各拆成「做」与「取消」两个操作，不合并成一个带开关的。
// 它们同时也是 agent 的工具：一个叫「取消点赞」的工具，比一个叫「点赞（undo=true）」的
// 少一次误用；而模型选错开关是不会报错的。

func postIDInput() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("post_id").Label("推文 id")}
}

func okOutput(label string) []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label(label)}
}

// Like 点赞。
type Like struct{}

func (Like) Meta() contract.Meta {
	return contract.Meta{ID: "x_like", Label: "点赞", TimeoutSec: 30}
}
func (Like) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Like) Outputs() []contract.FieldSpec { return okOutput("已点赞") }

// Unlike 取消点赞。
type Unlike struct{}

func (Unlike) Meta() contract.Meta {
	return contract.Meta{ID: "x_unlike", Label: "取消点赞", TimeoutSec: 30}
}
func (Unlike) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Unlike) Outputs() []contract.FieldSpec { return okOutput("已取消") }

// Repost 转推。
type Repost struct{}

func (Repost) Meta() contract.Meta {
	return contract.Meta{ID: "x_repost", Label: "转推", Desc: "原样转发（要带评论就用「发推文」的引用）", TimeoutSec: 30}
}
func (Repost) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Repost) Outputs() []contract.FieldSpec { return okOutput("已转推") }

// Unrepost 取消转推。
type Unrepost struct{}

func (Unrepost) Meta() contract.Meta {
	return contract.Meta{ID: "x_unrepost", Label: "取消转推", TimeoutSec: 30}
}
func (Unrepost) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Unrepost) Outputs() []contract.FieldSpec { return okOutput("已取消") }

// Bookmark 加书签。
type Bookmark struct{}

func (Bookmark) Meta() contract.Meta {
	return contract.Meta{ID: "x_bookmark", Label: "加书签", Desc: "书签是私密的，别人看不到", TimeoutSec: 30}
}
func (Bookmark) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Bookmark) Outputs() []contract.FieldSpec { return okOutput("已收藏") }

// Unbookmark 取消书签。
type Unbookmark struct{}

func (Unbookmark) Meta() contract.Meta {
	return contract.Meta{ID: "x_unbookmark", Label: "取消书签", TimeoutSec: 30}
}
func (Unbookmark) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Unbookmark) Outputs() []contract.FieldSpec { return okOutput("已取消") }

// Follow 关注一个账号。
type Follow struct{}

func (Follow) Meta() contract.Meta {
	return contract.Meta{ID: "x_follow", Label: "关注", TimeoutSec: 30}
}

func (Follow) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("user_id").Label("用户 id").Desc("只有用户名的话，先过一次「查用户」").Optional(),
		field.String("username").Label("用户名").Desc("不带 @；填了则插件先查 id（多花一次查询）").Optional(),
	}
}

func (Follow) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("已关注"),
		field.Bool("pending").Label("等待对方通过").Desc("对方是私密账号时为真"),
	}
}

// Unfollow 取消关注。
type Unfollow struct{}

func (Unfollow) Meta() contract.Meta {
	return contract.Meta{ID: "x_unfollow", Label: "取消关注", TimeoutSec: 30}
}
func (Unfollow) Inputs() []contract.FieldSpec  { return Follow{}.Inputs() }
func (Unfollow) Outputs() []contract.FieldSpec { return okOutput("已取消关注") }

// —— 读 ——
//
// 四个读操作共用一套增量游标形状（见本文件顶部第 1 条）：
// since_id 进、next_cursor/has_more 出，游标存数据表，和其它增量流插件一模一样。

func cursorInputs(desc string) []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("cursor").Label("游标（推文 id）").
			Desc("上一批返回的 next_cursor。" + desc).Optional(),
		field.Int("max_items").Label("最多几条").
			Desc("默认 50，上限 100；自动翻页。**每条都计费**（读一条约 $0.005），别随手调大").Default(50),
	}
}

func cursorOutputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Post{}).Label("推文列表").Desc("按时间正序（老的在前），与游标推进方向一致"),
		field.String("next_cursor").Label("下一个游标").Desc("本批最新一条的 id；没有新内容时原样返回传入的游标"),
		field.Bool("has_more").Label("还有更多").Desc("为真表示这批拉满了，应当立刻再拉一次"),
		field.Int("count").Label("本批条数"),
	}
}

// Search 搜最近 7 天的推文。
type Search struct{}

func (Search) Meta() contract.Meta {
	return contract.Meta{ID: "x_search", Label: "搜索推文",
		Desc: "按查询式搜最近 7 天的公开推文（自助档只有 7 天，全量存档是 Enterprise）", TimeoutSec: 120}
}

func (Search) Inputs() []contract.FieldSpec {
	return append([]contract.FieldSpec{
		field.Text("query").Label("查询式").
			Desc("X 的搜索语法：`from:elonmusk`、`#AI -is:retweet`、`\"大模型\" lang:zh`、`url:openai.com`。" +
				"**默认全网都搜，包括转推**——不想要转推就加 -is:retweet"),
		field.Enum("sort", field.Opt("recency", "按时间"), field.Opt("relevancy", "按相关度")).
			Label("排序").Default("recency"),
	}, cursorInputs("留空则从最近的开始；有游标则只取比它更新的")...)
}

func (Search) Outputs() []contract.FieldSpec { return cursorOutputs() }

// UserTimeline 某个账号发的推文。
type UserTimeline struct{}

func (UserTimeline) Meta() contract.Meta {
	return contract.Meta{ID: "x_user_timeline", Label: "用户时间线",
		Desc: "拉某个账号自己发的推文（可含/不含转推与回复）", TimeoutSec: 120}
}

func (UserTimeline) Inputs() []contract.FieldSpec {
	return append([]contract.FieldSpec{
		field.String("user_id").Label("用户 id").Optional(),
		field.String("username").Label("用户名").Desc("不带 @；与 id 二选一（填用户名会多花一次查询）").Optional(),
		field.Bool("exclude_reposts").Label("不要转推").Default(true),
		field.Bool("exclude_replies").Label("不要回复").Default(false),
	}, cursorInputs("留空则从最近的开始")...)
}

func (UserTimeline) Outputs() []contract.FieldSpec { return cursorOutputs() }

// Mentions 提到授权账号的推文。
type Mentions struct{}

func (Mentions) Meta() contract.Meta {
	return contract.Meta{ID: "x_mentions", Label: "提及我的",
		Desc: "拉 @ 了授权账号的推文。要实时的话用本插件的事件源，不必自己定时拉", TimeoutSec: 120}
}

func (Mentions) Inputs() []contract.FieldSpec {
	return cursorInputs("留空则从最近的开始；接着上次拉就把它存进数据表")
}

func (Mentions) Outputs() []contract.FieldSpec { return cursorOutputs() }

// ListPosts 一个列表里的推文。
//
// 这是**按账号盯人的正确姿势**：把要盯的账号都加进一个列表，一次调用拉全部，
// 比逐个账号调 user_timeline 省几十倍的请求与钱。
type ListPosts struct{}

func (ListPosts) Meta() contract.Meta {
	return contract.Meta{ID: "x_list_posts", Label: "列表时间线",
		Desc: "拉一个 X 列表里所有成员的推文——盯一批账号时，比逐个拉时间线省几十倍请求", TimeoutSec: 120}
}

func (ListPosts) Inputs() []contract.FieldSpec {
	return append([]contract.FieldSpec{
		field.String("list_id").Label("列表 id").Desc("列表页地址 x.com/i/lists/<这一串>"),
	}, cursorInputs("留空则从最近的开始")...)
}

func (ListPosts) Outputs() []contract.FieldSpec { return cursorOutputs() }

// PostGet 按 id 取推文（可一次取多条）。
type PostGet struct{}

func (PostGet) Meta() contract.Meta {
	return contract.Meta{ID: "x_post_get", Label: "取推文",
		Desc: "按 id 批量取推文详情，一次最多 100 条", TimeoutSec: 60}
}

func (PostGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Strings("post_ids").Label("推文 id").Desc("一次最多 100 个"),
	}
}

func (PostGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Post{}).Label("推文列表").Desc("删掉/私密的推文不会出现在结果里，不是错误"),
		field.Int("count").Label("条数"),
	}
}

// UserGet 查账号。
type UserGet struct{}

func (UserGet) Meta() contract.Meta {
	return contract.Meta{ID: "x_user_get", Label: "查用户",
		Desc: "按用户名或 id 查账号资料；都留空则查授权账号自己", TimeoutSec: 60}
}

func (UserGet) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("username").Label("用户名").Desc("不带 @").Optional(),
		field.String("user_id").Label("用户 id").Optional(),
	}
}

func (UserGet) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Json("user", User{}).Label("账号"),
		field.Bool("found").Label("是否查到").Desc("为假表示账号不存在/已封禁，不是错误"),
	}
}

// —— 私信 ——

// DMSend 发私信。
type DMSend struct{}

func (DMSend) Meta() contract.Meta {
	return contract.Meta{ID: "x_dm_send", Label: "发私信",
		Desc: "给某个用户或某个会话发私信。对方未关注你且未开放私信时会被拒", TimeoutSec: 60}
}

func (DMSend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("text").Label("正文").Desc("与媒体至少给一个"),
		field.String("user_id").Label("收信人 id").Desc("发给某个人；与会话 id 二选一").Optional(),
		field.String("conversation_id").Label("会话 id").Desc("回复某个已有会话（私信事件里带这个字段）").Optional(),
		field.Strings("media_ids").Label("媒体 id").Desc("来自「上传媒体」且用途选了「私信用」").Optional(),
	}
}

func (DMSend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("conversation_id").Label("会话 id"),
		field.String("event_id").Label("事件 id"),
	}
}

// DMEvents 拉私信。
type DMEvents struct{}

func (DMEvents) Meta() contract.Meta {
	return contract.Meta{ID: "x_dm_events", Label: "拉私信",
		Desc: "拉授权账号的私信事件；给了会话 id 则只拉那个会话", TimeoutSec: 120}
}

func (DMEvents) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("conversation_id").Label("会话 id").Desc("留空 = 拉全部会话").Optional(),
		field.String("cursor").Label("游标（事件 id）").Desc("上一批返回的 next_cursor").Optional(),
		field.Int("max_items").Label("最多几条").Default(50),
	}
}

func (DMEvents) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []DMEvent{}).Label("私信事件").Desc("按时间正序"),
		field.String("next_cursor").Label("下一个游标"),
		field.Bool("has_more").Label("还有更多"),
		field.Int("count").Label("条数"),
	}
}

// —— 列表 ——

// ListMemberAdd 把账号加进列表。
type ListMemberAdd struct{}

func (ListMemberAdd) Meta() contract.Meta {
	return contract.Meta{ID: "x_list_member_add", Label: "列表加成员",
		Desc: "把账号加进 X 列表——配合「列表时间线」就是一套低成本的盯人方案", TimeoutSec: 30}
}

func (ListMemberAdd) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("list_id").Label("列表 id"),
		field.String("user_id").Label("用户 id").Optional(),
		field.String("username").Label("用户名").Desc("不带 @；与 id 二选一").Optional(),
	}
}

func (ListMemberAdd) Outputs() []contract.FieldSpec { return okOutput("已加入") }

// ListMemberRemove 把账号移出列表。
type ListMemberRemove struct{}

func (ListMemberRemove) Meta() contract.Meta {
	return contract.Meta{ID: "x_list_member_remove", Label: "列表移除成员", TimeoutSec: 30}
}
func (ListMemberRemove) Inputs() []contract.FieldSpec  { return ListMemberAdd{}.Inputs() }
func (ListMemberRemove) Outputs() []contract.FieldSpec { return okOutput("已移出") }

// —— 健康检查 ——

// HealthCheck 这条凭证还活着吗。
//
// **操作 id 是平台约定的 `health_check`**（credential.HealthCheckOp）：凭证页的「测试」按钮、
// 工作流里的「检查凭证」都调它。平台无从代劳——「还活着吗」怎么问只有插件自己知道。
//
// 出参 ok=false 表示凭证不可用，**这不是错误**：调用要成功返回，让上层把原因写进凭证状态。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc: "拿授权账号自己试一次（约 $0.01）。ok=false 表示要重新授权", TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明").Desc("不可用时是原因；可用时是当前账号 @用户名"),
		field.String("username").Label("授权账号").Optional(),
	}
}

// —— 事件 ——
//
// 两个事件共享 post_id / author_username（见 Events.CommonFields）：平台把公共字段平铺到
// 触发输入顶层，两条分支共用同一套变量——接「发推文」回复时绑一个就够。
//
// **为什么是轮询而不是 webhook**：X 的实时推送（filtered stream / Account Activity）
// 只在 Enterprise 档，自助档拿不到。轮询提及/搜索是唯一可行的路，间隔与配额都写在凭证里。

func postEventFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("post_id").Label("推文 id"),
		field.String("text").Label("正文"),
		field.String("author_id").Label("作者 id").Optional(),
		field.String("author_username").Label("作者用户名").Optional(),
		field.String("author_name").Label("作者昵称").Optional(),
		field.String("conversation_id").Label("对话 id").Optional(),
		field.String("url").Label("链接").Optional(),
		field.String("created_at").Label("发布时间").Optional(),
		field.String("kind").Label("类型").Desc("original / replied_to / retweeted / quoted").Optional(),
		field.String("matched_query").Label("命中的查询式").Desc("关键词命中事件才有").Optional(),
	}
}

// MentionReceived 有人 @ 了授权账号。
type MentionReceived struct{}

func (MentionReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "mention_received", Label: "被提及"}
}
func (MentionReceived) Fields() []contract.FieldSpec { return postEventFields() }

// KeywordMatched 关键词命中（凭证里配查询式）。
type KeywordMatched struct{}

func (KeywordMatched) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "keyword_matched", Label: "关键词命中"}
}
func (KeywordMatched) Fields() []contract.FieldSpec { return postEventFields() }

// Events 声明公共字段。
type Events struct{}

func (Events) CommonFields() []string { return []string{"post_id", "text"} }

// —— 凭证 ——

// Credential 本插件的凭证契约。
//
// 只有 OAuth 一条路：X 的写操作要用户身份，而 client_secret 在平台手里
// （插件永远看不到它，也不经手 refresh_token）。
type Credential struct{}

// AuthMeta：X 的 OAuth 2.0。
//
// **作用域一次要齐**：X 不支持增量授权，少申请一个就得让所有人重新授权一遍。
// offline.access 是命门——没有它就没有 refresh_token，access_token 两小时后失效且无法自愈。
func (Credential) AuthMeta() contract.AuthMeta {
	return auth.OAuth("x",
		"tweet.read", "tweet.write", "users.read",
		"like.read", "like.write",
		"bookmark.read", "bookmark.write",
		"follows.read", "follows.write",
		"list.read", "list.write",
		"dm.read", "dm.write",
		"media.write",
		"offline.access",
	)
}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("access_token").Label("访问令牌（授权注入）").Desc("点「授权」后由平台写入，勿手填").Optional(),
		field.Secret("refresh_token").Label("刷新令牌（授权注入）").Desc("勿手填").Optional(),
		// X 在境外。没有这一项，插件在国内部署装上就是废的，而症状只是「超时」，
		// 看不出是网络不通（与 notion / 搜索插件同一条教训）。
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 x.com 时必填").Optional(),

		// —— 事件源的配置 ——
		// 盯什么只能由凭证说了算：事件源是常驻进程，它没有「节点配置」这回事。
		field.Select("watch_mentions", "off", "on").
			Label("监听提及").Desc("填 on 则有人 @ 授权账号时触发工作流；off = 不监听").Default("off"),
		field.Text("watch_query").Label("监听的查询式").
			Desc("X 搜索语法（如 `\"我的品牌\" -is:retweet lang:zh`）；留空 = 不监听关键词。" +
				"**每轮都是一次计费的搜索**，间隔别调太短").Optional(),
		field.Text("poll_seconds").Label("轮询间隔（秒）").
			Desc("默认 300。X 的搜索/提及限流是 15 分钟 300 次，但真正的约束是钱——" +
				"每轮拉回的每条推文都计费").Default("300"),
		field.Text("watch_cursor").Label("轮询游标").Desc("事件源自动维护，勿手填").Optional(),
	}
}
