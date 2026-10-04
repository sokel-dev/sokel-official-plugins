// tushare plugin: data retrieval for TuShare Pro.
//
// It has exactly one responsibility — **fetch the data back**. No storing to a database, no
// dedup, no transformation: an incremental stream returns a batch of records plus a next_cursor;
// where the cursor is stored, where the data is written, and how duplicates are merged, are all
// decided on the canvas by the data table node. Same shape as the other incremental-stream plugins.
//
// Run:
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

// TuShare's trade_date is the exchange's local date, with no timezone marker. Advancing day by day
// must split days by Shanghai time, otherwise running on a UTC machine shifts everything off by a
// day.
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
	p.SetDoc(usageDoc, "") // usage doc (docs/*.md): reported along with the handshake, shown in the UI as "usage instructions"
	// Called by the "check" button on the credential page (its id must be health_check). Not
	// governed by the TUSHARE_OPS allowlist — a deployment that can't even verify its credential
	// gains nothing from the allowlist being wide open.
	OnHealthCheck(p, opHealthCheck)

	// Two incremental streams: all on when TUSHARE_OPS is empty or *, otherwise only the listed ones
	// are registered.
	on := enabledOps(os.Getenv("TUSHARE_OPS"))
	if on("sync_broker_industry_reports") {
		OnSyncBrokerIndustryReports(p, syncBrokerIndustryReports)
	}
	if on("sync_broker_stock_reports") {
		OnSyncBrokerStockReports(p, syncBrokerStockReports)
	}

	// Catalog endpoints (all of TuShare, contracts already generated): all on by default, narrowed
	// down via TUSHARE_APIS (none = nothing on).
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

// ===== Incremental streams =====

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

// syncReports is shared by both streams; they only differ by report_type.
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
	// "Caught up" is judged against "yesterday": today's reports are still trickling in, so it's
	// correct to stop at today and keep re-pulling it.
	hasMore := day.Before(yesterday())
	return items, day, hasMore, nil
}

// reportDedupKey: the upstream has no primary key — the same institution can publish multiple
// reports on the same stock on the same day, and re-running the same day must get the same key,
// otherwise every backfill adds a duplicate. So it hashes "date+code+institution+title": a match on
// all four is good enough to conclude it's the same report.
func reportDedupKey(r schema.BrokerReport) string {
	sum := sha1.Sum([]byte(strings.Join([]string{r.TradeDate, r.TSCode, r.Institution, r.Title}, "|")))
	return "tushare:rr:" + hex.EncodeToString(sum[:])
}

// ===== Credential health check =====

// healthCheckAPI is the probe endpoint used for the health check.
//
// shibor_lpr was picked because it has the lowest threshold in the whole catalog (120 points, just
// registering is enough), and only returns one or two rows a day. A low threshold is the key point:
// probing with a 2000-point endpoint would report "token is valid but not enough points for that
// endpoint" as "credential unusable" — that's a false positive, and all the user sees is a red X.
const healthCheckAPI = "shibor_lpr"

// opHealthCheck hits healthCheckAPI once to see whether TuShare recognizes this token.
//
// On failure it returns ok=false + message, **not** an error: the platform treats an error as "this
// plugin can't run a health check" and ok=false as "the health check concluded it's unusable". The
// message copies the upstream text verbatim — "your token is invalid, please check" is a different
// case from "no permission to access this endpoint" (one needs a new token, the other needs more
// points), and the only way to tell them apart is that exact text; rewording it in our own words
// would erase the only signal we have.
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	day := time.Now().In(shanghai).Format("20060102")
	if _, err := clientOf(ctx).call(ctx, healthCheckAPI,
		map[string]string{"start_date": day, "end_date": day}, "date"); err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	// No data for today is normal (weekend, not updated yet); an empty result still means the token
	// went through.
	return &HealthCheckOut{OK: true, Message: "token 有效"}, nil
}

// ===== Cursor =====

// resolveDateCursor prefers the cursor, then the input's start date, then the built-in default.
// **The fetch day is the cursor day**: the lookback isn't subtracted here (see nextDay) — pairing
// "fetch day = cursor - lookback" with "next cursor = fetch day + 1" would make the cursor net
// retreat (lookback - 1) days every round, spinning in place at lookback=1 and drifting further
// back over time at lookback=3. An empty cursor is tolerated (the first run starts empty), but
// **a malformed date must error out** — silently falling back to the default start would make the
// sync run from scratch all over again.
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

// nextDay computes the next cursor: advance one day while not caught up; **the lookback only kicks
// in once caught up** — it rewinds to "yesterday - lookback days" and stops there (has_more is
// already false), so the next scheduled trigger rescans from there and picks up any reports that
// were backfilled a few days late.
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
