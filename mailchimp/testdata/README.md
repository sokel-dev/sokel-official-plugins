# Fixture provenance

Reviewed against official documentation on 2026-11-04. These are published documentation examples, not captured account responses:

- `ping.official.json`: complete response example from [Send your first API request](https://mailchimp.com/developer/marketing/build/start-developing/send-your-first-api-request).
- `members.official.json`: complete shortened response example from [Import and export contacts](https://mailchimp.com/developer/marketing/build/quickstarts/import-and-export-contacts); `YOUR_LIST_ID` is the documentation placeholder.
- `tags.official.json`: complete two-tag response example from [Organize contacts with tags](https://mailchimp.com/developer/marketing/build/quickstarts/organize-contacts-with-tags).

Only whitespace was normalized. Inline responses in tests are explicitly synthetic variants for pagination, integer precision, malformed success, consent, redirects, and error handling. No real account was accessed.
