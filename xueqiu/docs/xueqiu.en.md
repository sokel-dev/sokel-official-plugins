# Xueqiu (Unofficial API)

Publish posts, optionally with images, and check credentials.

> ⚠️ **Xueqiu has no official publishing API.** This plugin uses the endpoints used by its web client, which constitutes unauthorized automation. Endpoints can change without notice and cause publishing failures; accounts may be rate-limited or banned. Use it only for **your own authentic content**, within **your own account's allowance**, at **low frequency**, and configure alerts for expired credentials.

## Configuration

1. Sign in to Xueqiu in a browser.
2. Open F12 → Network, select a request, copy the **entire Cookie header**, not a single key, and paste it into the credential's “Cookie” field.
3. Initially leave the other three fields empty:
   - **Session token**: the plugin retrieves it from the page. If retrieval fails, the error explains where to copy it manually.
   - **Risk-control parameter**: try without it first. If blocked, copy the `md5__1038=` value from the browser request URL.
   - **User-Agent**: empty uses a common desktop Chrome UA. Matching the browser used to obtain the cookie is more reliable.
4. Click “Test.” It checks login status through an endpoint **without side effects** and does not publish anything to your timeline.

**Cookies expire.** Set up a workflow: scheduled “Check credential” → on expiration, notify and create a human to-do to sign in again → write the new Cookie back with “Update credential.” The platform already supports this flow; first enable “Allow workflows to update” on the credential.

## Two publishing approaches: short posts and long articles

| | Publish a short post | **Write an article (save draft)** |
|---|---|---|
| Destination | Timeline, for a short opinion | Articles, the main format for investment research |
| Result | **Published immediately** | **Draft**: click Publish on the website for the final step |
| Requirements | `session_token`, retrieved automatically or pasted once if retrieval fails | **No additional requirements**, making it easier to use |
| Risk-control parameter | May be required | Not observed |

**Use articles for greater reliability.** Their API is simpler, and automatically preparing a draft for human publication is also a more conservative compliance approach.

## Publishing posts

Provide **plain text or simple Markdown**. The plugin converts it to Xueqiu HTML:

- Blank lines and line breaks become `<p>` paragraphs.
- `**bold**` becomes `<b>`.
- URLs in the body are wrapped in clickable `<a>` links.
- Existing HTML containing `<p>`, `<div>`, or similar elements is **passed through unchanged**, without a second conversion.
- Images are uploaded to Xueqiu's image host, then embedded at the end using its required structure.

### AI disclosure (`ai_disclose`)

Xueqiu supports this post parameter. **Enable it whenever an LLM has rewritten or generated the content.** This is the service's compliance requirement, so the plugin exposes an explicit switch instead of hard-coding 0.

## Common problems

**“Cookie expired”**: sign in again and update the Cookie in the credential.

**“Blocked by risk controls”**, with an HTML page returned instead of JSON: copy the entire `md5__1038=` value from the browser request URL into the credential's “Risk-control parameter” field and retry.

**“Could not obtain session_token”**: publish one post manually in your browser, open F12 → Network → `update.json`, copy `session_token` from the request body, and paste it into the matching credential field.

**“Published, but the post ID was not recognized” / “Image URL was not recognized”**: Xueqiu changed its response structure. Share the response with the developer so the extraction can be adjusted; the plugin uses tolerant extraction rather than one fixed key name.

## Not implemented

- **One-click article publication**: `save.json` saves a draft. Publishing is intentionally a manual website step.
- **Comments or reposts**.
- **Deleting posts**: delete a mistaken post on the website instead of adding an operation that could accidentally delete content.
