# WPS365 Cloud Documents

Manage files and folders through the public WPS365 server REST API at `https://openapi.wps.cn/v7`. This integration targets WPS365 cloud documents. WebOffice callback APIs and its frontend editing SDK are separate products and are not implemented here. Personal desktop Office automation, spreadsheet cell editing, content conversion, file uploads and private deployments are outside this version.

## Application and credentials

Create an appropriate WPS365 developer application, request its API permissions and grant access to the intended data. Save an application or user access token in `access_token`; obtain and refresh it externally. This connector does not register an OAuth provider or refresh tokens automatically. App approval, tenant installation, data permissions and an optional deployment IP allowlist may be required by the application's configuration.

Read operations need `kso.file.read` or `kso.file.readwrite`; folder creation and renaming need `kso.file.readwrite`, together with access to the target drive and files. A token's app/user authorization type must match the permissions granted in WPS.

If API signing is enabled in the developer console, set both `app_id` (APPID) and `app_key` (APPKEY). If signing is disabled, leave both empty: the plugin then sends no `X-Kso-Date` or `X-Kso-Authorization`. Enabled signing uses the public KSO-1 HMAC-SHA256 algorithm over the actual method, encoded request URI including query, content type, GMT date and exact request bytes. The empty body is signed with an empty body hash, as specified by WPS. Only internal WPS IDs are used; this version does not send `X-Kso-Id-Type: external`.

`health_check` reads `/v7/developer/applications/current`, an endpoint with no additional scope requirement, and validates its envelope and application metadata. It returns `ok` and `message`. Success verifies the current credential and signature configuration; file access must still be checked against the intended drive.

## File workflow

Obtain the target `drive_id` and file/folder IDs from WPS365. The plugin does not enumerate drives. Use `parent_id` equal to `0` for the root folder.

| Operation | Behavior |
| --- | --- |
| `list_files` | Read one child-file page under the specified folder. |
| `get_file` | Read typed metadata by drive and file ID. |
| `create_folder` | Create an empty folder; fail when the name already exists. |
| `rename_file` | Rename the specified file or folder. |
| `get_file_path` | Read the location and ordered path segments. |
| `get_download_info` | Return a temporary download URL and available content hashes. |

`list_files` defaults to 100 items and supports 1–500. Keep the same drive/folder and pass back `next_page_token` unchanged until it is empty. A short or empty page can still have a continuation token. The plugin rejects a repeated token instead of silently looping.

File metadata includes IDs, name, type, size, version and the service's numeric creation/modification timestamps. Folder creation always sends `file_type: folder` and `on_name_conflict: fail`. Names may contain Unicode; this connector limits them to 240 characters and rejects reserved filename characters and controls. These are plugin limits in addition to any server-specific restrictions. Renaming sends `dst_name` and verifies the returned ID and name. Creation and rename receipts must match the requested resource.

Example inputs:

```json
{"drive_id":"YOUR_DRIVE_ID","parent_id":"0","page_size":100}
```

```json
{"drive_id":"YOUR_DRIVE_ID","parent_id":"0","name":"Workflow reports"}
```

## Download metadata and boundaries

`get_download_info` requests the public download information with `internal=false` and `with_hash=true`. It returns the URL without following it or uploading a platform file. Treat that URL as sensitive; it may expire and the storage gateway may have additional access requirements. File content is returned in its original format by WPS; this operation does not convert or merge it. Any subsequent downloader must implement its own URL, redirect and credential policy. The plugin never forwards the WPS token or signing headers to a storage host.

All API calls use the fixed public China origin, and HTTP redirects are rejected. Tokens cannot change the origin. JSON requests are limited to 1 MiB and responses to 4 MiB. No automatic retries are made, including writes. After an uncertain write timeout, read the destination before attempting the action again. This connector provides no event subscription or background source.

WPS business errors can arrive with HTTP 200. A response succeeds only when `code` is explicitly present and equals zero, `data` is valid, and the operation's response shape passes validation. Error messages expose the numeric WPS code or HTTP status but exclude upstream bodies and credential values. HTTP 429 includes a sanitized wait duration from `Retry-After` when available. Check permissions, token expiry, signature settings and application data access when a read fails.

## Official references and validation

- [WPS365 server products](https://open.wps.cn/documents/app-integration-dev/wps365/server/introduce), [API calling flow](https://open.wps.cn/documents/app-integration-dev/wps365/server/api-description/flow), [KSO-1 signing](https://open.wps.cn/documents/app-integration-dev/wps365/server/api-description/signature-description)
- [Current application](https://open.wps.cn/documents/app-integration-dev/wps365/server/app/get-info)
- [List files](https://open.wps.cn/documents/app-integration-dev/wps365/server/yundoc/file/get-file-list), [metadata](https://open.wps.cn/documents/app-integration-dev/wps365/server/yundoc/file/get-file-info-by-id), [create](https://open.wps.cn/documents/app-integration-dev/wps365/server/yundoc/file/create-file), [rename](https://open.wps.cn/documents/app-integration-dev/wps365/server/yundoc/file/rename-file)
- [Path](https://open.wps.cn/documents/app-integration-dev/wps365/server/yundoc/file/get-file-path), [download information](https://open.wps.cn/documents/app-integration-dev/wps365/server/yundoc/file/get-file-download)

Validation uses synthetic HTTP fixtures and the official published KSO-1 signature vectors. No real WPS token, live write or live service acceptance is claimed. Any first-release catalog fixture is temporary test input only, not a published image or verified registry digest.
