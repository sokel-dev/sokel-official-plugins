# Google Docs

Read document text and automate edits with Google OAuth. Enable the Google Docs API in the OAuth application's Google Cloud project, configure the platform's `google` provider, then authorize a credential. Requested scopes are `documents` and `userinfo.email`; the platform manages access and refresh tokens. The account must have access to each document.

## Operations

- **Get document and tab text** returns the document ID, title, current revision and a flattened list of all tabs, including child tabs. Each tab has its ID, parent ID, body text and body end index. Paragraph, table-cell and table-of-contents text is included; formatting, images, headers, footers, footnotes and suggestions are not exported. Plain text is a reading projection, not an index map.
- **Create blank document** creates a document with a title. Get the new document to obtain its first tab ID, then insert text. Use Google Drive for folders, sharing, copying and export.
- **Insert or append text** requires a tab ID. An omitted or zero `index` appends before that tab's final newline; a positive index inserts at that location. Google Docs indexes use UTF-16 code units, not bytes or Unicode character counts.
- **Replace text in a tab** replaces literal text in one explicit tab. Case matching defaults to false. An empty replacement deletes matches. Inspect `occurrences_changed`; zero means no text matched.
- **Delete a text range** uses inclusive `start_index` and exclusive `end_index`. Do not delete a tab's final newline or split a Unicode surrogate pair. Structural positions must come from Google's document model, not offsets in the flattened text.
- **Check credential** makes a read-only Google user-info request. A successful result verifies authorization, not access to every document or whether the Docs API is enabled.

Pass the latest `revision_id` as `required_revision_id` for a write that must fail if someone has edited the document since it was read. Omit it to write against the current document. A rejected revision is not retried: read again and decide how to apply the edit. Each edit is one batchUpdate request; writes are never automatically retried.

Example: get a template document, select the tab ID, then replace `{{customer}}` with a workflow value. For a new copy per customer, copy with Google Drive first. This plugin does not create automatic triggers.

Requests use fixed Google origins, reject redirects and cap JSON at 16 MiB. Large documents may need a narrower dedicated integration. API failures return English messages without upstream response bodies. No real-account acceptance has been performed as part of the fixture test suite.

References: [Docs tabs](https://developers.google.com/workspace/docs/api/how-tos/tabs), [requests](https://developers.google.com/workspace/docs/api/reference/rest/v1/documents/request), [batchUpdate and write control](https://developers.google.com/workspace/docs/api/reference/rest/v1/documents/batchUpdate).
