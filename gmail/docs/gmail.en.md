# Gmail

Read and write Gmail: list, read, and send email. The plugin can also act as an **event source**, triggering workflows when new email arrives.

## Authorization: no manual keys

This plugin uses **collaborative OAuth authorization**. You do not need an API key:

1. Create a Gmail credential under **Credentials**. **Leave all three fields blank**; the platform fills them automatically.
2. Save it and click **Authorize** on that credential row. The browser opens Google's consent page.
3. Sign in with the Gmail account you want to connect and grant access. The page closes automatically, and the credential becomes authorized.

The platform maintains `access_token`, `refresh_token`, and `history_id`. **Entering them manually can break the configuration.**
Expired tokens are refreshed automatically; you do not need to reauthorize periodically.

The credential row's **Check** action reads the account profile and returns whether authorization is still valid and which mailbox is connected.
Authorizing a different Google account is a common mistake; the returned email address makes it easy to spot.

## Deployments in mainland China

Google's authorization and API services are hosted outside mainland China. If the platform cannot reach them, authorization may
**silently stall in the popup**, a problem encountered in practice. Configure `GOOGLE_OAUTH_PROXY` for the platform deployment;
see the deployment documentation.

## New-email events

- Event sources are partitioned by credential: **one credential = one Gmail account = one polling loop**. Create separate credentials for multiple accounts.
- The event source maintains its own `history_id` cursor. Its first startup begins at the current point and does not replay historical email.
- Events contain metadata and a body excerpt only. To obtain attachments, use **Read email** downstream with the `message_id`.

## Notes

- Outgoing email is always sent from **the authorized account**; you cannot spoof the From address.
- **List email** uses Gmail's query syntax, such as `is:unread from:boss@x.com newer_than:7d`,
  exactly as in Gmail's web search box.
