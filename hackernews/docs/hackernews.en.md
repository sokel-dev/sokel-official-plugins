# Hacker News

Read rankings, stories, and comments, search by keyword, and **trigger workflows when someone comments on your story or replies to your comment**. No registration or payment is required.

**This plugin cannot submit stories or post replies.** Hacker News has no write API, and automated posting also violates its rules and can get an account banned. The plugin helps you keep up with discussions: a new comment sends its content and a direct link to your workflow, and you follow that link to reply on the HN website.

## Configuration

Reading HN data requires no login. Credentials specify **what to monitor**: event sources are persistent processes without node-level configuration, so they read their settings from credentials.
If you only use operations and do not need events, create an empty credential.

| Field | Description |
|---|---|
| HN username to monitor | Your HN account, case-sensitive. New comments on its recent stories or direct replies to its comments trigger “New comment” |
| Additional stories to monitor | Story IDs or URLs, separated by commas: for example, a story someone else submitted for you or a discussion you want to follow |
| Keywords to monitor | New stories or comments containing a keyword trigger “Keyword match”: a project name, `github.com/your/repository`, or your website domain |
| Days of stories to track | Default 14. Stories from two weeks ago may still receive comments; older stories are usually inactive |
| Polling interval (seconds) | Default 120; minimum 60 |
| Outbound proxy | Required if your deployment cannot reach news.ycombinator.com, for example when deployed in mainland China. Example: `http://127.0.0.1:7897` |

After configuration, click “Test” to check connectivity to both APIs and verify that the username exists.

## Events

**New comment** fires in any of these three cases, once per comment:
- Someone comments on one of your recent stories, whether directly on the story or in reply to someone else's comment within it.
- Someone comments on an additional monitored story.
- Someone directly replies to your comment on another person's story.

Your own comments do not trigger events.

| Field | Description |
|---|---|
| `comment_id` | Comment ID |
| `text` | Plain-text body with HTML removed; links retain their full URLs |
| `text_html` | Original HTML returned by HN, unchanged |
| `author` / `author_url` | Comment author and their profile |
| `url` | Comment URL; **follow this link to reply** |
| `story_id` / `story_title` / `story_url` | The story containing the comment |
| `parent_id` | The item being replied to: equal to `story_id` for a direct reply to the story, otherwise a comment ID |
| `is_reply_to_me` | True for a direct reply to a story or comment you wrote; useful for prioritization |
| `created_at` | Publication time in RFC3339, UTC |

**Keyword match** fires when a new story or comment contains a monitored keyword. Its fields are similar to those above, plus `kind` (story / comment), `item_id`, `title` (story title), `link` (external link for a link story), `points` (story score), and `matched_query`.

**Delivery delay** is one polling interval plus the HN search API's indexing delay, usually less than a minute. First-time activation does not replay historical comments; monitoring starts from activation time.

### Common workflows

- Write comments to a data table, deduplicating by `comment_id`, and build a list of comments awaiting replies in an app.
- Turn comments with `is_reply_to_me` set into to-dos and include `url`.
- Ask an LLM to draft a reply, then paste it into HN yourself.

## Operations

| Operation | Description |
|---|---|
| Get item | Read a story or comment by ID or by pasting its HN URL: score, comment count, and body |
| Get ranking | Retrieve up to 100 items from the front page, newest, best, Ask HN, Show HN, or jobs lists |
| Search | Search stories and comments by keyword, newest first; optionally restrict results to the last few hours |
| Get comments | Retrieve all comments under a story, flattened in discussion order with nesting depth; optionally return only comments after a specified time |
| Check connection | Check connectivity to both APIs and whether the monitored username exists |

## Limitations

- No story submission, replies, or voting: HN has no write API.
- Comment scores are not public; `score` is always 0 for comments.
- Deleted comments and comments marked dead are omitted.
