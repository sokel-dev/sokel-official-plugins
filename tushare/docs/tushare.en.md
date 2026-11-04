# TuShare Pro

Incremental brokerage research reports plus 221 TuShare catalog APIs. **This plugin only retrieves data**: it does not persist, deduplicate, or process records.

## Credentials

| Field | Description |
|---|---|
| `token` | **Required.** The API TOKEN on your tushare.pro profile page |
| `base_url` | Leave empty to use the official endpoint |

After entering the token, click **“Check”** on the credential row. It calls an API with the lowest access threshold, 120 points, and preserves TuShare's original response message. “您的token不对，请确认。” means the token is invalid and must be replaced; “没有访问该接口的权限” means insufficient points. The response message is how these two cases are distinguished.

⚠️ **TuShare grants access according to points.** Every API has a points threshold: research-report APIs start at 2,000 points, with some requiring 5,000. Insufficient points produce a business error inside an HTTP 200 envelope. The plugin reports these as errors; it **does not treat them as empty data and advance the cursor**, which would silently lose an entire range of records.

## Two incremental streams

| Operation | Description |
|---|---|
| **Incremental brokerage industry reports** | Retrieve one trading day at a time |
| **Incremental brokerage stock reports** | Same behavior as above |

Outputs: `items` / `synced_date` / `next_cursor` / `has_more` / `count`.

**Every record includes `dedup_key`.** TuShare research reports **have no primary key**. The plugin hashes the combination of trading date, stock code, institution, and title. Reprocessing the same day during lookback must yield the same key; otherwise every lookback would add another copy.

## Incremental retrieval and lookback

```
[Scheduled trigger] → [Data table: read cursor] → [Incremental research reports] → [Your downstream steps] → [Data table: save cursor]
```

After catching up to yesterday, `next_cursor` stays on the current day for repeated retrieval. Today's reports are still being added, so repeatedly fetching that day is correct.

`lookback_days` means **rewind once after catching up**. After reaching yesterday, the cursor rewinds to “yesterday minus the lookback period.” The next scheduled run rescans from there to collect reports entered several days late. **It does not subtract from the current cursor**: that would move the cursor backward by (lookback − 1) days on each run and sync progressively older data. This was a previously fixed bug. The default is 0.

## 221 catalog APIs

Contracts have been generated for **all APIs in the TuShare documentation**, and **all are enabled by default**. Restrict them with the `TUSHARE_APIS` environment variable, for example `TUSHARE_APIS=stock_basic,daily,fina_indicator`; use `none` to disable them all.
Enabling every API makes the canvas node picker difficult to navigate, and few workflows need all 221.

## Pitfalls

- **Use Shanghai time for day boundaries.** TuShare's `trade_date` is the exchange-local date without a timezone annotation. Calculating dates in the local timezone of a UTC machine shifts them by a day.
- **Calls without enough points fail rather than return empty data.** Check your points tier on tushare.pro first.
