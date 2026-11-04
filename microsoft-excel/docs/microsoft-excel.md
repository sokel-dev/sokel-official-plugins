# Microsoft Excel 365

Read and edit Office Open XML workbooks stored in OneDrive for Business, SharePoint document libraries or group drives. This plugin does not support personal OneDrive workbooks, legacy `.xls` files or application-only permissions. Use a work/school delegated token with `Files.ReadWrite` and workbook access; add `User.Read` for the identity health check. Microsoft documents inconsistent personal-account permission rows across individual Excel endpoints, so this plugin follows the documented business-storage boundary and session requirements.

## Workbooks and sessions

Obtain a `drive_id` and the workbook's drive item ID with the OneDrive or SharePoint plugin. Pass the latter as `workbook_id`. Session IDs belong to a particular workbook; do not reuse one for another file. This plugin has no OAuth renewal, local session cache or automatic session creation.

| Operation | Behavior |
| --- | --- |
| `create_session` | Create a workbook session with explicit `persist_changes`. |
| `close_session` | Close a supplied workbook session; requires a 204 response. |
| `list_worksheets` | List worksheet IDs/names, positions and visibility. |
| `list_tables` | List table IDs/names and header visibility. |
| `get_range` | Read a bounded A1 range on a worksheet. |
| `update_range` | Replace values in exactly the specified range. |
| `append_rows` | Append a rectangular batch to an existing table. |
| `list_rows` | Read up to 100 table rows using an explicit offset. |

For a temporary calculation/editing session, set `persist_changes=false`; for saved changes, set it to `true`. The flag is required and false is sent explicitly. Pass the returned `session_id` to subsequent calls; it becomes the `workbook-session-id` header. Closing a non-persistent session does not save its changes. If `session_id` is omitted, Graph uses sessionless calls and writes persist. Session creation is synchronous; async 202 responses are rejected and no polling URL is followed. Expired sessions require explicit recovery by the caller.

`list_worksheets` and `list_tables` return `next_link` for continuation on the same collection. `list_rows` uses `skip` as a nonnegative string offset and `$top=100`. If 100 rows are returned, `has_more=true` and `next_skip` advances by 100; an exact final full page requires one extra empty read. This indicator means more rows may exist, not a guaranteed remaining count. A short page that still carries a server continuation fails explicitly because this operation cannot translate an opaque continuation into a safe row offset. Keep the same session while paging and avoid concurrent row inserts/deletes; unexpected row indexes fail explicitly.

## Ranges and cell values

Provide a worksheet ID or name and a bounded address such as `A1:C2`. Sheet prefixes, whole columns, named ranges and open-ended ranges are not supported. Requests are limited to 1,000 rows, 100 columns and 10,000 cells, within Excel's worksheet bounds. Values must be a rectangular two-dimensional array of strings, finite numbers, booleans or null. Range writes must exactly match the address dimensions. For example, `[["Name","Done"],["Review",false]]` fills `A1:B2`.

Graph range updates treat null cells as unchanged; an empty string clears a cell value. This version sends the `values` property only, not `formulas`, formatting or number formats. Excel applies its normal value interpretation. For identifiers that require exact digits beyond floating-point precision, use text strings. Text cells are limited to 32,767 bytes by this plugin; the complete JSON body is limited to 1 MiB.

`append_rows` omits the optional index to append at the end. Graph returns one `workbookTableRow` receipt whose `values` may contain multiple inserted rows; the plugin checks that receipt dimensions match the request. A failed or timed-out append can already have changed the workbook. Inspect the table before retrying to avoid duplicate rows, including after 504 responses. Returned range and row values remain dynamic because cell types vary by workbook. No workbook creation, table creation, deletion or formula-specific operation is included.

## Credentials and request behavior

Enter a delegated Microsoft Graph access token issued for the global Microsoft Graph service. Obtain consent and refresh the token outside this plugin, then replace the stored `access_token` when it expires. This plugin has no OAuth provider, refresh token exchange or automatic renewal. Include `User.Read` for **Check delegated credentials**, which performs `GET /v1.0/me?$select=id`. A successful check proves the signed-in user identity only; it does not prove access to the requested files, sites or workbook. Failures return `ok=false` and a message.

The origin is fixed to `https://graph.microsoft.com/v1.0`. Sovereign clouds and deployment URL overrides are not supported. IDs cannot contain URL paths or query strings. Continuation URLs are preserved without re-encoding and must remain on the same Graph origin and resource; credentials are never forwarded through redirects. Each list operation reads one page. Empty `next_link` means no continuation was returned. Account changes during pagination can cause records to move between pages.

Requests time out after 45 seconds. JSON request bodies are limited to 1 MiB and responses to 4 MiB. A 429 error reports the `Retry-After` delay or a 60-second fallback. No request is automatically retried, including writes. After a timeout, 5xx response or invalid write receipt, inspect the target resource before retrying. Errors omit upstream bodies, access tokens and transport URLs. Tests use documented API shapes and synthetic HTTP fixtures, not live account captures. Live acceptance has not been performed.

## Official references

- [Excel storage and permission overview](https://learn.microsoft.com/en-us/graph/api/resources/excel?view=graph-rest-1.0)
- [Create session](https://learn.microsoft.com/en-us/graph/api/workbook-createsession?view=graph-rest-1.0)
- [Close session](https://learn.microsoft.com/en-us/graph/api/workbook-closesession?view=graph-rest-1.0)
- [Read range](https://learn.microsoft.com/en-us/graph/api/worksheet-range?view=graph-rest-1.0)
- [Update range](https://learn.microsoft.com/en-us/graph/api/range-update?view=graph-rest-1.0)
- [Append rows](https://learn.microsoft.com/en-us/graph/api/tablerowcollection-add?view=graph-rest-1.0)
- [List rows](https://learn.microsoft.com/en-us/graph/api/table-list-rows?view=graph-rest-1.0)
