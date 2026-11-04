# Nextcloud

Read and write files on your own Nextcloud server through the authenticated WebDAV API. No SaaS account is required. Files can stay within your infrastructure; any external-storage mounts, sharing services or server applications have their own dependencies. The connector does not install Nextcloud, configure external storage or enable public sharing. Nextcloud Server is licensed under AGPL-3.0.

## Configure the connection

Set deployment variable `SOKEL_NEXTCLOUD_URL` to the installation root, for example `http://nextcloud` or `https://files.example.org/cloud`. Do not include `/remote.php/dav`, credentials, query parameters or fragments. Internal HTTP and reverse-proxy path prefixes are supported. There is no public fallback. Inside a container, `localhost` means that container.

Save the canonical **User ID** and an **App password** from personal security settings as the plugin credential. Use the user ID shown in the account's WebDAV address, not a display name or an email login alias. Requests authenticate using HTTP Basic to `/remote.php/dav/files/{user_id}/`. An app password is especially useful with two-factor or external login. The plugin uses only the permissions of this account.

Use **Get file or folder metadata** with an empty path for a read-only connection check. The implementation follows Nextcloud's basic WebDAV API documented for server 33; the same basic API is documented in current manuals. It has protocol and generated-dispatch fixture coverage, **not a live Nextcloud deployment acceptance test**. No minimum-version or exhaustive server-compatibility claim is made.

## Operations

| Operation | Behavior |
| --- | --- |
| Get file or folder metadata | `PROPFIND Depth: 0`; returns path, name, directory state and available file ID, ETag, size, type and modified time. Empty path means the user's file root. |
| List directory | `PROPFIND Depth: 1`; returns the directory and its immediate children. Empty path means the root. |
| Create directory | `MKCOL`; creates one folder. Its parent must already exist. |
| Upload file | Read a native platform file and `PUT` it to the destination path. Returns whether a new file was created and the response ETag when available. |
| Download file | Check that the path is a file, fetch its bytes and store them as a native platform file for later workflow steps. Uses a strong ETag precondition when the metadata provides one. |
| Move file or directory | `MOVE` within the same account and installation. The destination must not exist, and its parent must exist. Optional expected ETag protects the source from concurrent changes. |

File IDs and ETags are strings and are never converted to workflow numbers. Failed optional WebDAV properties are omitted; a failed resource-type property is an error. A 207 multi-status response alone is not treated as success for all entries.

## Paths, files and overwrite protection

Paths are relative to the user's file root, for example `Reports/weekly report.pdf`. A single leading or trailing slash is accepted. Spaces, Unicode, `#` and `?` are encoded by the plugin. Supply **decoded paths**, not URLs. This version rejects literal percent signs (including percent escapes), backslashes, control characters, repeated slashes and `.`/`..` segments. The root is valid only for metadata and directory listing. A canonical user ID cannot contain `/`, `\\`, `%`, control characters or `:`.

Upload accepts a native platform file reference or inline bytes, **not an arbitrary download URL**. Each file is limited to 32 MiB, including downloads. An empty file is supported through a valid platform file ID. Destination filenames come from `path`; they need not match the input file's name. Download's optional output filename must be a basename, at most 255 UTF-8 bytes; by default it is the source basename.

Uploads default to `overwrite=false` and send `If-None-Match: *`. If the destination exists, the request fails rather than overwriting it. To replace an existing file, explicitly set `overwrite=true`. For concurrency protection, also pass the exact quoted strong ETag returned by metadata, such as `"a1b2c3"`, in `if_match`. `if_match` without `overwrite=true` is rejected. An overwrite without an ETag can replace the current destination regardless of intervening edits.

Moves always send `Overwrite: F`; no option silently replaces an existing destination. Moving the root, moving a path onto itself, or moving a folder inside itself is rejected. Weak ETags and wildcard ETags are not accepted as expected ETags.

## Limits and failures

Directory listing is one request, with no invented offset pagination. At most 2000 direct children and 8 MiB of XML are accepted. Larger or malformed responses fail instead of returning an incomplete list. Returned resource URLs must remain on the configured origin and inside this user's file tree; they are parsed as metadata and are never fetched as arbitrary URLs.

The HTTP client timeout is 120 seconds; metadata, folder and move operations have a 60-second SDK deadline, while file transfer operations have 180 seconds. Redirects and automatic write retries are disabled. HTTP 412 means the destination already exists or an ETag precondition failed; HTTP 423 indicates a lock. Errors omit raw server bodies and credential values. If a write times out, inspect current file state before repeating it. If platform storage fails after a download, no file output is reported.

This version excludes chunked upload, public shares, trash/deletion, recursive listing, search, share management and cross-account moves. Protocol tests do not establish a deployed service, available image or marketplace release.

Official references: [basic WebDAV](https://docs.nextcloud.com/server/33/developer_manual/client_apis/WebDAV/basic.html), [current WebDAV manual](https://docs.nextcloud.com/server/latest/developer_manual/client_apis/WebDAV/basic.html), [server releases](https://nextcloud.com/changelog/), [server license](https://github.com/nextcloud/server/blob/v33.0.10/COPYING).
