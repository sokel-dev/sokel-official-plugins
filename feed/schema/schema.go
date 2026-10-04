// Package schema 声明 feed 插件的操作与凭证契约。
//
// **Feed 订阅的统一入口**：一个操作、多个来源（vendor），产出同一个 Item 形状。
// 借鉴 RSSHub 的两条判断，但**不依赖它的服务**：
//
//  1. **统一条目形状**——十个来源归一到一份契约，画布上换来源不用改下游。
//     这与本平台搜索插件（五家上游归一）、发布器（七家同一套 publish）是同一条路子。
//  2. **一源一适配器**——加一家 = 加一个 adapter 文件，契约不动。
//
// 与 RSSHub 的两处**有意不同**：
//
//   - **产出 JSON 不是 XML**。RSS 是它的输出格式，而我们的下游是工作流节点——
//     给 XML 等于让每个下游都先解一次。要 RSS 的话在画布上加一步转换（暂时不需要）。
//   - **不把 RSSHub 当运行时依赖**。它的 1000+ route 是它十年攒的资产也是它全部的维护成本；
//     我们只借鉴「它怎么取数」这件知识（如雪球走 api.xueqiu.com 的 user_timeline，
//     而不是爬网页），自己实现少而准的几家。
//
// **增量游标是不透明串**（内含时间戳 + 最近见过的 id），调用方原样存回数据表即可——
// 只按时间戳会漏掉同秒的条目，只按 id 集合会无限膨胀，两者合起来才既不漏也不重。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Fetch 拉一个来源的增量。
type Fetch struct{}

func (Fetch) Meta() contract.Meta {
	return contract.Meta{ID: "feed_fetch", Label: "取内容",
		Desc: "从一个来源拉新条目（RSS/Atom 或站点接口），产出统一的 JSON 条目 + 增量游标", TimeoutSec: 120}
}

func (Fetch) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("source",
			field.Opt("rss", "RSS/Atom 地址（通用）"),
			field.Opt("xueqiu_user", "雪球 · 某用户的动态"),
			field.Opt("xueqiu_hots", "雪球 · 热帖"),
			field.Opt("xueqiu_livenews", "雪球 · 快讯"),
			field.Opt("cls_telegraph", "财联社 · 电报"),
			field.Opt("eastmoney_search", "东方财富 · 关键词搜索"),
			field.Opt("jin10", "金十数据 · 快讯")).
			Label("来源").Default("rss"),
		field.String("target").Label("目标").
			Desc("按来源填：RSS 填 feed 地址；雪球用户填 uid（主页地址 xueqiu.com/u/<这一串>）；" +
				"雪球热帖/快讯不用填；" +
				"财联社电报填分类（留空 = 全量电报流）；东方财富填关键词（如某只票的名称或代码）；" +
				"金十填频道号（留空 = 全部快讯）").Optional(),
		field.String("cursor").Label("游标").
			Desc("上一批返回的 next_cursor，**原样存回数据表**。留空 = 第一次拉（只取最近一批，不回溯全部历史）").Optional(),
		field.Int("max_items").Label("最多几条").Desc("默认 50").Default(50),
		field.Bool("skip_reposts").Label("跳过转发").Desc("只对雪球有效：转发内容通常不是原创观点").Default(false),
	}
}

func (Fetch) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []Item{}).Label("条目").Desc("**按时间正序**（老的在前），与游标推进方向一致"),
		field.String("next_cursor").Label("下一个游标").Desc("没有新内容时原样返回传入的游标"),
		field.Bool("has_more").Label("还有更多").Desc("为真表示这批拉满了，应当立刻再拉一次"),
		field.Int("count").Label("本批条数"),
	}
}

// HealthCheck 来源还活着吗（平台约定的操作 id）。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证",
		Desc:       "试着取一次雪球的匿名令牌。本插件多数来源不需要凭证，所以这里主要验网络是否通",
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
// **多数来源不需要凭证**：RSS 是公开地址，雪球的读接口只要一个匿名令牌（插件自己取）。
// 这里的字段都是「取不到/连不上时的退路」。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("xueqiu_cookie").Label("雪球 Cookie（可选）").
			Desc("插件默认自己去首页取匿名令牌；被风控挡住时，把浏览器里的整条 Cookie 粘进来").Optional(),
		field.Text("user_agent").Label("User-Agent").Desc("留空用常见的桌面 Chrome UA").Optional(),
		field.Text("proxy").Label("出站代理").Desc("取境外源时可能需要").Optional(),
	}
}
