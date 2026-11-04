# Google Drive

List or search files, upload small files, download binary content, export Google Workspace files, and poll file changes.

## Operations and limits

| Operation | Usage |
|---|---|
| list_files | Drive v3 q syntax, for example `name contains 'report' and trashed = false`; defaults to excluding trash; page_size 1–1000; pass next_page_token back as page_token |
| upload_file | Platform file with optional name/parent_id; creates a new multipart file up to 5 MiB; no overwrite or resumable uploads |
| download_file | Binary file_id, up to 32 MiB, returned as a platform file reference |
| export_file | Workspace file_id, export mime_type and filename; Google limits exports to 10 MiB; application/pdf is a common document format |
| health_check | Validate identity authorization |

Scopes are drive.readonly plus drive.file: read files covered by the user's grant and write application-created files. An existing parent folder needs both write permission and application access; otherwise uploading there can fail with 403. Root is the default destination. Delete, share, move and permission changes are not implemented. Shared-drive account and file permissions still apply; a driveId-specific selector is not provided.

File bytes enter and leave through the platform file layer. Platform file URLs are never supplied to Google's download endpoints. Google Docs/Sheets/Slides require export_file and are rejected by download_file. Long-running Google Vids downloads are not supported.

## Change events

Set watch_changes to true to poll file_changed every 60 seconds. Initial setup calls changes.getStartPageToken and emits no history. Events cover creation, modification, trash status, and removal of access/deletion. removed=true may mean access was lost, not permanent deletion; the file name may be empty.

All changes.list pages and event publications must complete before the plugin publishes newStartPageToken as its checkpoint. A round supports at most 1000 pages; larger backlogs fail visibly and retain the previous cursor. Replay uses stable event IDs. HTTP 410 never silently skips to the present: inspect the error before clearing poll_cursor to rebaseline; changes during the gap may be unavailable.

SDK v0.7.8 event and credential updates are fire-and-forget: a successful call only confirms local publish acceptance, not platform receipt or durable storage. Delivery is best-effort; disconnects may lose events or replay them. Stable event IDs support platform deduplication, but this is not an exactly-once guarantee. End-to-end delivery and persistence still need live platform acceptance. The managed next_poll_at field keeps source restarts from bypassing the 60-second interval; do not edit it.

## Setup and runtime

An administrator must register a Google OAuth app and its exact callback URL on the platform and enable the corresponding API in the same Google Cloud project. Create a plugin credential and click Authorize. The plugin only uses the short-lived access token injected by the platform. Client secrets, code exchange and refresh remain on the platform; the plugin writes no token files. The userinfo.email scope identifies the authorized account.

```sh
SOKEL_ENDPOINT=https://sokel.example.com SOKEL_TOKEN=skp_xxx ./google-drive
```

The encrypted refresh_token field belongs to the platform authorization flow; do not fill it manually. Google API origins are fixed and HTTP redirects are refused. Request errors never include upstream response bodies, tokens or complete URLs. There are no automatic retries, including writes. A connection failure after a successful write is ambiguous: inspect the upstream before retrying.

health_check validates Google identity authorization only, not service enablement, resource access or write permission.

## Validation status

Tests use synthetic protocol examples based on official documentation to verify wire requests, pagination, failures and polling checkpoints. They are not live account captures. No live Google credentials were available, so OAuth consent, real resource operations and deployment acceptance remain unverified. Complete those checks with a test account before production use.
