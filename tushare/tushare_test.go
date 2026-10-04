package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/tushare/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// fakeCtx 假的调用上下文：只提供凭证。本插件不碰文件，真调用了说明写错了。
type fakeCtx struct {
	context.Context
	cred map[string]string
}

func (c fakeCtx) Credential() map[string]string { return c.cred }
func (fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	panic("本插件不产文件")
}
func (fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("本插件不产文件")
}
func (fakeCtx) Fetch(*plugin.File) ([]byte, error) { panic("本插件不收文件") }

func ctxTo(url string) plugin.Ctx {
	return fakeCtx{Context: context.Background(),
		cred: map[string]string{"token": "test-token", "base_url": url}}
}

// capture 记下真实发出的请求体——TuShare 只有一个端点，所有信息都在 body 里。
type capture struct{ reqs []tsRequest }

// fakeTuShare 起一个假 TuShare。resp 是 data 部分的 JSON。
func fakeTuShare(t *testing.T, cap *capture, resp string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req tsRequest
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &req); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		cap.reqs = append(cap.reqs, req)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"request_id":"x","code":0,"msg":null,"data":`+resp+`}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const twoReports = `{
	"fields":["trade_date","ts_code","name","title","abstr","report_type","author","inst_csname","ind_name","url","file_name"],
	"items":[
		["20251009","000001.SZ","平安银行","息差企稳","摘要一","个股研报","张三","中信证券",null,"http://x/1.pdf","1.pdf"],
		["20251009","600519.SH","贵州茅台","批价回升","摘要二","个股研报","李四","中金公司",null,"http://x/2.pdf","2.pdf"]
	]
}`

