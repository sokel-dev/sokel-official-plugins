// tushare 插件：TuShare Pro 的数据获取。
//
// 职责只有一件事——**把数据取回来**。不落库、不去重、不加工：
// 增量流回一批记录加一个 next_cursor，游标存哪、数据往哪写、重复怎么合，
// 全在画布上由数据表节点决定。与其它增量流插件同一套形状。
//
// 运行：
//
//	SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx go run .
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/tushare/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	researchReportAPI    = "research_report"
	industryReportType   = "行业研报"
	stockReportType      = "个股研报"
	defaultReportStart   = "20251001"
	researchReportFields = "trade_date,ts_code,name,title,abstr,report_type,author,inst_csname,ind_name,url,file_name"
)

// TuShare 的 trade_date 是交易所本地日期，没有时区标注。逐日推进必须按上海时间切天，
// 否则跑在 UTC 机器上会整体差一天。
var shanghai = time.FixedZone("CST", 8*3600)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" {
		log.Fatal("请设置 SOKEL_TOKEN（插件管理里该插件接入组的 token）")
	}
	p := sokel.New(sokel.Config{
		Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "tushare-plugin",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "") // 使用说明（docs/*.md）：随握手上报，界面「使用说明」显示它
	// 凭证页「检查」按钮调它（id 必须是 health_check）。不受 TUSHARE_OPS 白名单管——
	// 一个连凭证都验不了的部署，白名单开得再全也没用。
	OnHealthCheck(p, opHealthCheck)

	// 两条增量流：TUSHARE_OPS 为空或 * 时全开，否则只注册列出的。
	on := enabledOps(os.Getenv("TUSHARE_OPS"))
	if on("sync_broker_industry_reports") {
		OnSyncBrokerIndustryReports(p, syncBrokerIndustryReports)
	}
	if on("sync_broker_stock_reports") {
		OnSyncBrokerStockReports(p, syncBrokerStockReports)
	}

	// 目录接口（TuShare 全量，契约已生成）：默认全开，TUSHARE_APIS 可收窄（none = 一个不开）。
	if n := registerCatalog(p, os.Getenv("TUSHARE_APIS")); n > 0 {
		log.Printf("已激活 %d 个 TuShare 目录接口（共 %d 个可选）", n, len(catalogOps))
	}

	log.Fatal(p.Run())
}

func enabledOps(spec string) func(string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "*" {
		return func(string) bool { return true }
	}
	set := map[string]bool{}
	for _, id := range strings.Split(spec, ",") {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = true
		}
	}
	return func(id string) bool { return set[id] }
}

// ===== 增量流 =====

func syncBrokerIndustryReports(ctx plugin.Ctx, in *SyncBrokerIndustryReportsIn) (*SyncBrokerIndustryReportsOut, error) {
	items, day, hasMore, err := syncReports(ctx, industryReportType, in.Cursor, in.StartDate)
	if err != nil {
		return nil, err
	}
	return &SyncBrokerIndustryReportsOut{
		Items: items, SyncedDate: day.Format("20060102"),
		NextCursor: nextDay(day, hasMore, in.LookbackDays), HasMore: hasMore, Count: len(items),
	}, nil
}

func syncBrokerStockReports(ctx plugin.Ctx, in *SyncBrokerStockReportsIn) (*SyncBrokerStockReportsOut, error) {
	items, day, hasMore, err := syncReports(ctx, stockReportType, in.Cursor, in.StartDate)
	if err != nil {
		return nil, err
	}
	return &SyncBrokerStockReportsOut{
		Items: items, SyncedDate: day.Format("20060102"),
		NextCursor: nextDay(day, hasMore, in.LookbackDays), HasMore: hasMore, Count: len(items),
	}, nil
}

// syncReports 两条流共用：只差一个 report_type。
func syncReports(ctx plugin.Ctx, reportType, cursor, startDate string) ([]schema.BrokerReport, time.Time, bool, error) {
	day, err := resolveDateCursor(cursor, startDate, defaultReportStart)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	tradeDate := day.Format("20060102")

	data, err := clientOf(ctx).call(ctx, researchReportAPI,
		map[string]string{"trade_date": tradeDate, "report_type": reportType}, researchReportFields)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	items, err := decodeRows[schema.BrokerReport](data)
	if err != nil {
		return nil, time.Time{}, false, err
	}
	for i := range items {
		items[i].DedupKey = reportDedupKey(items[i])
	}
	// 追平判断以「昨天」为界：当天的研报还在陆续入库，停在当天反复拉才是对的。
	hasMore := day.Before(yesterday())
	return items, day, hasMore, nil
}

