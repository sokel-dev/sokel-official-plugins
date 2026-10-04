// Package schema 声明 youtube-transcript 插件的操作与凭证契约。
//
// **取 YouTube 字幕，不要 API key、不要浏览器。** 思路取自 Python 的
// jdepoix/youtube-transcript-api：走 YouTube 网页客户端自己在用的那套未公开接口
// （watch 页拿 InnerTube key → player 接口拿字幕轨清单 → timedtext 拿内容），
// 而不是 YouTube Data API v3（那条路要 key、有配额，且**根本取不到自动生成的字幕**）。
//
// 与参考项目**有意不同**的两处：
//
//   - **列清单与取内容拆成两个操作**。Python 版是一条链（list → find → fetch），
//     在画布上不好摆：用户想做的往往是「先看有没有中文人工字幕，没有再退回机翻」，
//     那是一个条件分支，需要清单先成为一份可判断的数据。
//   - **同时产出 snippets 与 text**。下游一半场景是喂给 LLM 总结（要全文），
//     另一半是做时间轴跳转/分段（要时间）。只给一种，另一半就得再取一次。
//
// **风控是这个插件唯一的真实难点**：YouTube 对机房 IP 封得很凶（返回 429 或
// 「Sign in to confirm you're not a bot」）。所以凭证里那个出站代理不是可选装饰，
// 是自部署到云上之后大概率必须配的东西——文档里必须说清楚，否则用户只会看到
// 一句「被限流」然后以为插件坏了。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// videoDesc：三个操作共用的「视频」入参说明（形态一致，说明也该一致）。
const videoDesc = "视频 id 或任意 YouTube 链接：watch?v=… / youtu.be/… / shorts/… / embed/… / live/… 都认；" +
	"直接填 11 位 id 也行"

// Fetch 取一条字幕的完整内容。
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

// List 列出这个视频有哪些字幕可用。
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

// HealthCheck 网络与风控体检（平台约定的操作 id）。
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

// Credential 凭证契约。
//
// **本插件不需要 YouTube 账号**——字幕是公开数据。这里全部字段都是为了对付一件事：
// YouTube 对机房 IP 的封锁。自部署到任何云主机上，早晚会撞到 429 或
// 「Sign in to confirm you're not a bot」，那时唯一的解法就是走一个住宅代理出去。
//
// 刻意**不做** cookie 登录：参考项目那条路已经被 YouTube 改坏了，留着只会让人以为能用。
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
