# Feed subscriptions (feed)

Fetch new items from a source and return **JSON with a consistent structure**. Changing sources does not require changing downstream nodes.

## Sources

| Source | What to enter in Target | Credentials required? |
|---|---|---|
| **RSS/Atom URL** | The complete feed URL | No |
| **Xueqiu · User activity** | The UID from `xueqiu.com/u/<this-part>`; a complete profile URL also works | No; the plugin obtains an anonymous token |
| **Xueqiu · Trending posts** | Leave blank | Same as above |
| **Xueqiu · News flashes** | Leave blank | Same as above |
| **Cailian Press · Telegraph** | Leave blank for the complete feed, or enter a category | No; the plugin calculates the signature |
| **Eastmoney · Keyword search** | A keyword, such as a stock name or code | No |
| **Jin10 · News flashes** | A channel number; blank means all news flashes | No; the plugin supplies fixed request headers |

## Output: an individual item

```json
{
  "id": "...", "title": "...", "url": "https://...",
  "summary": "Plain-text summary with HTML removed, up to 500 characters",
  "content_html": "Body supplied by the source; RSS often provides only a summary",
  "author": "...", "published_at": "2026-08-19T02:00:00Z",
  "source": "xueqiu_user:1234", "tags": [], "images": [],
  "dedup_key": "..."
}
```

Use `dedup_key` for downstream database upserts, following the same convention as the platform's other data-fetching plugins.

## Incremental fetching

```
[Schedule trigger] → [Data table: read cursor] → [Fetch content] → [Your downstream nodes] → [Data table: save cursor]
```

The cursor is an **opaque string**. Save it unchanged in the data table; do not parse it.
When `has_more` is true, the batch is full: **fetch again immediately** instead of waiting for the next scheduled run.

**The first fetch, with an empty cursor, returns only the latest batch and does not backfill the entire history.**
Otherwise, connecting a ten-year-old source could overwhelm the workflow with thousands of items at once.

> A cursor looks like `2026-08-19T08:35:52Z|2ceab3c4,6bb9e374,…`.
> It contains both the latest timestamp and recently seen IDs. A timestamp alone would miss the second item published
> in the same second, which is common for news flashes. An ID set alone would grow indefinitely.
> Combining them avoids those gaps and duplicates.

## Credentials: usually leave blank

| Field | When to fill it in |
|---|---|
| Xueqiu Cookie | Paste the entire Cookie header from a browser if anonymous tokens are blocked by risk controls |
| User-Agent | To match the browser from which you obtained the cookie |
| Outbound proxy | When fetching sources outside mainland China |

## More source types

The plugin currently covers **standard feeds** (RSS/Atom) and **site-specific JSON endpoints** (such as Xueqiu and Cailian Press).
Some sites expose neither feeds nor JSON endpoints and require parsing a web page. Those are **not currently included in this plugin**;
on the canvas, combine an **HTTP Request** node with an **HTML Extract** node for the same result.

## Troubleshooting

**“No items could be parsed”**: the address is probably a web page rather than a feed. Look for a Subscribe link in the footer,
or a `<link rel="alternate" type="application/rss+xml">` element.

**Garbled Chinese text**: the plugin already handles GBK/GB2312 sources. If the text is still garbled, send the URL to a developer.

**No Xueqiu content**: the anonymous token has been blocked. Paste a Cookie header as described above.

**No Jin10 items**: its two required headers, `x-app-id` and `x-version`, may change when its frontend is updated.
The plugin identifies this cause explicitly; updating the two constants resolves it.

**No Cailian Press items**: its signature parameters, particularly version `sv`, have probably changed.
The plugin explicitly reports that the signature may have changed instead of pretending there is no new content.
Report the error to a developer so the constant can be updated.

**Empty time field**: the source did not provide a parseable timestamp. The plugin **does not guess**, because an incorrect timestamp
could advance the cursor past real new items. Deduplication for such sources relies entirely on IDs and continues to work.
