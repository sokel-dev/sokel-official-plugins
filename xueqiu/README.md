# xueqiu — Xueqiu posting plugin (**unofficial interface**)

Xueqiu has no official publish API (its open platform only covers market data, see
[docs/social-publishing-plugins.md](../../docs/social-publishing-plugins.md) §4).
This plugin calls **the private endpoints the web frontend uses itself** — plain HTTP, no
browser involved.

The user-facing guide is [docs/xueqiu.md](docs/xueqiu.md).

## Two APIs, two subdomains

**Short posts go through `xueqiu.com`, long-form articles go through
`mp.xueqiu.com`** — same cookie, completely different endpoints.

| Purpose | Request | Source |
|---|---|---|
| Post a short status | `POST xueqiu.com/statuses/update.json`, form fields `status`(HTML) / **`session_token`** / `ai_disclose` / `allow_reward` | captured from the browser |
| Upload image for a short post | `POST xueqiu.com/photo/upload.json`, multipart `file` | captured from the browser |
| **Write a long-form article (save as draft)** | `POST mp.xueqiu.com/xq/statuses/draft/save.json`, form fields `title` / `text`(HTML) / `is_private` — **no session_token needed, no risk-control param observed** | based on wechatsync's xueqiu driver |
| Upload image for a long-form article | `POST mp.xueqiu.com/xq/photo/upload.json` → returns `{url, filename}` as two parts that must be joined | same as above |
| Login state | `GET mp.xueqiu.com/write/`, the page embeds `window.UOM_CURRENTUSER` → login state + uid + display name | same as above |

> The `GET /etc/private_fund/state.json` the user originally provided also works as a
> login-state probe, but the writer page's semantics are **more direct** (it's literally
> "can you write"), and it also gives uid and display name along the way, so `health_check`
> uses the latter.

**Long-form is where research content really belongs**, and that API is also much
cleaner — it has neither of the two easiest-to-break things: session_token and a
risk-control param. Short posts suit a one-line take.

**Long-form articles produce a draft, not a published post**: `save.json` lands in the
draft box, leaving the final "publish" step to a human clicking it on the website. This
matches the endpoint's own semantics, and also happens to be the safer shape from a
compliance standpoint — automated drafting, human publishing.

Four headers are key to Xueqiu recognizing a real client: `Cookie` / `User-Agent` /
`X-Requested-With: XMLHttpRequest` / `Referer`+`Origin`. Missing any one risks being
blocked by the WAF, which returns **a full HTML page** unrelated to login state.

## Five design decisions

1. **The response shape is guesswork → loose extraction.** Undocumented, so pinning down
   a key name would turn one Xueqiu field rename into a blanket parse failure. An image
   address is found by **content** (the string containing the image-host domain); a post
   id is found recursively by key name.
2. **Failures come in three shapes**: an HTTP 403/401, an `error_code` inside a JSON
   envelope, and a full HTML page when blocked by risk control. The third is the
   nastiest — parsing it as JSON would just say "parse failed", so `translate` specifically
   recognizes it and tells the user to paste a risk-control param.
3. **`session_token` has a three-tier fallback**: pasted into the credential → scraped
   from the homepage → an error that **tells the user exactly where to copy it from**.
   Without this, it would send a request that's bound to be rejected.
4. **Text is escaped before being assembled.** A single `<` in plain text can break the
   whole structure; deciding "this is already HTML" requires a complete tag shaped like
   `<p …>` — looking only for `<p` would let "A<B" through too (pinned by a test).
5. **`ai_disclose` is an explicit input**, not hardcoded to 0. It should be set to 1
   whenever content has been rewritten by an LLM — this is a compliance requirement
   ([GEO plan](../../docs/geo-platform.md) §6), not optional.

## Two things that remain unverified (**only affect the short-post path**)

Both showed up during packet capture, but **whether they're actually required hasn't been
verified** (there's no way to send a real request from here). The long-form API needs
neither, so use long-form first if reliability matters:

- **`md5__1038` (risk-control param)**: the plugin leaves it off by default, and only
  attaches it when it's set in the credential. If posting returns a full HTML page, that
  means it's required → paste one into the credential.
  **If it turns out to be computed on the fly by JS**, this path would need to either
  replicate the algorithm or periodically fetch it with a real browser — the one place a
  browser might end up being necessary.
- **Where `session_token` comes from**: currently scraped from the homepage with a regex.
  If that fails, the user is explicitly told to paste one manually.

Once this has actually been verified working end to end, update this section with the result.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

Tests use a fake Xueqiu, pinning down: request shape (form fields + four headers), loose
extraction holding up against key renames, risk-control HTML being explained in plain
language, zero side effects on health check, text escaping, and the token's three-tier fallback.

## Not done / caveats

- **Long-form articles, comments, and deletion** aren't done yet (long-form is a separate
  endpoint that hasn't been captured yet; deleting via the website is safer in case of a
  mistaken post).
- **This is unauthorized automation**: only post genuine, self-authored content, stay
  within the account's own rate budget, keep it low frequency, and alert on failure. See
  GEO plan §6 for the shape and the red lines.
