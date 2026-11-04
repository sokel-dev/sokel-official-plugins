# Threads (Meta)

Publish posts and threads. Free, with no per-call charges, and one of the largest audiences among these international channels.

## Setup

1. Create an app in the [Meta developer console](https://developers.facebook.com/apps) and add the **Threads API** product.
2. In Threads API settings, enter the platform callback URL:
   `https://<your-platform-domain>/api/v1/credentials/oauth/callback`.
   It must **exactly match** the `THREADS_OAUTH_REDIRECT_URL` environment variable.
3. Have the platform administrator configure:
   ```
   THREADS_OAUTH_CLIENT_ID / THREADS_OAUTH_CLIENT_SECRET
   THREADS_OAUTH_REDIRECT_URL / THREADS_OAUTH_PROXY (if connectivity requires it)
   ```
4. Create a credential and click **Authorize**. The scopes are `threads_basic threads_content_publish`.
5. Click **Test**. It checks the account and **also reports how many posts remain in the 24-hour allowance**.

> Authorization involves two exchanges: the short-lived token obtained with the authorization code lasts only one hour,
> so it must be exchanged for a 60-day token. The platform handles this step.
> **After those 60 days, someone must authorize again**; Threads does not provide refresh tokens.

## Operations

| Operation | Description |
|---|---|
| Publish post | Text, optional media, a reply target, and reply permissions. Returns `id` and `url` |
| Publish thread | Joins text segments into a thread; the first post can include media |
| Check credentials | Checks token validity and **remaining publishing quota** |

## Media is fetched by Threads, not uploaded

Threads does not accept uploads here; it **downloads media itself** from the URLs supplied. Therefore:

- **Bind a platform file directly**. When passing it to the plugin, the platform converts it to a publicly reachable signed download URL;
- Alternatively, provide a public URL from an external media host in **Media URL**;
- Multiple media items automatically become a **carousel**, with up to 20 items;
- Images use JPEG/PNG and video uses MP4. **If Threads cannot download the media or the format is invalid**,
  the plugin reports the reason returned by Threads instead of a generic publishing failure.

For posts with media, the plugin **waits for Meta to finish processing before publishing**.
Without that wait, publication can fail unpredictably and then succeed on retry, making the problem difficult to diagnose.

## Limits

| Item | Limit |
|---|---|
| Text | 500 characters. The plugin rejects longer text before sending and suggests Publish thread |
| Media | 20 items per post |
| Publishing | **250 posts per rolling 24-hour window**, per account |

**Check credentials** returns both used and total quota so workflows can decide whether to publish.

## Troubleshooting

**401 / code 190**: the 60-day token has probably expired. Authorize again.

**Permission denied**: authorization lacks `threads_content_publish`, or the account is not a Threads professional/creator account.

**Media processing failed**: Threads cannot download the URL because it is private, requires login, or has an expired signature;
or the format is unsupported. The error includes Meta's stated reason.

**Cannot connect**: graph.threads.net is hosted outside mainland China. Configure an outbound proxy in the **credential**;
for a container, use `http://host.docker.internal:7897`.
