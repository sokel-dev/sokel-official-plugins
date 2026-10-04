// Package schema declares the feed plugin's operation and credential contracts.
//
// **The unified entry point for feed subscriptions**: one operation, many sources (vendors),
// producing the same Item shape. Borrows two design decisions from RSSHub, but **does not
// depend on its service**:
//
//  1. **Uniform item shape** — ten sources normalized into one contract, so switching sources
//     on the canvas doesn't require changing anything downstream. This is the same approach as
//     this platform's search plugin (five upstreams normalized) and publisher (seven providers
//     behind one publish).
//  2. **One adapter per source** — adding a source = adding one adapter file, the contract
//     doesn't change.
//
// Two places where we **intentionally differ** from RSSHub:
//
//   - **Produces JSON, not XML.** RSS is its output format, but our downstream consumers are
//     workflow nodes — handing them XML would mean every downstream node has to parse it first.
//     Add a conversion step on the canvas if RSS is ever needed (not currently required).
//   - **Doesn't treat RSSHub as a runtime dependency.** Its 1000+ routes are ten years of
//     accumulated assets for it and also its entire maintenance cost; we only borrow the
//     knowledge of "how it fetches data" (e.g. Xueqiu goes through api.xueqiu.com's
//     user_timeline rather than scraping web pages), and implement a small, accurate set of
//     sources ourselves.
//
// **The incremental cursor is an opaque string** (containing a timestamp + recently seen ids);
// the caller just stores it back into a data table as-is — relying on the timestamp alone would
// miss items published in the same second, relying on an id set alone would grow without bound,
// and combining both avoids dropping or repeating items.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Fetch pulls incremental content from one source.
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

// HealthCheck checks whether the source is still reachable (the platform's conventional
// operation id).
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

// Credential is the credential contract.
//
// **Most sources don't need a credential**: RSS is a public URL, and Xueqiu's read API only
// needs an anonymous token (which the plugin obtains itself). The fields here are all fallbacks
// for "can't fetch it / can't connect."
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("xueqiu_cookie").Label("雪球 Cookie（可选）").
			Desc("插件默认自己去首页取匿名令牌；被风控挡住时，把浏览器里的整条 Cookie 粘进来").Optional(),
		field.Text("user_agent").Label("User-Agent").Desc("留空用常见的桌面 Chrome UA").Optional(),
		field.Text("proxy").Label("出站代理").Desc("取境外源时可能需要").Optional(),
	}
}
