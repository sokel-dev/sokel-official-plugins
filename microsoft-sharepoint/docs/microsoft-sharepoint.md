# Microsoft SharePoint

Discover SharePoint sites and document libraries, inspect list columns, and read or write scalar list fields through Graph v1.0. Use a work/school account with delegated `Sites.Read.All` for site/list reads and `Sites.ReadWrite.All` for create/update. The signed-in user must also have permission on the resource. Personal Microsoft accounts and app-only credentials are outside this plugin's supported setup. Add `User.Read` for the identity health check.

## Operations and discovery

| Operation | Behavior |
| --- | --- |
| `search_sites` | Search tenant sites with free text, limited to 1,024 query bytes. |
| `get_site` | Read a composite site ID, hostname or the tenant `root` alias. |
| `list_drives` | List a site's document libraries and drive IDs. |
| `list_lists` | List visible lists in a site. |
| `list_columns` | Read internal names, display names, column kinds and required/read-only flags. |
| `list_items` | Read up to 100 list items with expanded `fields`. |
| `create_item` | Create one list item using a `fields` object. |
| `update_fields` | Patch fields on one existing numeric item ID. |

Use `search_sites` or `get_site` to obtain the composite site ID, then `list_lists` and `list_columns` to obtain a list ID and internal column names. IDs are strings. For file content operations, pass a returned drive ID to the OneDrive plugin. These operations do not implement SharePoint search queries, permissions changes, complex lookup/person writes, schema creation or deletion.

All collection operations return `next_link`. Pass it unchanged to the same operation while retaining its resource IDs. For item reads, the continuation retains Graph's field expansion. The returned field map is dynamic because every list defines its own columns; read values may contain arrays or objects. A column kind of `unknown` means its facet is not recognized by this version. System lists and hidden columns are not explicitly requested.

## Writing fields

Writes accept 1–100 scalar values keyed by internal names. Supported JSON types are string, finite number, boolean and null; array/object writes are rejected. Text values are limited to 64 KiB and the complete JSON body to 1 MiB. Use the column's actual type and allowed choices; Graph applies required-field and column validation. Null and empty strings are sent distinctly and may be rejected by a column that cannot be cleared.

For example, create an item with `{"Title":"Quarterly review","Count":0,"Done":false}` as `fields`. The create request wraps it as `{"fields":{...}}`. Updating sends the field map directly to `/items/{item-id}/fields`, preserving other columns. If an ETag is available from `list_items`, pass it as `if_match`; a 412 response prevents an update against a changed version. Omitting it allows an unconditional update. Create returns typed item metadata plus fields; update returns the requested item ID and Graph's field receipt. Writes do not use automatic retries or compensating deletes.

## Credentials and request behavior

Enter a delegated Microsoft Graph access token issued for the global Microsoft Graph service. Obtain consent and refresh the token outside this plugin, then replace the stored `access_token` when it expires. This plugin has no OAuth provider, refresh token exchange or automatic renewal. Include `User.Read` for **Check delegated credentials**, which performs `GET /v1.0/me?$select=id`. A successful check proves the signed-in user identity only; it does not prove access to the requested files, sites or workbook. Failures return `ok=false` and a message.

The origin is fixed to `https://graph.microsoft.com/v1.0`. Sovereign clouds and deployment URL overrides are not supported. IDs cannot contain URL paths or query strings. Continuation URLs are preserved without re-encoding and must remain on the same Graph origin and resource; credentials are never forwarded through redirects. Each list operation reads one page. Empty `next_link` means no continuation was returned. Account changes during pagination can cause records to move between pages.

Requests time out after 45 seconds. JSON request bodies are limited to 1 MiB and responses to 4 MiB. A 429 error reports the `Retry-After` delay or a 60-second fallback. No request is automatically retried, including writes. After a timeout, 5xx response or invalid write receipt, inspect the target resource before retrying. Errors omit upstream bodies, access tokens and transport URLs. Tests use documented API shapes and synthetic HTTP fixtures, not live account captures. Live acceptance has not been performed.

## Official references

- [Search sites](https://learn.microsoft.com/en-us/graph/api/site-search?view=graph-rest-1.0)
- [Get site](https://learn.microsoft.com/en-us/graph/api/site-get?view=graph-rest-1.0)
- [List drives](https://learn.microsoft.com/en-us/graph/api/drive-list?view=graph-rest-1.0)
- [List columns](https://learn.microsoft.com/en-us/graph/api/list-list-columns?view=graph-rest-1.0)
- [List items](https://learn.microsoft.com/en-us/graph/api/listitem-list?view=graph-rest-1.0)
- [Create item](https://learn.microsoft.com/en-us/graph/api/listitem-create?view=graph-rest-1.0)
- [Update fields](https://learn.microsoft.com/en-us/graph/api/listitem-update?view=graph-rest-1.0)
