// Package schema 声明 tushare 插件的操作、凭证与数据形状。
package schema

// BrokerReport 一条券商研报（research_report 接口）。
//
// 行业研报与个股研报是同一个接口的两个 report_type，字段集只差一个 ind_name，
// 故共用一个形状——个股研报的 ind_name 为空是正常的。
type BrokerReport struct {
	DedupKey string `json:"dedup_key" sokel:"dedup_key" label:"去重键" desc:"trade_date+ts_code+机构+标题 的稳定摘要——上游没有主键，只能这么对上同一篇"`

	TradeDate   string `json:"trade_date" sokel:"trade_date" label:"交易日期" desc:"格式 20060102"`
	TSCode      string `json:"ts_code" sokel:"ts_code" label:"股票代码"`
	Name        string `json:"name" sokel:"name" label:"股票名称"`
	Title       string `json:"title" sokel:"title" label:"报告标题"`
	Abstract    string `json:"abstr" sokel:"abstract" label:"报告摘要"`
	ReportType  string `json:"report_type" sokel:"report_type" label:"报告类型" desc:"行业研报 / 个股研报"`
	Author      string `json:"author" sokel:"author" label:"作者"`
	Institution string `json:"inst_csname" sokel:"institution" label:"机构简称"`
	IndName     string `json:"ind_name" sokel:"ind_name" label:"行业名称" desc:"个股研报为空"`
	URL         string `json:"url" sokel:"url" label:"报告链接"`
	FileName    string `json:"file_name" sokel:"file_name" label:"文件名"`
}
