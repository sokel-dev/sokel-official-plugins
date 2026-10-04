# tushare —— TuShare Pro 数据获取（第一方内置目录）

把一套内容同步服务里两条常驻同步任务（行业研报 / 个股研报）搬成平台插件，
外加 TuShare 文档站上的**全部接口**（契约全生成，默认全部激活，可用 `TUSHARE_APIS` 收窄）。**只负责取数**：不落库、不去重、不加工。

## 职责边界

```
[定时触发] → [数据表·取游标] → [本插件·增量拉取] → [随便你接下游] → [数据表·写回游标]
```

画布上的三步（拉列表 → 去重 → 入库）是通用写法，换个数据源就能照抄。
游标表也是同一个约定（`stream` / `cursor` / `last_ok_at` / `last_error`），不是平台机制。

## 增量流

| 操作 | 上游 | 游标 | 说明 |
|---|---|---|---|
| `sync_broker_industry_reports` | `research_report`（report_type=行业研报） | 日期 | 一次拉一天，支持 `lookback_days` 回补 |
| `sync_broker_stock_reports` | `research_report`（report_type=个股研报） | 日期 | 同上 |

出参：`items` / `synced_date` / `next_cursor` / `has_more` / `count`。
追平昨天后 `next_cursor` 停在当天不再前进——当天的研报还在陆续入库，反复拉同一天是对的。

`lookback_days` 是**追平之后退一次**：追平昨天后 `next_cursor` 退回「昨天 − 回补天数」，
下一次定时触发从那儿重扫一轮，捞回晚几天补录的研报。它不从游标上减——
「拉取日 = 游标 − 回补」配上「下一个游标 = 拉取日 + 1」会让游标每轮净退 (回补 − 1) 天，
填 1 原地打转（`has_more` 恒真，循环停不下来）、填 3 越同步越旧。

每条记录带 `dedup_key`。**上游没有主键**，故取「交易日期+股票代码+机构+标题」的摘要：
回补重跑同一天必须得到同一个键，否则回补一次就多一份。

## 全部加上，不全部激活

文档站 255 个页面 → **221 个接口契约全部生成**，默认全部注册（2026-09-17 起；此前默认一个不开）。

```
catalog/tushare-apis.json     cmd/catalog 从文档站抓的规格（进版本库）
  ↓ go run ./cmd/gen-schema
schema/gen_apis_NN.go         221 个契约声明 + 记录类型
gen_catalog.go                接口名 → 注册函数
  ↓ go generate ./...
zz_types.go / zz_register.go  223 个操作的 In/Out 与 OnXxx
```

255 → 221 的去处：33 个是分类页（没有参数表），1 个是 `pro_bar`（SDK 侧的封装函数，
不是 HTTP 接口），抓取日志里逐个列名。

```bash
TUSHARE_APIS=                     # 默认：全开（与 '*' 同义）
TUSHARE_APIS=daily,trade_cal      # 只开这几个（按接口名）
TUSHARE_APIS=行情数据,债券专题      # 只开这几段（按目录）
TUSHARE_APIS=none                 # 一个都不开
```

两条增量流走另一个开关 `TUSHARE_OPS`（默认全开）。

### 为什么不用 reportify 那份现成的 JSON

`reportify/core/tools/tushare/scraper` 已经抓过一份 232 接口的规格，但**不能用**。
它按表头猜表格类型（见「必选」当入参、见「默认显示」当出参），遇到出参表少一列
或页面多一张示例表就分错。实测：

- **25 个接口的出参整段丢失**——`daily` / `daily_basic` / `margin` / `top10_holders` /
  `forecast` / `express` / `fina_mainbz` … 出参表被当成入参吞掉，`required` 列里
  躺着「开盘价」「交易日期」这种描述文本
- 26 个接口入参被污染，4 个接口的入参名直接是中文（`hm_list` 把游资名字当成了参数名）
- 目录也漂了：本地 `index.json` 快照与线上差 27 个新接口 / 40 个已下线，
  还有 `fund_factor_pro描述` 这种正则抓歪的接口名

拿它去 codegen，结果是 25 个操作在画布上没有任何输出字段，**而且不报错**。

本目录的抓取器改成**按段落标记切**：页面结构是 `<p>输入参数</p><table>`、
`<p>输出参数</p><table>`，标记明确无歧义；这两个标记之外的表一律不看。
`daily` 现在是 4 入参 / 13 出参，与官方页面逐字一致。

### 重抓

```bash
go run ./cmd/catalog          # 约 2 分钟，并发 3
go run ./cmd/gen-schema
go generate ./...
```

同名接口出现在多个文档页时（`stk_mins` 挂在股票与 ETF 两页）合并成一个操作；
**合并前对比字段集**，不一致会打印出来让人看见，不会悄悄取其一。

::: tip 两个刻意的取舍
**入参一律 string**：生成的入参结构里数值是值类型，「没填」与「填了 0」是同一个 Go 零值，
而那对上游是两种不同的请求。原始类型写在字段说明里。出参保持类型。

**fields 显式要全列**：TuShare 不传 `fields` 只回默认列（实测 15% 的列不在默认集里），
而契约声明了全部列——不显式要，画布上就有一批字段永远是空的。生成期把全列清单算好写进代码。
:::

## 凭证

| 字段 | 说明 |
|---|---|
| `token` | tushare.pro 个人主页的接口 token。**能调哪些接口取决于账号积分** |
| `base_url` | 留空用官方端点 |

积分不够时上游回的是 `code=40203` 加一句中文说明，插件原样带给用户——
换成自己的措辞只会丢信息。

## 跑起来

```bash
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx go run .
```

## 测试

```bash
go test ./...                              # 假上游，离线可跑
TUSHARE_TOKEN=xxx go test -run Live ./...  # 打真 TuShare，验鉴权与列式还原
```
