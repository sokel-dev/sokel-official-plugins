# Reddit

Search posts, read subreddit and user posts, and read comments. **Trigger workflows when your posts receive new comments
or when a keyword you care about appears.** No registration or payment is required: the plugin uses Reddit's public `.rss` feeds, not its API.

**It cannot publish posts or replies.** Since November 2025, Reddit no longer grants API apps to individuals without an approval process
that is very difficult to pass. Without a key, write endpoints and the inbox are unavailable. This plugin helps you keep up with discussions
under your posts: when a comment arrives, it passes the content and a direct link to your workflow. Follow that link to reply on Reddit's website.
It **cannot see replies to your comments on other people's posts**.

## Configuration

There are no secrets in the credential. You specify only **what to watch**, because the continuously running event source reads its configuration
from the credential. If you need only operations, without events, create an empty credential.

| Field | Description |
|---|---|
| Reddit username to watch | Your account name without `u/`. New comments under its recent posts trigger New comment |
| Additional posts to watch | Comma-separated post URLs or IDs, for posts someone else published for you or discussions you want to follow |
| Keywords to watch | New posts matching a keyword trigger Keyword match; examples include a project name, `github.com/your/repository`, or a website domain |
| Restrict keywords to these subreddits | Join names with `+`, such as `golang+selfhosted`; blank means all of Reddit |
| Post age to track, in days | Defaults to 14. Each round watches at most five posts, newest first |
| Polling interval, in seconds | Defaults to 600, with a minimum of 300; measured from the end of the previous round |
| Outbound proxy | Required when the deployment cannot reach reddit.com, for example `http://127.0.0.1:7897` |

Click **Test** to fetch the watched user's posts once. A nonexistent username produces an error.

## Quota: one request per minute, with a risk of IP blocking

For unauthenticated access, Reddit allows **one feed request per outbound IP per minute**.
Observed behavior was an initial 200, followed by 429 responses, and another allowed request after 60 seconds.
The plugin queues requests internally and follows this cadence strictly, so:

- Each operation may wait up to a minute before returning;
- An event-source round takes “number of watched posts + 2” minutes: one user-post listing, one request per post, and one keyword search;
- Replicas sharing the same outbound IP would compete for that one request. Avoid deploying this way.

Testing also found that **too many 429 responses in a short time can cause Reddit to terminate that IP's connections at the TLS layer**.
Even curl cannot connect in that case; it is not a plugin failure. Datacenter IPs are especially susceptible.
The plugin already queues requests at the required pace, so normal use should not hit 429.
If health checks begin reporting that Reddit is unreachable while other sites still work, the IP has been blocked;
change the outbound IP or wait for the block to be lifted.

## Event: new comment

Triggers when a watched post receives a new comment, once per comment. Your own comments do not trigger it.

| Field | Description |
|---|---|
| `comment_id` | The full comment name, `t1_…` |
| `text` / `text_html` | Body as plain text and Reddit-rendered HTML |
| `author` / `author_url` | Comment author |
| `url` | Comment URL; **reply there** |
| `subreddit` | Subreddit |
| `post_id` / `post_title` / `post_url` | The parent post |
| `created_at` | Timestamp |

The first activation does not replay historical comments; tracking begins at that moment.
A post's feed returns at most 25 comments in Reddit's default order, which is not newest-first.
Popular posts with many comments may therefore hide newer comments farther down the order.

## Event: keyword match

Triggers once for each new post containing a watched keyword. Fields are `name` (`t3_…`), `title`, `text` / `text_html`,
`author` / `author_url`, `url`, `subreddit`, `created_at`, and `matched_query`.
Changing the keyword starts tracking again from that moment without replaying historical matches.
Only posts are searched; Reddit's feeds do not offer comment search.

## Operations

| Operation | Description |
|---|---|
| Search posts | Search all of Reddit or selected subreddits, sort by newest, relevance, top score, or most comments, and restrict the time range |
| Latest subreddit posts | The latest 25 posts from one or more subreddits, such as `golang+selfhosted` |
| Get post comments | The post itself and up to 25 comments |
| User posts | A user's latest 25 posts |
| Check configuration | Connect to Reddit and verify the watched username |

Each operation makes one feed request and is subject to the quota described above.

## Limitations

- No publishing, replying, voting, or inbox access; these require a key.
- Replies to your comments on other people's posts are not visible. Only comments under your own posts and explicitly watched posts are available.
- Feeds do not provide numeric scores or comment counts.
