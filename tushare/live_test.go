package main

// Runs once against the **real** TuShare.
//
// A fake upstream can only verify the assembly logic we wrote ourselves; whether data can actually
// be fetched depends on the token's point-based permission tier, and "no permission" and "the
// endpoint changed" look identical against a fake upstream. Hence we need a real one.
//
//	TUSHARE_TOKEN=xxx go test -run Live ./...
//
// Skipped when the token isn't set, so this doesn't turn CI or anyone else's machine red.

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

// Trading calendar is the lightest (doesn't cost points), so it's used to verify both auth and
// columnar restoration.
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

// Research report incremental stream: this is the plugin's core job, verifying the whole
// cursor -> request -> record -> dedup-key chain.
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
