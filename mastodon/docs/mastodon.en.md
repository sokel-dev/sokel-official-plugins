# Mastodon

Publish posts and threads, and delete posts. **Free, with no approval process or per-call charges**.
As a federated network, Mastodon lets you choose an instance that suits your audience, including those used by Chinese-language
financial communities, or host your own.

## Setup: three minutes

1. On your instance, open **Preferences → Development → New Application**;
2. Select at least `write:statuses` for posting, `write:media` for media uploads, and `read:accounts` for health checks;
3. Open the new application's details and copy **Your access token**;
4. Create a platform credential with the instance URL, such as `https://mastodon.social`, and access token.
   Add an outbound proxy if necessary;
5. Click **Test**. It verifies the token with the instance and succeeds only if that verification works.

> After changing scopes, **you must generate a new token**. Changing the settings does not update tokens already issued.
> A 403 when posting is a typical symptom.

## Operations

| Operation | Description |
|---|---|
| Publish post | Text, content warning, visibility, images/video, and a poll. Returns `id` and `url` |
| Publish thread | Joins text segments into a thread; the entire thread inherits the content warning and visibility |
| Delete post | Deletes only your own posts; accepts an ID or post link |
| Check credentials | Checks whether the token is still valid and has sufficient scopes |

## Differences from other platforms

**The character limit is not always 500.** Each Mastodon instance configures its own limit: mastodon.social uses 500,
while many Chinese-language instances use 5,000 and some allow 11,000. The plugin queries `/api/v2/instance` and caches the result,
so it supports the same post length as that instance's web interface.

**Content warnings (CW).** Federated communities often place sensitive or long content behind a CW, including financial commentary.
When **Content warning** is filled in, the body is collapsed and the warning text appears first.

**Four visibility levels**: public, which appears on public timelines; unlisted, which is visible but excluded from public timelines;
followers only; and direct. Later posts in a thread **inherit the first post's setting**, so visibility does not change within a thread.

**Retries do not duplicate posts.** Publishing includes an Idempotency-Key, and the same key produces only one post within an hour.
A workflow rerun after a network interruption therefore does not leave two identical posts on the timeline.

**Video requires transcoding.** Images are processed synchronously, while video and audio are processed asynchronously by the instance.
The plugin waits for processing to finish before publishing. Without that wait, the post would be rejected with 422 and only a
“media unavailable” error.

## Troubleshooting

**403**: the application probably lacks required scopes: `write:statuses` for posts and `write:media` for uploads.
Remember to generate a new token after changing scopes.

**422**: common causes include exceeding the text limit, invalid poll options, or supplying both media and a poll, which Mastodon does not allow.

**429**: rate limits are configured per instance and can be tight on smaller instances. Reduce the posting frequency.

**Cannot connect**: deployments in mainland China may need an outbound proxy in the **credential** to reach overseas instances.
In a container, use `http://host.docker.internal:7897` rather than `127.0.0.1`.
