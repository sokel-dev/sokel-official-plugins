package main

// 对着**真** TuShare 跑一遍。
//
// 假上游只能验我们自己写的装配逻辑；能不能真取到数取决于 token 的积分权限，
// 而「没权限」与「接口改了」在假上游里长得一模一样。故要有一条真的。
//
//	TUSHARE_TOKEN=xxx go test -run Live ./...
//
// 没设 token 就跳过，CI 与他人机器上不会因此变红。

import (
	"context"
	"os"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func liveCtx(t *testing.T) plugin.Ctx {
	t.Helper()
	token := os.Getenv("TUSHARE_TOKEN")
	if token == "" {
		t.Skip("设 TUSHARE_TOKEN=<你的 token> 跑真实 TuShare 接口")
	}
	return fakeCtx{Context: context.Background(), cred: map[string]string{"token": token}}
}

// 交易日历最轻（不吃积分），拿它验鉴权与列式还原这两件事。
func TestLiveTradeCal(t *testing.T) {
	type rec struct {
		CalDate string `json:"cal_date"`
		IsOpen  int64  `json:"is_open"`
	}
	type in struct {
		StartDate string `json:"start_date" sokel:"start_date"`
		EndDate   string `json:"end_date" sokel:"end_date"`
	}
	recs, err := queryCatalog[rec](liveCtx(t), "trade_cal",
		&in{StartDate: "20250106", EndDate: "20250110"}, "cal_date,is_open", nil)
	if err != nil {
		t.Fatalf("拉取交易日历失败: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("交易日历回了 0 条")
	}
	if recs[0].CalDate == "" {
		t.Fatalf("字段没还原出来: %+v", recs[0])
	}
	t.Logf("交易日历 %d 条，首条 %s 开市=%d", len(recs), recs[0].CalDate, recs[0].IsOpen)
}

// 研报增量流：这条是插件的主业，验的是「游标 → 请求 → 记录 → 去重键」整条链路。
func TestLiveSyncBrokerStockReports(t *testing.T) {
	ctx := liveCtx(t)
	out, err := syncBrokerStockReports(ctx, &SyncBrokerStockReportsIn{Cursor: "20251009"})
	if err != nil {
		t.Fatalf("拉取个股研报失败（token 积分不够时会是权限错误）: %v", err)
	}
	t.Logf("个股研报 %d 条，next_cursor=%s", out.Count, out.NextCursor)
	if out.Count == 0 {
		t.Skip("那天没有研报，换个日期再试")
	}
	r := out.Items[0]
	if r.Title == "" || r.DedupKey == "" {
		t.Fatalf("记录不完整: %+v", r)
	}
	t.Logf("首条: %s / %s / %s", r.TradeDate, r.Institution, r.Title)
}
