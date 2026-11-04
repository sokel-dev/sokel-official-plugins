# Dropbox

List, search, upload and download files with a Dropbox user access token. This plugin uses the Dropbox API v2 and the public Sokel SDK v0.7.8.

## Credentials and deployment

Save a current user **Access token** in the credential. Grant `account_info.read` for the health check, `files.metadata.read` for listing/search/metadata, `files.content.read` for downloads, and `files.content.write` for uploads. Access is also limited by the app's App Folder or Full Dropbox configuration and the user's permissions.

Dropbox access tokens are short lived. This version does not refresh tokens or implement an OAuth provider: obtain and refresh tokens through your external authorization service, then update the Sokel credential. A generated developer token is useful for testing, not a permanent unattended deployment credential. Do not supply a team access token; team-member selection and namespace/root headers are not implemented.

The deployment defaults to `https://api.dropboxapi.com` for RPC and `https://content.dropboxapi.com` for file bytes. Operators may set `SOKEL_DROPBOX_BASE_URL` and `SOKEL_DROPBOX_CONTENT_BASE_URL` to trusted HTTPS endpoints. These are deployment settings, never workflow inputs. HTTP is allowed only for loopback tests. All redirects are rejected.

Run **Check credentials**. It performs the read-only `users/get_current_account` request and requires an account ID. A failed or unavailable credential returns `ok=false` with a safe message. Success checks identity, not every file permission.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Check the current account without writing. |
| `list_folder` | Read one page, optionally recursively and including deleted entries. |
| `continue_list` | Read the next page or subsequent changes using a saved list cursor. |
| `get_metadata` | Read file, folder or deleted-item metadata. |
| `search` | Search file/folder names or supported indexed content. |
| `continue_search` | Read the next search page with its separate search cursor. |
| `upload_file` | Create a file without overwriting or autorenaming a conflicting path. |
| `download_file` | Download file bytes and store them as a Sokel platform file. |

Use absolute paths such as `/Reports/2026.txt`. The empty string identifies the root for listing and search only. Read operations also accept `id:...` identifiers. Namespace paths, revision paths, shared-link credentials, trailing slashes, backslashes, dot components and control characters are not supported. Paths are limited to 4,096 UTF-8 bytes. Item names must be valid UTF-8, at most 255 bytes, and contain no path separators or control characters.

`list_folder.options` supports optional `recursive`, `include_deleted` and `limit` (1–2,000, default 100). Explicit false values are preserved. Dropbox treats this page size as approximate. Pass `cursor` to `continue_list` while `has_more` is true. Save the final cursor after processing the page: calling `continue_list` later retrieves changes, including deletions when enabled. An unchanged cursor with `has_more=false` is valid. There is no built-in polling source or durable checkpoint. If Dropbox rejects an expired/reset cursor, restart listing and reconcile your state.

Entries have `kind` equal to `file`, `folder` or `deleted`. File-only fields such as `size`, `rev` and `is_downloadable` are absent for folders/deletions; deleted entries have no file ID. `path_lower` and `path_display` can be absent for unmounted content. Downstream workflows should branch on `kind` before using file fields.

Search requires 1–1,000 characters. `options` accepts `path`, `filename_only` and `max_results` (1–1,000). Its cursor belongs only to `continue_search`. Stop when `has_more=false`. Dropbox search is indexed, may return duplicates across pages or omit recent results, and is limited to 10,000 matches. Search is not a complete change feed.

## File handling and limits

Uploads accept a Sokel platform file and an absolute destination file path. Requests fix `mode=add`, `autorename=false` and `strict_conflict=true`; a name conflict fails rather than replacing an existing file. Parent directories must already exist. Unicode paths, including supplementary characters, are escaped to ASCII-safe JSON in `Dropbox-API-Arg`. The token goes only to the configured Dropbox content endpoint, never the platform file's source URL.

Uploads and downloads support 0–32 MiB. Upload size is checked both before and after obtaining platform file bytes. Download metadata is read from `Dropbox-API-Result`; the plugin verifies file identity, declared size and the actual byte count before storing the platform file. This does not implement Dropbox content-hash verification. Non-downloadable cloud documents require an export API, which is outside this version. Upload sessions, folder creation, deletion, overwrite and revision-based updates are not implemented.

RPC requests time out after 45 seconds and file requests after 120 seconds; operation timeouts are 60 and 180 seconds respectively. JSON requests are limited to 1 MiB and JSON responses to 8 MiB. Errors omit upstream response bodies, tokens and transport URLs. A 429 reports a Retry-After delay when available. No request is automatically retried. If an upload times out or has an invalid receipt, inspect the destination before retrying because it may already have succeeded.

## Validation

Tests use synthetic fixtures based on the official API schema, not captured live account responses. They verify actual serialized requests, generated input binding, pagination, metadata unions, Unicode headers, empty files, size limits, cancellation/timeouts, redirects and secret-safe errors. No live account writes were performed.

## Official references

- [Dropbox file access guide](https://docs.dropboxapi.com/dropbox-api/docs/file-access)
- [Official file API schema](https://github.com/dropbox/dropbox-api-spec/blob/main/files.stone)
- [Official account API schema](https://github.com/dropbox/dropbox-api-spec/blob/main/users.stone)
- [OAuth and short-lived access tokens](https://docs.dropboxapi.com/dropbox-api/docs/oauth)
