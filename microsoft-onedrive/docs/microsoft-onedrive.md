# Microsoft OneDrive

Browse drive items, search file content and metadata, upload small files, obtain temporary download information, and explicitly read delta pages. This version has no automatic polling source or webhook.

## Setup and scopes

Use delegated `Files.Read` for reading the signed-in user's files and `Files.ReadWrite` for uploads. Broader delegated file or site permissions may be needed for other accessible drives; the signed-in user must also have resource access. Personal and work/school accounts are supported by the relevant Drive APIs, subject to account capabilities. Include `User.Read` for the health check. Discover your default drive with `get_drive`; copy the returned drive ID into subsequent operations. A SharePoint document library drive ID can be obtained with the SharePoint plugin.

| Operation | Behavior |
| --- | --- |
| `get_drive` | Read an explicit drive ID, or the signed-in user's default drive when omitted. |
| `list_items` | Read up to 100 children; omit `parent_id` to use the root. |
| `get_item` | Read one drive item by ID. |
| `search_items` | Search a drive; text can match names, metadata and content. |
| `upload_file` | Upload a platform file of at most 4 MiB to a parent folder. |
| `delta` | Read one explicit change page or continue a saved checkpoint. |
| `get_download_info` | Return file metadata and a temporary download URL, without downloading bytes. |

List and search calls return `next_link`. Pass it to the same operation with the same drive, parent and search inputs. Search text is limited to 1,024 bytes. Outputs include ID, name, size, `size_known`, browser URL, ETag, modified time, folder/deleted flags and MIME type. Metadata omitted by Graph is represented by empty strings. For partial delta records, `size_known=false` means `size=0` is unknown rather than an empty file.

## Upload and download boundaries

`upload_file` reads bytes through the Sokel platform file API and sends raw binary content to Graph. It does not fetch an arbitrary input URL. Zero-byte files are allowed; declared and actual sizes are checked against 4 MiB. Filenames must not contain paths, reserved OneDrive filename characters or trailing spaces/dots. This operation replaces an existing file with the same name. Supply `if_match` with an existing ETag to request optimistic concurrency checking; a stale value produces a 412 error. Large upload sessions, folders, rename, move and delete operations are not included.

`get_download_info` returns the short-lived, pre-authenticated HTTPS URL supplied by Graph. Treat it as sensitive file access information: avoid logging or publishing it. It requires no Graph Authorization header. The plugin does not request that URL, follow its redirects or turn it into a platform file. Downstream downloaders must apply their own network and redirect policy and must never attach this plugin's bearer token. If the URL expires, obtain new download information. Folder items or missing/invalid download URLs fail explicitly.

## Explicit delta checkpoints

Start `delta` with only `drive_id`. Continue `next_link` through `continuation_url`; when `delta_link` is returned, save it only after successfully processing the entire change sequence. Use that checkpoint as the next `continuation_url`. The plugin accepts query-style and `delta(token=...)` links on the same drive's `/root/delta` or `/delta` endpoint. It rejects another drive and user/site aliases whose equivalence cannot be verified.

Delta may omit metadata and return an item ID more than once. Results remain in received order; consumers must apply the last occurrence and handle `deleted=true`. A delta feed reports current state, not an audit log of every change. A 410 error requires a fresh enumeration and explicit reconciliation. There is no automatic checkpoint storage, scheduling or exactly-once delivery claim.

## Credentials and request behavior

Enter a delegated Microsoft Graph access token issued for the global Microsoft Graph service. Obtain consent and refresh the token outside this plugin, then replace the stored `access_token` when it expires. This plugin has no OAuth provider, refresh token exchange or automatic renewal. Include `User.Read` for **Check delegated credentials**, which performs `GET /v1.0/me?$select=id`. A successful check proves the signed-in user identity only; it does not prove access to the requested files, sites or workbook. Failures return `ok=false` and a message.

The origin is fixed to `https://graph.microsoft.com/v1.0`. Sovereign clouds and deployment URL overrides are not supported. IDs cannot contain URL paths or query strings. Continuation URLs are preserved without re-encoding and must remain on the same Graph origin and resource; credentials are never forwarded through redirects. Each list operation reads one page. Empty `next_link` means no continuation was returned. Account changes during pagination can cause records to move between pages.

Requests time out after 45 seconds. JSON request bodies are limited to 1 MiB and responses to 4 MiB. A 429 error reports the `Retry-After` delay or a 60-second fallback. No request is automatically retried, including writes. After a timeout, 5xx response or invalid write receipt, inspect the target resource before retrying. Errors omit upstream bodies, access tokens and transport URLs. Tests use documented API shapes and synthetic HTTP fixtures, not live account captures. Live acceptance has not been performed.

## Official references

- [List children](https://learn.microsoft.com/en-us/graph/api/driveitem-list-children?view=graph-rest-1.0)
- [Search drive items](https://learn.microsoft.com/en-us/graph/api/driveitem-search?view=graph-rest-1.0)
- [Upload content](https://learn.microsoft.com/en-us/graph/api/driveitem-put-content?view=graph-rest-1.0)
- [Download content](https://learn.microsoft.com/en-us/graph/api/driveitem-get-content?view=graph-rest-1.0)
- [Delta and checkpoint forms](https://learn.microsoft.com/en-us/graph/api/driveitem-delta?view=graph-rest-1.0)
