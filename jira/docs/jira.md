# Jira Cloud

Read and modify Jira Cloud issues with an Atlassian account email and API token. Jira Server and Data Center are not supported.

## Credential and routing

Save `email` and `api_token` in a Sokel credential. Use an API token, not your account password. For a token without scopes, set `site` to `https://your-site.atlassian.net` or its bare hostname. Only a single HTTPS Atlassian Cloud origin is accepted; custom domains, ports, URL credentials, paths and queries are rejected.

For a scoped token, instead set the tenant's `cloud_id` UUID. It takes precedence over `site` and routes through `https://api.atlassian.com/ex/jira/{cloud_id}` as required by Atlassian. Retrieve the Cloud ID from your site's `/_edge/tenant_info` or your organization's Atlassian administrator. Both routes use Basic authentication with the account email and API token. Redirects are rejected. Tokens expire and require manual replacement; no OAuth or token refresh is implemented.

Grant scopes for the operations you intend to use and the underlying project permissions. Identity checking additionally requires the permission/scope for the current-user endpoint. `health_check` performs a readonly `GET /rest/api/3/myself`, returning `ok` and a sanitized `message`. A successful identity check does not prove create, edit or transition permission in a project.

## Operations

| Operation | Behavior |
| --- | --- |
| `search_issues` | Enhanced JQL search using `POST /rest/api/3/search/jql`. |
| `get_issue` | Get selected core issue fields by key or numeric ID. |
| `create_issue` | Create using project key, issue type ID, summary, optional details and custom fields. |
| `update_issue` | Modify supplied details and custom fields; omission preserves existing values. |
| `list_transitions` | List available transitions and their required-field metadata. |
| `transition_issue` | Apply a transition ID with optional required fields. |
| `list_comments` | Read one page of comments with raw ADF bodies. |
| `add_comment` | Add a plain-text comment converted to Atlassian Document Format. |
| `health_check` | Read current account identity. |

Search requires a bounded JQL query such as `project = DEMO ORDER BY updated DESC`. It accepts `limit` (1–100, default 50) and `next_page_token`. Continue using the returned token until `is_last` is true; keep JQL unchanged. Search can be eventually consistent immediately after writes. The removed legacy search route is not used.

Comments use `start_at` (default 0), the same page-size range, and return `next_start_at`, `has_more` and `total`. List operations return a single page. All issue/comment timestamps are returned unchanged. Core issue outputs include ID, key, summary, description ADF JSON, status, assignee account ID, labels, updated time, due date and priority ID. Arbitrary custom fields are not included in issue read outputs.

## Writes and custom fields

```json
{"project_key":"DEMO","issue_type_id":"10001","summary":"Review earnings report","details":{"description":"Prepare the summary.","labels":["research"]},"custom_fields":{"customfield_10020":"Example"}}
```

Obtain real issue type, priority, custom-field and account IDs from your Jira configuration; the example IDs are placeholders. `details`/`changes` support `summary`, `description`, `assignee_account_id`, `labels`, `due_date` and `priority_id`. Create uses the top-level summary only. Description and comment text become ADF paragraphs, preserving line breaks as separate paragraphs; Markdown syntax is not rendered.

On update, omitted fields are preserved. Empty description clears it; empty assignee unassigns; `labels: []` removes labels; empty due date clears it. Due dates otherwise use `YYYY-MM-DD`. Summary and priority cannot be cleared. JSON null is omitted for typed optional fields; use empty strings or arrays as documented.

`custom_fields` accepts only `customfield_<number>` keys with tenant-specific JSON values. JSON null is preserved here for clearing where Jira allows it. The SDK binds opaque JSON numbers as float64; pass large integer identifiers as strings to avoid precision loss. Built-in fields cannot be overridden through this object. Jira validates field context, required fields and edit screens. Metadata discovery is outside this version.

Call `list_transitions` first. Its `fields_json` is the upstream metadata JSON; supply required transition fields using their Jira field shapes, such as `{"resolution":{"id":"10000"}}`. This dynamic `fields` object is intentionally passed through and is also subject to the float64 numeric limitation. `update_issue` does not change status; use `transition_issue`. Successful update/transition responses return `ok: true` after Jira accepts the request.

## Limits and errors

Operations have a 60-second platform timeout and 55-second HTTP timeout, with 4 MiB request and 8 MiB response limits. Errors identify status categories without echoing response bodies, tokens, email addresses or request URLs. An empty result array is valid; a missing collection is rejected. No automatic retry is performed, including for rate limits. After an ambiguous timeout during a write, inspect Jira before retrying.

This version does not delete issues, edit/delete comments, manage attachments, discover create/edit metadata, or provide event triggers. Synthetic fixtures test request contracts, pagination, ADF, credential isolation and errors. No live-account acceptance has been performed.
