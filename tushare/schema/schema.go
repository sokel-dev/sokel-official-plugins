package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// The incremental-stream signature matches the other incremental-stream plugins verbatim (inputs
// take a cursor, outputs return the next cursor), deliberately so: the canvas's three-step "read
// cursor -> pull a batch -> write the cursor back" should carry over unchanged when swapping in a
// different data source, instead of each upstream getting its own wiring.

// dateCursorInputs holds the shared inputs for date-based incremental streams.
// TuShare's research report endpoints can only be fetched a whole trade_date at a time, with no
// incremental ID, hence the cursor is a date.
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

// SyncBrokerIndustryReports is the industry research report incremental stream (research_report,
// report_type=行业研报/"industry report").
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

// SyncBrokerStockReports is the individual-stock research report incremental stream
// (research_report, report_type=个股研报/"stock report").
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

// Credential is the credential contract.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Secret("token").Label("TuShare Token").
			Desc("tushare.pro 个人主页的接口 token。能调哪些接口取决于账号积分"),
		field.Text("base_url").Label("服务地址").
			Desc("留空用官方端点 https://api.tushare.pro").Optional(),
	}
}

// —— Credential health check ——

// HealthCheck checks this credential: hits the lowest-threshold endpoint once to see whether the
// token is recognized.
//
// The operation id must be health_check — the platform's credential page uses this to decide
// whether this plugin's "check" button can verify liveness. When the token is invalid, it returns
// ok=false + message instead of an error: the platform treats an error as "this plugin can't run a
// health check" and ok=false as "the health check concluded it's unusable" — the latter is what
// needs to be said here.
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