// reportDedupKey 上游没有主键——同一天同一家机构对同一只票可能有多篇，
// 而重跑同一天必须得到同一个键，否则回补一次就多一份。
// 故取「日期+代码+机构+标题」的摘要：这四项相同基本可以断定是同一篇。
func reportDedupKey(r schema.BrokerReport) string {
	sum := sha1.Sum([]byte(strings.Join([]string{r.TradeDate, r.TSCode, r.Institution, r.Title}, "|")))
	return "tushare:rr:" + hex.EncodeToString(sum[:])
}

// ===== 凭证体检 =====

// healthCheckAPI 体检用的探活接口。
//
// 挑 shibor_lpr 是因为它在整个目录里门槛最低（120 积分，注册就够），一天只回一两行。
// 门槛低是关键：拿一个 2000 积分的接口去探活，会把「token 有效但积分不够调那个接口」
// 报成「凭证不可用」——那是误判，而用户看到的只有一个红叉。
const healthCheckAPI = "shibor_lpr"

// opHealthCheck 打一次 healthCheckAPI，看 TuShare 认不认这个 token。
//
// 失败时返回 ok=false + message 而**不是** error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」。message 里照抄上游原文——「您的token不对，请确认。」
// 与「没有访问该接口的权限」是两码事（一个要换 token，一个要攒积分），
// 而这两者只能靠那句话分辨，换成自己的措辞等于把唯一的判据抹掉。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	day := time.Now().In(shanghai).Format("20060102")
	if _, err := clientOf(ctx).call(ctx, healthCheckAPI,
		map[string]string{"start_date": day, "end_date": day}, "date"); err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	// 当天没有数据是常态（周末、还没更新），空结果同样说明 token 通了。
	return &HealthCheckOut{OK: true, Message: "token 有效"}, nil
}

// ===== 游标 =====

// resolveDateCursor 游标优先，其次入参起始日期，最后内置默认值。**拉取日就是游标日**：
// 回补不在这儿减（见 nextDay）——「拉取日 = 游标 − 回补」配上「下一个游标 = 拉取日 + 1」
// 会让游标每轮净退 (回补 − 1) 天，回补填 1 就原地打转、填 3 就越同步越旧。
// 容忍空游标（第一次跑就是空的），但**格式错的日期要报错**——
// 悄悄退回默认起点会让同步从头再跑一遍。
func resolveDateCursor(cursor, startDate, fallback string) (time.Time, error) {
	raw := strings.TrimSpace(cursor)
	if raw == "" {
		raw = strings.TrimSpace(startDate)
	}
	if raw == "" {
		raw = fallback
	}
	day, err := time.ParseInLocation("20060102", raw, shanghai)
	if err != nil {
		return time.Time{}, fmt.Errorf("日期游标「%s」格式不对，应为 20060102 这样的八位数字", raw)
	}
	return day, nil
}

// nextDay 下一个游标：没追平就往前一天；**追平之后才回补**——退回「昨天 − 回补天数」
// 并停下（has_more 已是假），下一次定时触发从那儿重扫一轮，把晚几天补录的研报捞回来。
func nextDay(day time.Time, hasMore bool, lookbackDays int) string {
	if hasMore {
		return day.AddDate(0, 0, 1).Format("20060102")
	}
	if lookbackDays > 0 {
		return yesterday().AddDate(0, 0, -lookbackDays).Format("20060102")
	}
	return day.Format("20060102")
}

func yesterday() time.Time {
	return startOfDay(time.Now().In(shanghai).AddDate(0, 0, -1))
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, shanghai)
}

func clientOf(ctx plugin.Ctx) *client {
	c := sokel.CredentialAs[Cred](ctx)
	return newClient(c.Token, c.BaseURL)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
