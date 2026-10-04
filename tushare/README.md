# tushare — TuShare Pro data retrieval (first-party built-in catalog)

Ports two always-on sync jobs from a content-sync service (industry research reports / individual
stock research reports) into a platform plugin, plus **every endpoint** on the TuShare doc site
(contracts are all generated, all activated by default, narrowable with `TUSHARE_APIS`). **Fetching
data only**: no storing to a database, no dedup, no transformation.

## Scope of responsibility

```
[scheduled trigger] → [data table · read cursor] → [this plugin · incremental pull] → [whatever downstream you wire up] → [data table · write cursor back]
```

The three steps on the canvas (pull the list → dedup → store) are a generic pattern that carries
over unchanged when swapping in a different data source. The cursor table follows the same
convention (`stream` / `cursor` / `last_ok_at` / `last_error`); it's not a platform mechanism.

## Incremental streams

| Operation | Upstream | Cursor | Notes |
|---|---|---|---|
| `sync_broker_industry_reports` | `research_report` (report_type=行业研报/"industry report") | date | Pulls one day at a time; supports `lookback_days` backfill |
| `sync_broker_stock_reports` | `research_report` (report_type=个股研报/"stock report") | date | Same as above |

Outputs: `items` / `synced_date` / `next_cursor` / `has_more` / `count`.
Once caught up to yesterday, `next_cursor` stops at today and doesn't advance further — today's
reports are still trickling in, so repeatedly pulling the same day is correct.

`lookback_days` **only kicks in once caught up**: after catching up to yesterday, `next_cursor`
rewinds to "yesterday - lookback days", and the next scheduled trigger rescans from there to pick
up reports that were backfilled a few days late. It is not subtracted from the cursor directly —
pairing "fetch day = cursor - lookback" with "next cursor = fetch day + 1" would make the cursor
net retreat (lookback - 1) days every round: setting it to 1 would spin in place (`has_more` stays
permanently true and the loop never stops), and setting it to 3 would drift further back over time.

Every record carries a `dedup_key`. **The upstream has no primary key**, so it's a hash of "trade
date + stock code + institution + title": re-running the same day during a backfill must get the
same key, otherwise every backfill adds a duplicate.

## Everything generated, not everything activated

255 pages on the doc site → **221 endpoint contracts, all generated**, all registered by default
(since 2026-09-17; before that, none were on by default).

```
catalog/tushare-apis.json     spec scraped from the doc site by cmd/catalog (checked into the repo)
  ↓ go run ./cmd/gen-schema
schema/gen_apis_NN.go         221 contract declarations + record types
gen_catalog.go                endpoint name -> registration function
  ↓ go generate ./...
zz_types.go / zz_register.go  In/Out and OnXxx for 223 operations
```

Where the 255 → 221 went: 33 are category pages (no parameter table), 1 is `pro_bar` (an SDK-side
wrapper function, not an HTTP endpoint); the scrape log lists each one by name.

```bash
TUSHARE_APIS=                     # default: everything on (same as '*')
TUSHARE_APIS=daily,trade_cal      # only turn on these (by endpoint name)
TUSHARE_APIS=行情数据,债券专题      # only turn on these groups (by category: "Quotes data", "Bond topics")
TUSHARE_APIS=none                 # nothing on
```

The two incremental streams are controlled by a separate switch, `TUSHARE_OPS` (everything on by
default).

### Why not use reportify's existing JSON

`reportify/core/tools/tushare/scraper` already scraped a 232-endpoint spec, but it **can't be
used**. It guesses the table type from the column headers (seeing "必选"/"required" it treats the
table as inputs, seeing "默认显示"/"default shown" it treats it as outputs), and misclassifies
whenever an outputs table is missing a column or a page has an extra sample table. In practice:

- **25 endpoints lost their entire outputs section** — `daily` / `daily_basic` / `margin` /
  `top10_holders` / `forecast` / `express` / `fina_mainbz`, … had their outputs table swallowed as
  inputs, with descriptive text like "opening price" or "trade date" sitting in the `required`
  column
- 26 endpoints had their inputs contaminated, and 4 endpoints had input names that were literally
  Chinese text (`hm_list` mistook "hot money" names for parameter names)
- The category list also drifted: the local `index.json` snapshot differed from the live site by
  27 new endpoints / 40 decommissioned ones, plus endpoint names mangled by a stray regex match like
  `fund_factor_pro描述`

Feeding that into codegen left 25 operations with zero output fields on the canvas, **and no
error**.

This repo's scraper instead **splits by paragraph marker**: the page structure is
`<p>输入参数</p><table>` (input parameters), `<p>输出参数</p><table>` (output parameters) — the
markers are explicit and unambiguous, and any table outside these two markers is simply ignored.
`daily` now comes out as 4 inputs / 13 outputs, matching the official page verbatim.

### Re-scraping

```bash
go run ./cmd/catalog          # ~2 minutes, concurrency 3
go run ./cmd/gen-schema
go generate ./...
```

When the same endpoint name appears on multiple doc pages (`stk_mins` is listed under both the
stock and ETF pages), they're merged into one operation; **the field sets are compared before
merging**, and a mismatch is printed for a human to see rather than silently picking one.

::: tip Two deliberate tradeoffs
**Inputs are always strings**: in a generated input struct, a numeric value type gives "not filled
in" and "filled in as 0" the same Go zero value, even though they're two different requests to the
upstream. The original type is written into the field description. Outputs keep their real type.

**fields explicitly requests every column**: TuShare only returns the default columns unless
`fields` is passed (in practice, 15% of columns aren't in the default set), while the contract
declares every column — without asking for them explicitly, a batch of fields on the canvas would
always be empty. The full column list is computed at generation time and baked into the code.
:::

## Credential

| Field | Notes |
|---|---|
| `token` | The API token from your tushare.pro personal homepage. **Which endpoints you can call depends on your account's points** |
| `base_url` | Leave empty to use the official endpoint |

When points run out, the upstream returns `code=40203` plus a Chinese explanation, which the plugin
passes along to the user verbatim — rewording it in our own words would only lose information.

## Running it

```bash
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx go run .
```

## Testing

```bash
go test ./...                              # fake upstream, runs offline
TUSHARE_TOKEN=xxx go test -run Live ./...  # hits real TuShare, verifies auth and columnar restoration
```
