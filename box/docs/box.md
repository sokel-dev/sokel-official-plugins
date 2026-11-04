# Box

Read files and folders, search content, create folders, and transfer files through the Box API. This plugin uses the standard `/2.0` endpoints and the public Sokel SDK v0.7.7.

## Credentials and deployment

Save a current **Access token** with the app scopes and item permissions required by your workflow. This version accepts tokens obtained by your external OAuth, JWT or client-credentials service, but does not perform those flows or refresh tokens. Update the Sokel credential when the token changes. Box Developer Tokens expire after 60 minutes and cannot be refreshed programmatically; use them only for development and testing, not unattended production workflows. No OAuth provider is declared.

The deployment defaults to `https://api.box.com` for API requests and `https://upload.box.com` for uploads. Operators may set `SOKEL_BOX_BASE_URL` and `SOKEL_BOX_CONTENT_BASE_URL` to trusted HTTPS endpoints. Workflow callers cannot choose an authenticated upstream URL. HTTP is allowed only for loopback tests. Ordinary API and upload redirects are rejected.

Run **Check credentials** to read `/2.0/users/me` and verify the user ID. It makes no writes and returns `ok=false` on authentication or response failures. Success checks identity, not every folder permission or upload policy.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Verify the current user's identity. |
| `list_folder` | Read 100 items using marker pagination. |
| `search` | Search accessible non-trashed items using offset pagination. |
| `get_file` | Read file metadata. |
| `get_folder` | Read folder metadata. |
| `create_folder` | Create a folder under the supplied parent. |
| `upload_file` | Upload a new file under the supplied parent. |
| `download_file` | Download a file and store it as a Sokel platform file. |

IDs are numeric strings; folder ID `0` means the account root. A file ID cannot be `0`. Folder and file names must be valid UTF-8, at most 255 bytes, without paths, control characters or leading/trailing whitespace. Box may impose additional naming or content restrictions.

For `list_folder`, omit `marker` initially, then pass `next_marker` while `has_more=true`. The request explicitly sets `usemarker=true`. Entries may be `file`, `folder` or `web_link`; inspect `type` before using file-only fields. Optional metadata is absent when Box does not supply it. Each call reads one page and does not create a snapshot.

Search accepts a query of 1–4,096 bytes, an optional `ancestor_folder_id`, and a `page` object with `offset` (0–10,000, default 0) and `limit` (1–200, default 100). Recent shared-link wrappers are excluded to keep one stable item shape. `total_count` is Box's offset bound, not a guaranteed exact record count. Advance with `next_offset`, which adds the returned page limit rather than the number of entries; valid pages may contain fewer items or gaps. Stop when `has_more=false`. If the next offset exceeds 10,000, partition the query. Indexed search can lag writes and return changing results.

## Uploads and downloads

Uploads take a Sokel platform file and a parent folder ID. The multipart request sends JSON `attributes` before the file part, and preserves UTF-8 filenames using the regular quoted `filename` parameter. A filename collision returns an error; this operation does not upload a new version or overwrite an existing file. The Box token is never sent to the platform file source URL.

Both directions support 0–32 MiB. Upload size is checked before and after reading platform bytes. The plugin verifies the returned upload receipt contains one file with the expected size. Larger files and chunked upload sessions are outside this version.

Downloads first read file metadata, then request `/files/{id}/content`. Box normally returns a temporary download URL. The plugin follows at most three download redirects, accepts only HTTPS hosts under `.boxcloud.com` on port 443, rejects user information and access-token query parameters, and sends no Authorization header or cookies after leaving the API request. This supports Box's documented `dl.boxcloud.com` download URLs. Other storage/CDN domains are deliberately unsupported and fail explicitly; they cannot be enabled through workflow inputs.

The plugin verifies the downloaded size and, when present, the file's SHA-1 checksum before storing a platform file. Concurrent changes may cause validation to fail; retry the whole read operation to obtain fresh metadata. Download URLs are not returned to workflows. If Box says the file is not ready, the operation fails without waiting or polling.

## Errors and limits

Metadata requests time out after 45 seconds; uploads allow 120 seconds and the download network sequence has a 120-second deadline. Operation timeouts are 60 seconds for metadata and 180 seconds for file transfer. JSON requests are limited to 1 MiB and responses to 8 MiB. Errors omit upstream bodies, bearer tokens and signed URLs. A 429 reports a Retry-After delay when available. No request is automatically retried. After an uncertain folder creation or upload, inspect Box before retrying because the write may already have succeeded.

This version has no delete, move, overwrite, event source, automatic checkpoint, shared-link authentication or acting-as-user feature.

## Validation

Tests use synthetic fixtures based on official documentation, not captured live account responses. They verify serialized request bytes, generated input/output binding, pagination, multipart ordering and filenames, redirect allowlists, absence of credentials on content requests, empty files, size/checksum validation, cancellation/timeouts and secret-safe errors. No live writes were performed.

## Official references

- [List folder items](https://developer.box.com/reference/get-folders-id-items)
- [Search](https://developer.box.com/reference/get-search)
- [Get file metadata](https://developer.box.com/reference/get-files-id)
- [Create folder](https://developer.box.com/reference/post-folders)
- [Upload file](https://developer.box.com/reference/post-files-content)
- [Download behavior](https://developer.box.com/guides/downloads/file)
- [Get current user](https://developer.box.com/reference/get-users-me)
- [Developer token limitations](https://developer.box.com/guides/authentication/tokens/developer-tokens)
