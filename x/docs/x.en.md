# X (Twitter)

Publish posts and threads, upload images and videos, like and repost, search and read timelines, send direct messages, manage lists, and **trigger workflows for mentions or keyword matches**.

## Read this first: X API calls cost money

From February 2026, X removed its free tier and switched to **pay-per-use billing**, deducting each call from prepaid credits:

| Action | Price stated by the provider |
|---|---|
| Read one post | About $0.005 |
| Publish one post | About $0.015, or about $0.20 when the body contains a link |
| Look up one user | About $0.010 |

Therefore:

- **Do not casually increase result limits** on read operations. Reading 100 posts in one call costs about half a dollar.
- Event polling defaults to 300 seconds, with a minimum of 60 seconds. Keyword monitoring performs a billed search on every round.
- HTTP 402, indicating insufficient credits, is reported with an understandable explanation.

## Configuration: create an app, configure the platform, authorize

**1. Create an app at [developer.x.com](https://developer.x.com).**

- Enable **OAuth 2.0** in User authentication settings.
- Set **Type of App to Web App / Automated App**, a confidential client that receives a Client Secret.
- Set **App permissions to Read and write**, or Read and write and Direct message if you need direct messages.
- Set Callback URI to the platform's callback URL, **exactly matching `X_OAUTH_REDIRECT_URL` below**:
  `https://<your-platform-domain>/api/v1/credentials/oauth/callback`

> ⚠️ After changing App permissions, **you must authorize again**. Changing settings alone does not update existing tokens. Otherwise all write operations may return 403, and X's own message will not identify this cause.

**2. Configure the platform.** An administrator does this once for all workspaces.

```
X_OAUTH_CLIENT_ID=...
X_OAUTH_CLIENT_SECRET=...
X_OAUTH_REDIRECT_URL=https://<platform-domain>/api/v1/credentials/oauth/callback
X_OAUTH_PROXY=http://127.0.0.1:7897   # Required if the platform cannot reach x.com
```

**3. Create a credential and click “Authorize.”** On X's consent page, click Authorize app.

Authorization requests all of these scopes together. X does not support incremental authorization, so omitting one would require everyone to authorize again:
`tweet.read tweet.write users.read like.* bookmark.* follows.* list.* dm.* media.write offline.access`.

`offline.access` is essential. Without it, no refresh token is issued, and the access token expires after two hours without automatic recovery.

**4. If the plugin process cannot reach x.com**, set “Outbound proxy” in the credential. Configure it per credential rather than relying on process-wide `HTTP_PROXY`, which is global and can also misroute internal requests.

## Operations

| Group | Operation | Description |
|---|---|---|
| Publishing | Publish post | Original posts, replies, quotes, polls, or images; returns the ID and link |
| | Publish thread | Automatically link multiple text segments; the first segment may include images |
| | Delete post | Only posts published by your account can be deleted |
| Media | Upload media | Chunked upload, wait for processing, and set alt text; returns `media_id` |
| Interaction | Like / Unlike | |
| | Repost / Undo repost | To add commentary, use the quote option in Publish post |
| | Bookmark / Remove bookmark | Bookmarks are private |
| | Follow / Unfollow | A username can be used, at the cost of an additional lookup |
| Reading | Search posts | Last 7 days, using X search syntax |
| | User timeline | Posts published by an account |
| | My mentions | Posts mentioning the authorized account |
| | List timeline | **The efficient way to monitor multiple accounts**, described below |
| | Get posts | Batch retrieval by ID, up to 100 per call |
| | Look up user | By username or ID, or retrieve the authorized account itself |
| Direct messages | Send / Get direct messages | Sending is refused if the recipient neither follows you nor accepts messages from everyone |
| Lists | Add member / Remove member | Use with List timeline |

### Incremental retrieval: store the cursor in a data table

The four list-style read operations—search, user timeline, mentions, and list timeline—use the **incremental-stream shape**, matching other incremental plugins for announcements and regulatory filings:

```
[Scheduled trigger] → [Data table: read cursor] → [Search posts] → [Your downstream steps] → [Data table: save cursor]
```

Pass `cursor`, a post ID, to retrieve only newer posts. Results include `next_cursor` and `has_more`.
When `has_more` is true, the batch reached its limit: **fetch again immediately** instead of waiting for the next scheduled run.

When there is no new content, `next_cursor` returns the input value unchanged, preserving progress.

> Why not use X's own `next_token`? It expires after an hour and is valid only within one search session. Saving it in a data table for a later run will fail.

### Monitoring multiple accounts: use a list, not individual timelines

Add the accounts to an X list; Add list member can automate this step. Then call List timeline once. Monitoring 30 accounts takes 1 request instead of 30. With pay-per-use billing, this affects the bill as well as the time spent waiting.

### Reposts and replies

**Many search results are reposts.** Every post includes `kind`: `original`, `replied_to`, `retweeted`, or `quoted`. The plugin derives this from `referenced_tweets`; X does not provide it directly.
To retrieve only original posts, add `-is:retweet` to the query or filter by `kind` downstream.

### Publishing threads

Publish thread posts each text segment in sequence, with each post replying to the previous one. **It does not split text for you**. One item is one post, and X rejects an item exceeding its length limit.

A failure partway through **does not roll back earlier posts**. They are already on the public timeline, and deleting them would be a second destructive action. The error includes the IDs already published, and output `ids` also contains the posts actually sent.

### Media

Upload media → receive `media_id` → bind it to the media IDs input of Publish post. Videos are uploaded in chunks, and the operation waits until X finishes processing them. Publishing immediately after FINALIZE can otherwise produce “media not found,” which looks like an incorrect ID but actually means processing is incomplete.

Choose the correct purpose: posts or direct messages. X validates `media_category`, and a mismatch may only be rejected when publishing.

## Events: Mentioned / Keyword match

Configure these in the **credential**, because the event source is a persistent process without node-level configuration:

| Field | Description |
|---|---|
| Monitor mentions | Set to `on` to trigger when someone mentions the authorized account |
| Query to monitor | X search syntax, such as `"my brand" -is:retweet lang:zh`; empty disables monitoring |
| Polling interval (seconds) | Default 300; minimum 60 |

Both events, Mentioned and Keyword match, share `post_id` and `text`. The platform flattens them into the top level of the trigger input, so replying with Publish post needs only one variable binding.

**No historical replay on first start.** Connecting an account with ten years of history could otherwise overwhelm the workflow with thousands of mentions. The first round only records the current position; subsequent items trigger events.

> Why polling instead of a real-time push? X's filtered stream and Account Activity API are available only on the Enterprise tier, starting at $42,000 per month; self-service tiers cannot access them.

## Common problems

**Unexplained 403**: check these three causes, which the plugin also includes in its error messages:
1. App permissions are not Read and write, or permissions changed without reauthorization.
2. Authorization omitted a required scope, such as `dm.write` for direct messages.
3. The operation requires a higher tier: **quote reposting** and **full-archive search** are Enterprise-only.

**429 rate limit**: the error states when the limit resets. X's `x-rate-limit-reset` is an absolute timestamp. Search and mentions allow 300 requests per 15 minutes; publishing allows 100 posts per 15 minutes.

**Cannot find content older than 7 days**: self-service search covers only the last 7 days. This is X's limit, not the plugin's.

**Cannot send a direct message**: X refuses it when the recipient does not follow you and has not enabled messages from everyone.
