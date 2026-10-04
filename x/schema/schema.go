// Package schema declares the x plugin's operations, events, and credential contract.
//
// Five decisions run through all of it (all learned the hard way from other plugins, not stylistic
// preference):
//
//  1. **Reads follow the platform's incremental-stream shape, not a copy of X's pagination.** X's
//     pagination is next_token (it expires in an hour and is only valid within a single search
//     session), while a workflow's pattern is "run every hour, continue from last time". So
//     externally this gives a since_id cursor + next_cursor/has_more — the same shape as other
//     incremental-stream plugins, with the cursor stored in a data table. next_token is only used
//     internally for auto-pagination within a single call.
//
//  2. **Retweets and replies must be distinguishable at a glance.** Most of a search result is
//     retweets; the `kind` field is derived by the plugin from referenced_tweets (X doesn't give
//     it), and without it downstream would have no choice but to guess from an "RT @" prefix.
//
//  3. **A thread is one operation, not N posts laid out on the canvas.** X has no thread endpoint —
//     threads are chained one in_reply_to at a time; laid out on the canvas that would be N nodes +
//     N connections, editing the copy once would mean touching N places, and a break partway
//     through would be untraceable.
//
//  4. **Media upload is its own operation.** It's a three-stage INIT/APPEND/FINALIZE flow plus
//     processing polling; folding it into the post operation would saddle even "post plain text"
//     with chunking logic. Splitting it out makes it an explicit canvas step that can be retried on
//     its own if it fails.
//
//  5. **Write operations always return an id and a link.** If posting doesn't hand back a link,
//     downstream has to assemble the URL by hand just to send a notification.
//
// There's only one auth path: platform-side OAuth 2.0 authorization (provider=x, with PKCE handled
// by the platform). X's API is billed per call (pay-per-use since 2026-02), and each operation's
// cost is documented in the usage doc.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/auth"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— Publishing ——

// PostCreate posts a single tweet: original, reply, quote, with media, with a poll — all go through this.
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

// PostThread posts a thread (a series of connected tweets).
//
// X has no thread endpoint: this posts them one at a time, using the previous tweet's id as the
// next one's in_reply_to. **It must be possible to continue after an interruption** — so the
// output gives "every id posted so far", not just the root id.
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

// PostDelete deletes a single tweet.
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

// —— Media ——

// MediaUpload uploads a file and returns a media_id.
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

// —— Engagement ——
//
// Like/retweet/bookmark/follow are each split into a "do" and an "undo" operation, rather than
// merged into one with a toggle. These are also agent tools: a tool named "unlike" leaves one
// fewer way to misuse it than one named "like (undo=true)" — and a model picking the wrong value
// for a toggle wouldn't even error.

func postIDInput() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("post_id").Label("推文 id")}
}

func okOutput(label string) []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label(label)}
}

// Like likes a tweet.
type Like struct{}

func (Like) Meta() contract.Meta {
	return contract.Meta{ID: "x_like", Label: "点赞", TimeoutSec: 30}
}
func (Like) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Like) Outputs() []contract.FieldSpec { return okOutput("已点赞") }

// Unlike removes a like.
type Unlike struct{}

func (Unlike) Meta() contract.Meta {
	return contract.Meta{ID: "x_unlike", Label: "取消点赞", TimeoutSec: 30}
}
func (Unlike) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Unlike) Outputs() []contract.FieldSpec { return okOutput("已取消") }

// Repost retweets.
type Repost struct{}

func (Repost) Meta() contract.Meta {
	return contract.Meta{ID: "x_repost", Label: "转推", Desc: "原样转发（要带评论就用「发推文」的引用）", TimeoutSec: 30}
}
func (Repost) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Repost) Outputs() []contract.FieldSpec { return okOutput("已转推") }

// Unrepost removes a retweet.
type Unrepost struct{}

func (Unrepost) Meta() contract.Meta {
	return contract.Meta{ID: "x_unrepost", Label: "取消转推", TimeoutSec: 30}
}
func (Unrepost) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Unrepost) Outputs() []contract.FieldSpec { return okOutput("已取消") }

// Bookmark adds a bookmark.
type Bookmark struct{}

func (Bookmark) Meta() contract.Meta {
	return contract.Meta{ID: "x_bookmark", Label: "加书签", Desc: "书签是私密的，别人看不到", TimeoutSec: 30}
}
func (Bookmark) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Bookmark) Outputs() []contract.FieldSpec { return okOutput("已收藏") }

// Unbookmark removes a bookmark.
type Unbookmark struct{}

func (Unbookmark) Meta() contract.Meta {
	return contract.Meta{ID: "x_unbookmark", Label: "取消书签", TimeoutSec: 30}
}
func (Unbookmark) Inputs() []contract.FieldSpec  { return postIDInput() }
func (Unbookmark) Outputs() []contract.FieldSpec { return okOutput("已取消") }

// Follow follows an account.
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

