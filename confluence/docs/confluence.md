# Confluence Cloud

Read spaces and create or update published pages with the Confluence Cloud v2 API. The credential check uses the current-user endpoint in v1. Server and Data Center are not supported.

## Credential and permissions

Save your Atlassian account `email` and `api_token` in a Sokel credential. For a token without scopes, set `site` to `https://your-site.atlassian.net` or its bare hostname. For a scoped token, set the tenant `cloud_id` UUID instead; it takes precedence over site and selects the fixed `https://api.atlassian.com/ex/confluence/{cloud_id}` gateway. Obtain the Cloud ID from the site's `/_edge/tenant_info` or your administrator.

Both routes use email/token Basic authentication. Only HTTPS Atlassian Cloud origins or the fixed scoped-token gateway are accepted. URL credentials, custom domains, ports, paths, queries and redirects are rejected. Expiring API tokens must be replaced manually; OAuth and token refresh are not included.

Grant access to the required spaces and pages, plus read/write scopes for the selected endpoints. In particular, the v2 reads use space/page read scopes, writes use page write scopes, and the v1 current-user check requires its user read scope. `health_check` performs a readonly `GET /wiki/rest/api/user/current` and returns `ok` and a sanitized `message`. It does not verify write permission in every space.

## Operations and pagination

| Operation | Behavior |
| --- | --- |
| `list_spaces` | Read one page of accessible spaces, optionally matching a space key. |
| `get_space` | Read a space by numeric ID. |
| `list_pages` | Read one page of current published pages, optionally filtered by space ID and exact title. |
| `get_page` | Read a page, its body and current version by numeric ID. |
| `create_page` | Create a published page in a space, optionally under a parent page. |
| `update_page` | Replace title/body only after checking the supplied current version. |
| `health_check` | Read current user identity through the v1 endpoint. |

List operations accept `limit` (1–100, default 50) and `cursor`. Pass `next_cursor` to the next call with unchanged filters; an empty cursor means completion. The plugin extracts a cursor from the v2 `_links.next` or `Link` header and validates its origin and resource path. It never follows an arbitrary pagination URL.

Space outputs include ID, key, name, type and status. Page outputs include ID, space/parent IDs, title, status, version, body, representation, created timestamp and web UI path. `web_path` is an upstream path rather than a guaranteed absolute URL. Dates are returned unchanged. All page, parent and space IDs must be positive numeric strings.

## Page bodies and writes

`representation` is `storage` (the default, Confluence storage XHTML) or `atlas_doc_format` (a JSON document encoded as a string). ADF must have `type: "doc"` and `version: 1`; Confluence validates the rest of the document. Plain Markdown and rendered HTML views are not accepted as alternate formats. Storage content is sent as supplied; the plugin does not render or sanitize markup.

```json
{"space_id":"12345","parent_id":"67890","title":"Research notes","body":"<p>Quarterly findings.</p>","representation":"storage"}
```

Create only publishes current pages. To update, first call `get_page`, reconcile your changes with its body, then send the returned version as `expected_version`, together with the complete desired title and body:

```json
{"page_id":"67890","expected_version":7,"title":"Research notes","body":"<p>Revised findings.</p>","version_message":"Update analysis"}
```

The plugin performs another GET, rejects a stale version or non-current page, then submits version 8 in this example. If another writer races after the check, the API conflict is returned. No blind overwrite retry occurs. After a conflict, fetch and reconcile again. This replaces the full body. Always supply the complete desired content; an omitted or empty body is rejected. To intentionally clear a storage body, send an explicit empty paragraph such as `<p></p>`. Title and body are required.

Confluence may reconcile a published-page update with an existing draft and, when content diverges, may overwrite that draft. The version guard protects the published version only. Review existing drafts before replacing content; this plugin does not manage drafts.

## Limits and errors

Operations have a 60-second platform timeout and 55-second HTTP timeout. Requests are limited to 4 MiB and responses to 8 MiB. HTTP failures return sanitized English errors without response bodies, credentials or URLs. Empty result arrays are valid; missing list fields are rejected. Rate limits and other errors are not automatically retried. An ambiguous write timeout may already have committed; inspect the page before retrying.

This version does not create/edit spaces, delete pages, manage drafts, attachments or restrictions, search with CQL, or provide event triggers. Tests use synthetic protocol fixtures and cover request bytes, pagination links and concurrency conflicts. No live-account acceptance has been performed.
