# WeChat Official Accounts

Write articles to the draft box, publish them, and obtain permanent links.
**The only primary domestic channel combining an established API, support for financial content, and sufficient reach.**

## Three prerequisites

1. **The account must be a verified service or subscription account.** Since July 2025, WeChat has removed publishing API access
   from accounts owned by individuals and unverified business accounts. Those accounts receive 48001.
2. **The calling server's public outbound IP must be allowlisted**, or every endpoint returns 40164.
   Configure it under **Settings & Development → Basic Configuration → IP Allowlist**.
   This is easy to overlook in containers: run `curl ifconfig.me` inside the container to check its actual outbound IP rather than guessing.
3. **Published articles do not enter the historical message feed.** `freepublish` produces permanent links suited to keeping an article
   available and directing readers from elsewhere. **It is not a mass broadcast.** Broadcasting uses separate endpoints and limits,
   including a restricted number of monthly sends.

## Setup

In the account console, obtain **AppID** and **AppSecret** under **Settings & Development → Basic Configuration** and enter them
in the credential. The secret is shown only when generated; if lost, it must be reset, immediately invalidating the old one.
Add the server's outbound IP to the allowlist as well.

Click **Test** to obtain a token and read the draft count. This **checks three things at once**:
whether the secret is correct, whether the IP is allowlisted, and whether the account has publishing permission.

> ⚠️ **Use a single replica.** The access_token is globally unique. Obtaining another for the same AppID immediately invalidates the previous one.
> Multiple replicas invalidate each other's tokens, causing intermittent 40001 errors.
> The plugin retries once automatically, but that is not a solution for multiple replicas.

## Example workflow

```
[Cover image] → [Upload image: cover] ────────┐
                                             ├→ [Create draft] → [Human review: optional] → [Publish draft] → [Article link]
[Body images] → [Upload image: article body] ─┘
```

**Why draft creation and publishing are separate:** the draft can be inspected in the account console before publication,
a useful review point for financial content. Whether to add a human-review node is your choice;
connect the two steps directly for a fully automated flow.

## Three pitfalls the plugin checks before publication

**A cover image is required**, and it must be the media_id of a permanent asset.
Use **Upload image** with the purpose set to **Cover**. Without it, WeChat returns the unclear error 41005.

**Externally hosted images in the article body will not display**, due to WeChat's hotlink protection.
First use **Upload image** with **Article body image** to obtain an `mp.weixin.qq.com` URL,
then put that URL in the HTML `<img src>` attribute. Before creating a draft, the plugin scans the body and rejects external images,
preventing publication of an article whose images are missing.

**Publishing is asynchronous.** A successful `submit` means only that the task was accepted;
it may still fail during an originality declaration or platform review.
By default, **Publish draft waits until publication finishes** before returning,
with readable status names such as success, publishing, original_failed, audit_failed, and banned.

## Supported article HTML

Basic HTML is supported: p, h1-h6, strong, em, ul, ol, blockquote, img, a, and table.
Scripts, iframes, and external stylesheets are **not supported**; some inline styles work.
For complex layouts, prepare the design elsewhere and convert it to this restricted HTML.

## Limits

| Item | Limit |
|---|---|
| Title | 64 characters |
| Summary | 120 characters; if blank, WeChat takes the first 54 characters of the body |
| Cover image | 10 MB |
| Article body image | **1 MB**, a different limit from cover images |
| API calls | Daily quotas vary by account; exceeding them returns 45009 |

## Common error codes

| Code | Meaning |
|---|---|
| 40164 | IP is not allowlisted—the most common error |
| 48001 | The account lacks access to the endpoint, commonly because it is unverified or individually owned |
| 40001 | AppSecret is incorrect, or another replica invalidated the token |
| 45009 | API call rate limit reached |
| 53405 | Noncompliant content; financial articles should pay particular attention to stock recommendations and promises of returns |