// Unfollow unfollows an account.
type Unfollow struct{}

func (Unfollow) Meta() contract.Meta {
	return contract.Meta{ID: "x_unfollow", Label: "取消关注", TimeoutSec: 30}
}
func (Unfollow) Inputs() []contract.FieldSpec  { return Follow{}.Inputs() }
func (Unfollow) Outputs() []contract.FieldSpec { return okOutput("已取消关注") }

// —— Reading ——
//
// The four read operations share one incremental-cursor shape (see point 1 at the top of this
// file): since_id in, next_cursor/has_more out, cursor stored in a data table — identical to other
// incremental-stream plugins.

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

// Search searches tweets from the last 7 days.
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

// UserTimeline fetches the tweets a given account posted.
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

// Mentions fetches tweets that mention the authorized account.
type Mentions struct{}

func (Mentions) Meta() contract.Meta {
	return contract.Meta{ID: "x_mentions", Label: "提及我的",
		Desc: "拉 @ 了授权账号的推文。要实时的话用本插件的事件源，不必自己定时拉", TimeoutSec: 120}
}

func (Mentions) Inputs() []contract.FieldSpec {
	return cursorInputs("留空则从最近的开始；接着上次拉就把它存进数据表")
}

func (Mentions) Outputs() []contract.FieldSpec { return cursorOutputs() }

// ListPosts fetches the tweets from a list.
//
// This is **the right way to watch a group of accounts**: add all the accounts you want to watch
// into a list and fetch them all in one call — tens of times cheaper in requests and money than
// calling user_timeline for each account individually.
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

// PostGet fetches tweets by id (can fetch several at once).
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

// UserGet looks up an account.
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

// —— Direct messages ——

// DMSend sends a direct message.
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

// DMEvents fetches direct-message events.
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

// —— Lists ——

// ListMemberAdd adds an account to a list.
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

// ListMemberRemove removes an account from a list.
type ListMemberRemove struct{}

func (ListMemberRemove) Meta() contract.Meta {
	return contract.Meta{ID: "x_list_member_remove", Label: "列表移除成员", TimeoutSec: 30}
}
func (ListMemberRemove) Inputs() []contract.FieldSpec  { return ListMemberAdd{}.Inputs() }
func (ListMemberRemove) Outputs() []contract.FieldSpec { return okOutput("已移出") }

// —— Health check ——

// HealthCheck checks whether this credential is still alive.
//
// **The operation id is the platform-mandated `health_check`** (credential.HealthCheckOp): both
// the credential page's "test" button and the workflow's "check credential" call it. The platform
// can't do this on its own — only the plugin knows how to ask "are you still alive".
//
// An output of ok=false means the credential is unusable, **and this is not an error**: the call
// must return successfully so the caller can record the reason in the credential's status.
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

// —— Events ——
//
// The two events share post_id / author_username (see Events.CommonFields): the platform flattens
// the common fields into the top level of the trigger input, so both branches share the same set
// of variables — binding one when wiring up a reply via "post" is enough.
//
// **Why polling instead of webhooks**: X's real-time push (filtered stream / Account Activity) is
// only available on the Enterprise tier, unreachable on the self-serve plan. Polling mentions/search
// is the only viable path; the interval and quota are both configured on the credential.

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

// MentionReceived fires when someone @'s the authorized account.
type MentionReceived struct{}

func (MentionReceived) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "mention_received", Label: "被提及"}
}
func (MentionReceived) Fields() []contract.FieldSpec { return postEventFields() }

// KeywordMatched fires on a keyword match (the query is configured on the credential).
type KeywordMatched struct{}

func (KeywordMatched) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "keyword_matched", Label: "关键词命中"}
}
func (KeywordMatched) Fields() []contract.FieldSpec { return postEventFields() }

// Events declares the shared fields.
type Events struct{}

func (Events) CommonFields() []string { return []string{"post_id", "text"} }

// —— Credential ——

// Credential is this plugin's credential contract.
//
// There's only one path, OAuth: X's write operations require a user identity, and client_secret is
// held by the platform (the plugin never sees it, and never touches refresh_token either).
type Credential struct{}

// AuthMeta: X's OAuth 2.0.
//
// **Scopes must all be granted in one go**: X doesn't support incremental authorization — missing
// one means making everyone re-authorize from scratch. offline.access is the critical one — without
// it there's no refresh_token, and the access_token expires in two hours with no way to recover.
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
		// X is hosted abroad. Without this field, the plugin is dead on arrival for a domestic
		// deployment, and the only symptom is "timeout" — it doesn't show that the network simply
		// can't reach it (same lesson as the notion / search plugins).
		field.Text("proxy").Label("出站代理").
			Desc("如 http://127.0.0.1:7897；部署环境直连不了 x.com 时必填").Optional(),

		// —— Event source configuration ——
		// What to watch can only be decided by the credential: the event source is a long-running
		// process, with no such thing as "per-node configuration".
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
