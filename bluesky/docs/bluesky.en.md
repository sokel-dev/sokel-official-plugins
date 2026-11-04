# Bluesky

Publish posts and threads, and delete posts. **Free, with no approval process or per-call charges**—one of the easiest public social platforms to connect.

## Setup: two minutes

1. In Bluesky, open **Settings → Privacy and Security → App Passwords** and generate an app password
   (in the form `xxxx-xxxx-xxxx-xxxx`).
2. Create a credential for this plugin on the platform:
   - **Account**: your full handle, such as `acme.bsky.social`, or your registered email address;
   - **App password**: the value generated above;
   - **Service URL**: leave blank unless you use a self-hosted PDS;
   - **Outbound proxy**: fill this in if the deployment cannot reach bsky.social, for example `http://127.0.0.1:7897`.
3. Click **Test** to verify it. The check actually exchanges the credential for a session and succeeds only if that works.

> **Do not enter your login password.** App passwords can be revoked individually and cannot change your password or delete your account.

## Operations

| Operation | Description |
|---|---|
| Publish post | Text plus optional images, link card, or reply. Returns `uri` (an at:// address), `cid`, and `url` (the web link) |
| Publish thread | Joins multiple text segments into a thread; the first post can include images |
| Delete post | Deletes only your own posts; a bsky.app link can be pasted directly |
| Check credentials | Checks whether the app password is still valid |

## Links, hashtags, and mentions become clickable automatically

The plugin handles this for you, but the detail matters: **Bluesky does not automatically recognize URLs in post text**.
It requires explicit rich-text annotations, called facets, whose indices are **UTF-8 byte offsets**.
Counting characters in a post that mixes Chinese and English shifts those annotations and breaks the links.
This is a common trap when constructing API requests yourself.

Three types are supported:

- **Links**: `https://…`; trailing Chinese punctuation is removed automatically (the full stop in “See https://x.com/a。” is not part of the link);
- **Hashtags**: `#A股`, including Chinese text;
- **Mentions**: `@acme.bsky.social`, using the full handle. Unrecognized mentions remain plain text and do not cause publishing to fail.

## Link cards

When a link is provided, the plugin fetches the target page's title, summary, and thumbnail to build a card.
Financial updates are often a sentence plus a link; without a card, the timeline shows only a bare URL.

- By default, it uses **the first link in the post text**;
- Enter another address in **Link card** to use that address instead;
- Enter `-` (a hyphen) to disable the card.

If the page cannot be fetched because of crawler blocking or a timeout, **publishing still proceeds** with a plain-text link.

## Limits imposed by Bluesky

| Item | Limit |
|---|---|
| Text | 300 graphemes; each Chinese character counts as one. The plugin rejects longer text **before sending** |
| Images | Four per post, each ≤2 MB |
| Video | Not yet supported by this plugin |
| Rate | 5,000 points per account per hour and 35,000 per day; creating a post costs three points, or approximately 1,666 posts per hour |

## Troubleshooting

**“The app password may have been revoked”**: generate a new one in Settings and update the credential.
Changing the account password invalidates all app passwords.

**A thread stops partway through**: posts already published **are not rolled back**. They are already on the public timeline,
and deleting them would be another destructive action. The error message and the `uris` output contain the published addresses;
you can continue the thread or finish it manually.

**A reply does not attach to the thread**: a reply needs both a root and a parent, which the plugin retrieves for you.
If you construct an at:// address manually, verify that the root is the thread's first post, not the post being replied to.

**Cannot connect**: bsky.social is hosted outside mainland China. Configure an outbound proxy in the **credential**;
proxy settings apply to that credential and do not affect other outbound traffic. In a container, use
`http://host.docker.internal:7897` rather than `127.0.0.1`.
