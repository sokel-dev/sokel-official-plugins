package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// 增量流的签名与其它增量流插件逐字一致（入参给游标、出参回下一个游标），
// 刻意如此：画布上「取游标 → 拉一批 → 写回游标」那三步应当换个数据源就能照抄，
// 而不是每个上游一套接法。

// dateCursorInputs 日期型增量流的公共入参。
// TuShare 的研报接口只能按 trade_date 整天取，没有增量 ID，故游标是日期。
func dateCursorInputs(startDesc string) []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("cursor").Label("游标（日期）").
			Desc("上一批返回的 next_cursor，格式 20060102。留空则从「起始日期」开始").Optional(),
		field.String("start_date").Label("起始日期").
			Desc(startDesc).Optional(),
		field.Int("lookback_days").Label("回补天数").
			Desc("追平昨天之后，下一个游标退回这么多天重扫一轮——研报会晚几天补录，靠这一步捞回来").Default(0),
	}
}

func dateCursorOutputs(itemsLabel string) []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("items", []BrokerReport{}).Label(itemsLabel),
		field.String("synced_date").Label("本次拉取日").Desc("格式 20060102，就是这次用的游标日"),
		field.String("next_cursor").Label("下一个游标").
			Desc("未追平昨天则为拉取日次日；已追平则退回「昨天 − 回补天数」等下一轮（回补为 0 时停在拉取日）"),
		field.Bool("has_more").Label("还有更多").Desc("为真表示尚未追平昨天，应当立刻再拉一次"),
		field.Int("count").Label("本批条数"),
	}
}

// SyncBrokerIndustryReports 行业研报增量流（research_report，report_type=行业研报）。
type SyncBrokerIndustryReports struct{}

func (SyncBrokerIndustryReports) Meta() contract.Meta {
	return contract.Meta{
		ID: "sync_broker_industry_reports", Label: "行业研报增量",
		Desc:       "按交易日逐天拉取券商行业研报。一次一天",
		TimeoutSec: 120,
	}
}

func (SyncBrokerIndustryReports) Inputs() []contract.FieldSpec {
	return dateCursorInputs("首次同步的起点，格式 20060102。留空用 20251001")
}

func (SyncBrokerIndustryReports) Outputs() []contract.FieldSpec {
	return dateCursorOutputs("行业研报列表")
}

// SyncBrokerStockReports 个股研报增量流（research_report，report_type=个股研报）。
type SyncBrokerStockReports struct{}

func (SyncBrokerStockReports) Meta() contract.Meta {
	return contract.Meta{
		ID: "sync_broker_stock_reports", Label: "个股研报增量",
		Desc:       "按交易日逐天拉取券商个股研报。一次一天",
		TimeoutSec: 120,
	}
}

func (SyncBrokerStockReports) Inputs() []contract.FieldSpec {
	return dateCursorInputs("首次同步的起点，格式 20060102。留空用 20251001")
}

func (SyncBrokerStockReports) Outputs() []contract.FieldSpec {
	return dateCursorOutputs("个股研报列表")
}

// Credential 凭证契约。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("token").Label("TuShare Token").
			Desc("tushare.pro 个人主页的接口 token。能调哪些接口取决于账号积分"),
		field.Text("base_url").Label("服务地址").
			Desc("留空用官方端点 https://api.tushare.pro").Optional(),
	}
}

// —— 凭证体检 ——

// HealthCheck 体检这条凭证：打一次门槛最低的接口，看 token 认不认。
//
// 操作 id 必须是 health_check——平台凭证页的「检查」按钮据此判断这个插件能不能验活。
// token 无效时返回 ok=false + message 而不是 error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」，后者才是这里要说的话。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "打一次门槛最低的 TuShare 接口，报 token 是否有效；上游原文照抄进说明——" +
			"「token 不对」与「积分不够」只能靠那句话分辨"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}
