# Product Hunt

Read products, comments, and daily rankings, and **trigger workflows when your products receive new comments or replies**.

**This plugin cannot launch products or reply to comments.** Product Hunt's API is read-only for both actions. Use the website on launch day, and follow an event's `url` to reply. The plugin keeps you informed when someone comments.

## Configuration

1. Open <https://api.producthunt.com/v2/oauth/applications> and create an application. Any redirect URI is sufficient, such as `https://localhost`.
2. Find the **Developer Token** at the bottom of the application page. It does not expire and represents your own account.
3. Create a credential for this plugin in the platform:

| Field | Description |
|---|---|
| Developer token | The token copied in the previous step |
| Outbound proxy | Required if your deployment cannot reach producthunt.com, for example when deployed in mainland China. Example: `http://127.0.0.1:7897` |
| Products to monitor | Product slugs or product-page URLs, separated by commas |
| Monitor products I made | On by default: also monitor the 10 most recent products where the token's account is a maker |
| Polling interval (seconds) | Default 180; minimum 60 |

Click “Test” to read the current account using the token.

## Event: New comment

Fires when a monitored product receives a new comment or reply, once per comment. Your own comments do not trigger events.

| Field | Description |
|---|---|
| `comment_id` / `body` | Comment ID and body |
| `url` | Comment URL; **reply there** |
| `parent_id` | The comment being replied to; empty for top-level comments |
| `is_reply_to_me` | Whether the comment replies to one you wrote |
| `username` | Comment author. **Usually empty**: Product Hunt hides author identities from the API and only indicates whether a comment is yours. Open `url` to see the author on the website |
| `post_id` / `post_name` / `post_url` | The product receiving the comment |
| `votes_count` | Votes on this comment |
| `created_at` | Timestamp |

First-time activation does not replay historical comments; monitoring starts at activation time. Each polling round retrieves comments newest first, up to 3 pages per product: 60 top-level comments, each with its 5 most recent replies. Comments beyond that volume within one round may be missed. Shorten the polling interval if launch-day discussions are especially active.

## Operations

| Operation | Description |
|---|---|
| Get product | Votes, comment count, featured time, and makers |
| Get comments | Comments and replies, newest first. Defaults to 20 top-level comments, maximum 50. Product Hunt returns 20 per page; additional pages are fetched as needed. `is_mine` identifies your own comments |
| Get ranking | Products launched on a given day, using Pacific Time day boundaries, ordered by rank, votes, or newest |
| Check credential | Read the current account |

## Limitations

- No launching products, commenting, or voting: the API does not expose these write operations.
- The API hides comment-author identities: `username` and similar fields are usually empty; the indication of whether a comment is yours is reliable.
- Product Hunt applies query-complexity limits over 15-minute windows. Normal polling intervals stay within that allowance.
