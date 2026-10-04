// Package schema declares the operation and credential contract for the youtube-transcript plugin.
//
// **Fetches YouTube transcripts, needs no API key, no browser.** The approach is borrowed from the
// Python jdepoix/youtube-transcript-api: it uses the same undocumented API the YouTube web client itself
// relies on (get the InnerTube key from the watch page → get the caption-track list from the player
// endpoint → get the content from timedtext), rather than the YouTube Data API v3 (which needs a key,
// has a quota, and **simply cannot fetch auto-generated transcripts at all**).
//
// Two places this is **deliberately different** from the reference project:
//
//   - **Listing and fetching are split into two operations.** The Python version is a single chain
//     (list → find → fetch), which doesn't lay out well on a canvas: what a user usually wants is
//     "check whether a Chinese manual transcript exists, fall back to machine translation if not" — a
//     conditional branch, which needs the list to first become data that can be inspected.
//   - **Both snippets and text are produced together.** Half of downstream use cases feed an LLM for
//     summarization (needs the full text), the other half build a timeline jump/segmentation UI (needs
//     timing). Giving only one would force a second fetch for the other half.
//
// **Anti-bot blocking is this plugin's one real hard problem**: YouTube blocks datacenter IPs
// aggressively (returning 429 or "Sign in to confirm you're not a bot"). So the outbound proxy field in
// the credential isn't an optional nicety — it's something that will almost certainly need configuring
// once this is self-deployed to the cloud, and the docs must say so clearly, otherwise a user will just
// see "rate limited" and assume the plugin is broken.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// videoDesc is the "video" input description shared by all three operations (same shape, so the
// description should be consistent too).
const videoDesc = "视频 id 或任意 YouTube 链接：watch?v=… / youtu.be/… / shorts/… / embed/… / live/… 都认；" +
	"直接填 11 位 id 也行"

// Fetch retrieves the full content of one transcript.
type Fetch struct{}

func (Fetch) Meta() contract.Meta {
	return contract.Meta{ID: "transcript_fetch", Label: "取字幕",
		Desc:       "按语言优先级取一条字幕，产出带时间轴的分句 + 拼好的全文",
		TimeoutSec: 60}
}

func (Fetch) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("video").Label("视频").Desc(videoDesc).Required(),
		field.String("languages").Label("语言优先级").
			Desc("逗号分隔，**按优先级从高到低**，如 `zh-Hans,zh,en`。取第一个命中的；留空 = en。" +
				"**认地区变体**：填 en 也能命中 en-US / en-GB；同族之内人工字幕优先，其次才看代码是否完全一致").
			Default("en"),
		field.Enum("prefer",
			field.Opt("any", "都行（人工优先）"),
			field.Opt("manual", "只要人工字幕"),
			field.Opt("generated", "只要自动生成的")).
			Label("字幕类型").
			Desc("人工字幕质量明显更好但常常没有。「都行」= 同语言下人工优先，没有才用自动生成的").
			Default("any"),
		field.String("translate_to").Label("翻译成").
			Desc("填语言代码（如 zh-Hans）则用 YouTube 自带的机器翻译转一遍。" +
				"**先按上面的优先级选轨，再翻译**；所选轨不支持翻译会报错").Optional(),
		field.Bool("preserve_formatting").Label("保留格式标记").
			Desc("保留 <b> <i> 之类的内联标签。默认去掉——多数下游要的是纯文本").Default(false),
	}
}

func (Fetch) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("video_id").Label("视频 id"),
		field.String("text").Label("全文").Desc("各分句以空格拼接，已解转义、已去标签"),
		field.Array("snippets", []Snippet{}).Label("分句").Desc("带时间轴，按时间正序"),
		field.Int("count").Label("分句数"),
		field.String("language").Label("语言名"),
		field.String("language_code").Label("语言代码"),
		field.Bool("is_generated").Label("是机器生成的"),
		field.Bool("translated").Label("经过翻译").Desc("为真表示这份文本是 YouTube 机翻的结果"),
		field.Number("duration_sec").Label("覆盖时长（秒）").Desc("最后一句的结束时刻；不等于视频总长"),
	}
}

// List lists which transcripts are available for this video.
type List struct{}

func (List) Meta() contract.Meta {
	return contract.Meta{ID: "transcript_list", Label: "列可用字幕",
		Desc:       "看这个视频有哪些语言的字幕、哪些是人工的、能翻译成什么",
		TimeoutSec: 30}
}

func (List) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("video").Label("视频").Desc(videoDesc).Required(),
	}
}

func (List) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("video_id").Label("视频 id"),
		field.Array("transcripts", []TrackInfo{}).Label("字幕轨").Desc("人工字幕排在自动生成的前面"),
		field.Int("count").Label("条数"),
		field.Bool("has_manual").Label("有人工字幕").Desc("方便直接接条件分支，不用在下游数数组"),
	}
}

// HealthCheck checks network reachability and anti-bot status (the platform-mandated operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc:       "拿一个公开视频试取一次字幕。本插件不需要账号，这里验的是**出站网络通不通、有没有被 YouTube 风控**",
		TimeoutSec: 30}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}

// Credential is the credential contract.
//
// **This plugin needs no YouTube account** — transcripts are public data. Every field here exists to
// deal with exactly one thing: YouTube's blocking of datacenter IPs. Self-deployed on any cloud
// instance, it will sooner or later hit a 429 or "Sign in to confirm you're not a bot", and the only fix
// at that point is routing through a residential proxy.
//
// Deliberately **not** implementing cookie login: the reference project's version of that path has
// already been broken by YouTube, and keeping it would only mislead people into thinking it works.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("proxy").Label("出站代理").
			Desc("形如 `http://user:pass@host:port` 或 `socks5://host:1080`。" +
				"**部署在机房/云主机上时基本是必需的**：YouTube 会用 429 或「确认你不是机器人」挡掉数据中心 IP，" +
				"住宅代理是目前唯一稳定的解法").Optional(),
		field.Text("user_agent").Label("User-Agent").
			Desc("留空用一个常见的桌面 Chrome UA").Optional(),
	}
}
