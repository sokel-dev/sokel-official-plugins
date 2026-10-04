# feed — feed subscription plugin (one operation, many sources, produces JSON)

Pulls new items from RSS/Atom or site APIs and produces **uniformly-shaped JSON items + an
incremental cursor**. The user-facing manual is [docs/feed.md](docs/feed.md).

## What we borrow from RSSHub, and what we don't

| RSSHub's thing | Us |
|---|---|
| **Uniform item shape** (one contract, many upstreams) | **Copied** — same approach as the search plugin (five providers normalized) and the publisher (seven providers behind one publish) |
| **One adapter per source** | **Copied** — adding a source = adding a file, the contract doesn't change |
| **1000+ scraping rules for sites** | **Not copied.** That's ten years of accumulated assets for them, and also their entire maintenance cost (every route breaks whenever the target changes its site, and the community keeps patching it). We only borrow the knowledge of "**how it fetches data**" |
| Emitting RSS (XML) output | **Not done.** Downstream consumers are workflow nodes; handing them XML would mean every node has to parse it first. Add a conversion step on the canvas if you need RSS |
| Treating it as a runtime dependency (self-hosting an instance) | **Not done** (decided by the author) — one more service to maintain, and it breaks on its own too |

**What was actually borrowed**: reading its `lib/routes/xueqiu/user.ts` is what revealed that
Xueqiu's read API is `api.xueqiu.com/v4/statuses/user_timeline.json`, and that it **only needs
an anonymous token** (obtained just by hitting the homepage) — no user login required — so
fetching data and posting are two completely separate paths. Figuring this out from scratch
would have taken half a day.

## Three design decisions

1. **Cursor = timestamp + recently seen ids** (`cursor.go`). Timestamp alone would miss a second
   item published in the same second (wire-style feeds commonly emit several items per second);
   an id set alone would grow without bound into tens of KB. Combining both avoids dropping or
   repeating items, and only the most recent 50 seen ids are kept.
2. **The first fetch doesn't backfill the entire history**: connecting a feed that's been
   publishing for ten years would otherwise flood the workflow with thousands of items at once
   (same reasoning as the x/notion event source).
3. **The summary is plain text, the HTML body is stored separately**: downstream consumers
   ("feed to a model" / "send a notification") want the former, while an RSS description is
   often a blob of raw HTML; both fields are provided so nothing is lost.

## Tolerances (all forced by domestic feeds)

- **GBK/GB2312**: plenty of sites still use this encoding, which comes out garbled if parsed as
  UTF-8 (`charset.go`);
- **Non-strict XML**: invalid entities (like `&nbsp;`) are common; strict mode would fail to
  parse the whole document;
- **RSS and Atom use different field names** (item/entry, pubDate/updated, description/summary,
  `<link>` vs. `<link href>`) — one parser has to recognize all of them, otherwise "switch feeds
  and it's empty";
  warning: the author field **can't have a separate field for each** — `encoding/xml` doesn't
  allow `author` and `author>name` to coexist, which fails the entire parse (caught while
  writing tests);
- **If a timestamp can't be recognized, leave it blank — don't guess**: guessing wrong makes the
  cursor skip genuinely new items;
- **Xueqiu's API paths are guaranteed wrong if you guess from the name**:
  `/v4/statuses/hots.json` looks the most like "hot posts" but 404s; the web-facing
  `xueqiu.com/statuses/hot/listV2.json` hits Alibaba Cloud's WAF and returns an HTML page; only
  `api.xueqiu.com/statuses/hot/listV2.json` works. Same for the anonymous token — **the homepage
  only issues the WAF's `acw_tc`, only `/hq` issues `xq_a_token`** (RSSHub pulled in Playwright
  for this step, but changing the entry point is actually enough).
- **The hot-posts list is wrapped in a shell**: the outer `items[].id` is the list-entry id,
  while the body/time/author live in `original_status`; without unwrapping it you get empty
  titles and dedup ids that never collide.
- **A flash-news `target` is an absolute URL**, while a post's is a relative path. Not handling
  them separately produces `https://xueqiu.comhttp://…`.
- **Xueqiu's `created_at` is in milliseconds**: treating it as seconds would turn 2026 into the
  year 56000, jumping the cursor into the future and cutting off all new content.

## Where to make changes when adding a source

1. Add an entry to the `source` enum in `schema/schema.go`;
2. Create `<site>.go`, producing `[]schema.Item`;
3. Add a branch to the switch in `feed.go`.

The contract, cursor, dedup, and sorting don't need to be touched — that's the whole point of
this shape.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

## Sources fall into three categories; only the first two are ported

| Category | Examples | What to do |
|---|---|---|
| **Standard feeds** | Official RSS/Atom from various sites | Handled directly by the `rss` source |
| **Site JSON APIs** | Xueqiu (`api.xueqiu.com`'s user_timeline / hot·listV2 / livenews), Cailianpress (`/api/cache` + signature), Eastmoney (JSONP, no auth), Jin10 (two fixed headers) | **Ported** — this is exactly the value of this plugin: the endpoint paths, signing algorithms, and timestamp-unit pitfalls are all handled for you |
| **HTML-only parsing** | Jisilu (list page + detail page, two hops), Gelonghui, etc. | **Not done for now.** A "HTTP Request" + "HTML Extract" node pair on the canvas can assemble this; actually folding it into the plugin would require pulling in an HTML parser and maintaining selectors per site — which is exactly where RSSHub's entire maintenance cost comes from |

Cost of porting one JSON-type source: read through its route in RSSHub once (to get the endpoint
path and signing algorithm) + write one adapter file + three lines of wiring — **half an hour**.

## Not done

- **Fetching full text** (RSS often only gives a summary; fetching the original via the link) —
  decided by the author to skip for now, the link is good enough;
- **HTML-type sources** (see table above);
- Eastmoney/THS (Tonghuashun) — check whether their routes are JSON or HTML first; the former
  can be ported, the latter goes through node composition.
