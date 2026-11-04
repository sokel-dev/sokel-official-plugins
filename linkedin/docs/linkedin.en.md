# LinkedIn (personal posts)

Publish posts as the authorized account's **individual member identity**. Self-service access requires **no partner approval**
and is useful for sharing investment research with a professional audience.

> This plugin **publishes personal posts only, not company-page posts**. Company pages require `w_organization_social`
> and approval through a partner program, which can take weeks or months. Combining the two would make it difficult
> to explain why a user can publish personally but cannot post to a company page.

## Setup

1. Create an app in the [LinkedIn developer console](https://www.linkedin.com/developers/apps).
   You must associate it with a company page to create the app, but this does not affect personal posting.
2. Under **Products**, add two self-service products: `Sign In with LinkedIn using OpenID Connect`
   and **`Share on LinkedIn`**. Both can be enabled directly without approval.
3. On the Auth page, set the callback to the platform's URL:
   `https://<your-platform-domain>/api/v1/credentials/oauth/callback`.
   It must **exactly match** the platform environment variable `LINKEDIN_OAUTH_REDIRECT_URL`.
4. Have the platform administrator configure:
   ```
   LINKEDIN_OAUTH_CLIENT_ID / LINKEDIN_OAUTH_CLIENT_SECRET
   LINKEDIN_OAUTH_REDIRECT_URL / LINKEDIN_OAUTH_PROXY (if connectivity requires it)
   ```
5. Create a credential and click **Authorize**. The requested scopes are `openid profile w_member_social`.

## ⚠️ Tokens expire after 60 days

LinkedIn access tokens **last only 60 days**, and **most self-service apps cannot obtain refresh tokens**.
Refresh tokens require a separate Marketing Developer Platform application. When the token expires, someone must click **Authorize** again.

Failure is not silent: **Check credentials** marks an expired credential as invalid.
This triggers a **Credential invalid** alert, which can start a workflow that asks you to reauthorize.

## Operations

| Operation | Description |
|---|---|
| Publish post | Text, optional images, and visibility. Returns `id` (a URN) and `url` |
| Delete post | Deletes only your own posts; accepts a URN or post link |
| Check credentials | Checks whether the token is still valid and returns the account name |

## Writing post text

Use **plain text and line breaks; Markdown and HTML are not supported**. Writing `**bold**` displays all four asterisks.
The limit is 3,000 characters.

LinkedIn automatically recognizes links in the text and creates cards. **You do not need to build them yourself**; include the URL.

LinkedIn mentions require URNs rather than @names. This plugin does not support mentions; create the post manually if you need them.

## Images

Up to nine images are supported, each ≤10 MB. One image creates a single-image post; multiple images create a multi-image post automatically.
Alt text corresponds to images in order. Providing fewer alt-text entries does not prevent publication.

## Limits

| Item | Limit |
|---|---|
| Text | 3,000 characters |
| Images | Nine, each 10 MB |
| Rate | **150 calls per person per day**, and 100,000 calls per app per day |

## Troubleshooting

**401**: the 60-day token has probably expired. Authorize again.

**403**: authorization lacks `w_member_social`, or the app does not have the **Share on LinkedIn** product.
After changing the product configuration, **authorize again**.

**426**: LinkedIn has retired the plugin's pinned `LinkedIn-Version`, currently 202601, and the plugin needs an update.
LinkedIn releases a version each month and retires older versions after roughly a year. Pinning is intentional so upgrades are explicit.

**Cannot connect**: linkedin.com is hosted outside mainland China. Configure an outbound proxy in the **credential**;
for a container, use `http://host.docker.internal:7897`.