func TestSyncBrokerStockReports(t *testing.T) {
	var cap capture
	srv := fakeTuShare(t, &cap, twoReports)

	out, err := syncBrokerStockReports(ctxTo(srv.URL), &SyncBrokerStockReportsIn{Cursor: "20251009"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Fatalf("应当回 2 条，得到 %d", out.Count)
	}
	if out.SyncedDate != "20251009" || out.NextCursor != "20251010" {
		t.Errorf("拉取日 %q / 下一游标 %q，追赶历史时应当逐日推进", out.SyncedDate, out.NextCursor)
	}
	if !out.HasMore {
		t.Error("离今天还远，has_more 应当为真")
	}

	first := out.Items[0]
	if first.Title != "息差企稳" || first.Institution != "中金公司" && first.Institution != "中信证券" {
		t.Errorf("列式结果还原错了: %+v", first)
	}
	if first.TSCode != "000001.SZ" || first.Abstract != "摘要一" {
		t.Errorf("字段映射错了: %+v", first)
	}
	// ind_name 是 null（个股研报没有行业），不该炸，落成空串即可。
	if first.IndName != "" {
		t.Errorf("null 应当落成空串，得到 %q", first.IndName)
	}

	// 真实发出的请求：api_name / trade_date / report_type / fields 一个都不能少。
	req := cap.reqs[0]
	if req.APIName != "research_report" {
		t.Errorf("api_name = %q", req.APIName)
	}
	if req.Params["trade_date"] != "20251009" || req.Params["report_type"] != "个股研报" {
		t.Errorf("params = %+v", req.Params)
	}
	if !strings.Contains(req.Fields, "inst_csname") {
		t.Errorf("fields = %q，不显式要全列的话 TuShare 只回默认列", req.Fields)
	}
	if req.Token != "test-token" {
		t.Errorf("token 没带上: %q", req.Token)
	}
}

func TestSyncBrokerIndustryReportsUsesIndustryType(t *testing.T) {
	var cap capture
	srv := fakeTuShare(t, &cap, twoReports)
	if _, err := syncBrokerIndustryReports(ctxTo(srv.URL), &SyncBrokerIndustryReportsIn{Cursor: "20251009"}); err != nil {
		t.Fatal(err)
	}
	if cap.reqs[0].Params["report_type"] != "行业研报" {
		t.Errorf("report_type = %q，两条流只差这一个参数，串了就是拉错数据",
			cap.reqs[0].Params["report_type"])
	}
}

// 去重键必须只由内容决定：回补重跑同一天要得到同一个键，否则回补一次就多一份。
func TestReportDedupKeyIsStable(t *testing.T) {
	r := schema.BrokerReport{TradeDate: "20251009", TSCode: "000001.SZ", Institution: "中信证券", Title: "息差企稳"}
	first := reportDedupKey(r)
	r.Abstract = "摘要改了" // 摘要变化不该改变身份
	if second := reportDedupKey(r); first != second {
		t.Errorf("同一篇研报算出两个键: %s vs %s", first, second)
	}
	other := r
	other.Title = "另一篇"
	if reportDedupKey(other) == first {
		t.Error("不同标题算出了同一个键")
	}
	if !strings.HasPrefix(first, "tushare:rr:") {
		t.Errorf("键少了来源前缀: %s", first)
	}
}

// 追平之后停在当天反复拉：当天的研报还在陆续入库。
func TestSyncStopsAtToday(t *testing.T) {
	var cap capture
	srv := fakeTuShare(t, &cap, `{"fields":["trade_date"],"items":[]}`)
	today := time.Now().In(shanghai).Format("20060102")

	out, err := syncBrokerStockReports(ctxTo(srv.URL), &SyncBrokerStockReportsIn{Cursor: today})
	if err != nil {
		t.Fatal(err)
	}
	if out.HasMore || out.NextCursor != today {
		t.Errorf("已经追到今天了：has_more=%v next=%q", out.HasMore, out.NextCursor)
	}
}

func TestResolveDateCursor(t *testing.T) {
	day, err := resolveDateCursor("20251009", "", "20251001")
	if err != nil {
		t.Fatal(err)
	}
	if got := day.Format("20060102"); got != "20251009" {
		t.Errorf("拉取日就是游标日 = %s，想要 20251009", got)
	}
	if d, _ := resolveDateCursor("", "", "20251001"); d.Format("20060102") != "20251001" {
		t.Error("空游标应当退回内置起点")
	}
	if _, err := resolveDateCursor("2025-10-09", "", "20251001"); err == nil {
		t.Error("格式不对的日期游标必须报错——悄悄退回起点会让同步从头重跑")
	}
}

// 回补不许把游标拽回去：曾经是「拉取日 = 游标 − 回补」而 next = 拉取日 + 1，
// 于是回补填 1 原地打转（且 has_more 恒真，画布上的循环停不下来）、填 3 每轮净退 2 天。
func TestLookbackDoesNotRewindCursor(t *testing.T) {
	var cap capture
	srv := fakeTuShare(t, &cap, `{"fields":["trade_date"],"items":[]}`)

	out, err := syncBrokerStockReports(ctxTo(srv.URL), &SyncBrokerStockReportsIn{
		Cursor: "20251009", LookbackDays: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SyncedDate != "20251009" {
		t.Errorf("拉取日就是游标日，得到 %s", out.SyncedDate)
	}
	if !out.HasMore || out.NextCursor != "20251010" {
		t.Errorf("追赶阶段每轮前进一天，得到 next=%s has_more=%v", out.NextCursor, out.HasMore)
	}
}

// 追平之后才回补：下一个游标退回「昨天 − 回补天数」，等下一次定时触发重扫一轮。
func TestLookbackKicksInAfterCatchUp(t *testing.T) {
	var cap capture
	srv := fakeTuShare(t, &cap, `{"fields":["trade_date"],"items":[]}`)
	today := time.Now().In(shanghai).Format("20060102")

	out, err := syncBrokerStockReports(ctxTo(srv.URL), &SyncBrokerStockReportsIn{
		Cursor: today, LookbackDays: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.HasMore {
		t.Error("已经追到今天了，has_more 应当为假")
	}
	if want := yesterday().AddDate(0, 0, -3).Format("20060102"); out.NextCursor != want {
		t.Errorf("追平后应当退回 %s 重扫，得到 %s", want, out.NextCursor)
	}
}

// 业务错误装在 HTTP 200 里（code != 0）。只看状态码会把「没权限」当成空数据，
// 于是游标照常前进，那一段数据就静默丢了。
func TestBusinessErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":40203,"msg":"抱歉，您没有访问该接口的权限","data":null}`)
	}))
	defer srv.Close()

	_, err := syncBrokerStockReports(ctxTo(srv.URL), &SyncBrokerStockReportsIn{Cursor: "20251009"})
	if err == nil {
		t.Fatal("code!=0 必须变成错误，不能当成空结果")
	}
	if !strings.Contains(err.Error(), "没有访问该接口的权限") {
		t.Errorf("上游的话应当原样带给用户，得到: %v", err)
	}
}

func TestDecodeRowsHandlesShortRowsAndNulls(t *testing.T) {
	type rec struct {
		A string  `json:"a"`
		B float64 `json:"b"`
	}
	// 行比列少一格（上游偶尔如此）不该整批失败，缺的那格留零值。
	got, err := decodeRows[rec](&tsData{Fields: []string{"a", "b"}, Items: [][]any{{"x", nil}, {"y"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].A != "x" || got[0].B != 0 || got[1].A != "y" {
		t.Errorf("还原错了: %+v", got)
	}
	if rows, err := decodeRows[rec](&tsData{}); err != nil || rows != nil {
		t.Errorf("空结果应当回 nil，得到 %+v %v", rows, err)
	}
}

func TestCatalogTableIsPopulated(t *testing.T) {
	if len(catalogOps) < 200 {
		t.Fatalf("目录接口只有 %d 个，TuShare 文档有 220+ ——多半是抓漏或生成器过滤过头", len(catalogOps))
	}
	for _, id := range []string{"daily", "stock_basic", "trade_cal", "research_report"} {
		if op, ok := catalogOps[id]; !ok {
			t.Errorf("目录里缺了 %s", id)
		} else if op.Category == "" || op.Register == nil {
			t.Errorf("%s 的登记不完整", id)
		}
	}
}

func TestCatalogMatcher(t *testing.T) {
	cases := []struct {
		name, spec, id, category string
		want                     bool
	}{
		{"默认全开", "", "daily", "股票数据/行情数据", true},
		{"星号全开", "*", "daily", "股票数据/行情数据", true},
		{"none 一个不开", "none", "daily", "股票数据/行情数据", false},
		{"按接口名", "daily", "daily", "股票数据/行情数据", true},
		{"没列出的不开", "daily", "trade_cal", "股票数据/基础数据", false},
		{"按目录整段开", "行情数据", "daily", "股票数据/行情数据/历史日线", true},
		{"目录不匹配", "行情数据", "cb_basic", "债券专题/可转债", false},
		{"混列", "daily, 债券专题", "cb_basic", "债券专题/可转债", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := catalogMatcher(c.spec)(c.id, c.category); got != c.want {
				t.Errorf("catalogMatcher(%q)(%q, %q) = %v，想要 %v", c.spec, c.id, c.category, got, c.want)
			}
		})
	}
}

// 生成的目录接口：入参装配 + fields 全列 + 列式还原，走一遍假上游。
func TestQueryCatalogAssemblesRequest(t *testing.T) {
	var cap capture
	srv := fakeTuShare(t, &cap, `{"fields":["ts_code","close"],"items":[["000001.SZ",12.34]]}`)

	type in struct {
		TsCode    string `json:"ts_code" sokel:"ts_code"`
		TradeDate string `json:"trade_date" sokel:"trade_date"`
	}
	type rec struct {
		TSCode string  `json:"ts_code"`
		Close  float64 `json:"close"`
	}
	recs, err := queryCatalog[rec](ctxTo(srv.URL), "daily",
		&in{TsCode: " 000001.SZ ", TradeDate: ""}, "ts_code,close", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Close != 12.34 {
		t.Fatalf("记录没还原出来: %+v", recs)
	}
	req := cap.reqs[0]
	if req.Params["ts_code"] != "000001.SZ" {
		t.Errorf("值应当去掉空白后带上，得到 %q", req.Params["ts_code"])
	}
	if _, ok := req.Params["trade_date"]; ok {
		t.Error("空参数不该出现在请求里")
	}
	if req.Fields != "ts_code,close" {
		t.Errorf("fields = %q", req.Fields)
	}
}
