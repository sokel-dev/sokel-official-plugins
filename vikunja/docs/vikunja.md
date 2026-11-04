# Vikunja

Use your own Vikunja server to discover projects, read and create tasks, update selected task fields and add comments. No cloud account or paid feature is required. This plugin uses API v2, introduced in Vikunja 2.4.0, and was tested against an isolated community Vikunja 2.7.1 container. It does not fall back to legacy API v1.

## Deployment and authentication

Set the required deployment environment variable `SOKEL_VIKUNJA_URL` to the server root, for example `http://vikunja:3456` or `https://tasks.example.internal/vikunja`. Do not append `/api/v2`; the plugin adds it. Internal HTTP and reverse-proxy path prefixes are supported. There is no default server or public fallback. URLs must not contain embedded credentials, query parameters, fragments or dot path segments. Use HTTPS when traffic crosses an untrusted network.

Inside a container, `localhost` refers to that container. Use the service's reachable container-network name or an appropriate host address. The plugin does not install or start Vikunja or its database.

Create a scoped API token in Vikunja under Settings → API Tokens and enter it as **API token**. Requests send `Authorization: Bearer ...`. Grant the required project, task and task-comment permissions; reading, creating and updating are separate capabilities. A partial update needs the permissions used by Vikunja's PATCH implementation, including read and update access to the target task. The local acceptance used a scoped `tk_` token with the relevant `projects`, `tasks` and `tasks_comments` permissions.

Rotate tokens and manage expiry outside the plugin. No OAuth provider, automatic login, account creation or token refresh is included. A deployment may accept a manually supplied bearer JWT, but its lifetime remains your responsibility; a scoped API token is the intended setup.

`health_check` reads a project page and returns `ok` and `message`. Configuration, transport or authorization failure returns `ok=false`. It confirms project-list access, not every write permission, and does not create data. A token with only task access may fail this check even if particular task operations are permitted.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Verify the configured service and token's project-list access. |
| `list_projects` | Optional `page`; return project metadata, string `total`, `next_page` and `has_more`. |
| `get_project` | `project_id`; return title, description HTML and archive flag. |
| `create_project` | `title` and optional `description`; create a normal project and return it. Vikunja creates its default views and buckets. |
| `list_tasks` | `project_id` and optional `page`; return one flat task page and pagination metadata. |
| `get_task` | `task_id`; return the task's actual project ID, title, description HTML, completion, priority and due date. |
| `create_task` | A positive `project_id`, `title` and optional `fields`; create a task in that normal project. |
| `update_task` | `task_id` and nonempty `fields`; apply a JSON Merge Patch and return the resulting task. |
| `add_comment` | `task_id` and `comment` HTML; create a comment and return its ID, body and creation timestamp. |

Project and task IDs are canonical decimal **strings** in Sokel, preserving signed 64-bit precision. Do not convert them through floating-point values. Task IDs and project IDs used for creation must be positive. No project or task deletion operation is exposed in this release.

## Normal and virtual projects

Vikunja exposes saved filters and Favorites as virtual projects with negative IDs. `-1` denotes Favorites; saved filter N is represented as `-N-1`. Project listing and `get_project` preserve these IDs, and `list_tasks` accepts them for reads. Tasks returned by a virtual project still report their actual positive `project_id`.

You cannot create a task inside a virtual project through this plugin. Choose a real positive project ID. Updating a task obtained through a virtual project changes the actual task and may make it stop matching that filter.

The real 2.7.1 server appends virtual projects to the paginated normal projects. Its `total` and page count cover normal projects only. A project page can therefore contain more entries than the requested size, and the same virtual project may appear on multiple pages. Keep virtual entries when useful, deduplicate all project results by ID, and use `has_more`/`next_page` rather than the number of returned items to decide whether to continue. A total of zero can still accompany virtual entries.

## Task fields and HTML

Creation accepts these optional `fields`: `description`, `done`, `priority` and `due_date`. Updates accept the same fields plus `title`. Omitted fields remain unchanged on PATCH; at least one field must be supplied. Explicit `done=false`, `priority=0` and an empty description are preserved.

```json
{
  "task_id": "123",
  "fields": {
    "done": false,
    "priority": 0,
    "description": "<p>Updated details</p>",
    "due_date": "2027-01-01T12:00:00Z"
  }
}
```

Titles are limited by this plugin to 250 UTF-8 bytes and cannot be blank. Descriptions and comments are limited to 256 KiB. Priority accepts the usual UI range 0–5, where zero represents no priority; the wider upstream integer field is intentionally restricted here. Due dates must be RFC3339 timestamps with a timezone. Use `"0001-01-01T00:00:00Z"` to clear a due date; an empty string is rejected.

Descriptions and comments are exchanged as HTML, not Markdown. This release does not enable the Markdown conversion mode. Treat returned content as HTML and sanitize it appropriately in any downstream renderer. The server may normalize submitted HTML. Comment mentions and task changes can trigger Vikunja notifications according to server settings. Repeating tasks may reopen themselves and change dates when marked done; use the returned task state instead of assuming the requested flag remained set.

Creates require the documented HTTP 201 resource response. Updates require HTTP 200 and a matching task ID. Missing metadata, mismatched IDs or malformed response bodies fail explicitly; an empty response is not treated as success. PATCH changes only the exposed fields; assignees, labels, reminders, recurrence and other task properties are not replaced by this plugin. It does not provide optimistic concurrency conditions, so concurrent writers may still overwrite changes to the same field.

## Pagination and scope

`page.number` is 1–1000000, default 1. `page.size` is 1–1000, default 50. The server may cap the effective page size; continuation uses the response metadata and accepts this cap. Optional `page.query` is a search string up to 16 KiB, sent as API v2's `q` parameter.

Each invocation fetches one page. While `has_more=true`, copy `next_page` to `page.number` and keep other inputs unchanged. Completion returns zero. Totals are decimal strings. Inconsistent page metadata, repeated resource IDs within a page and a continuation that cannot make progress fail explicitly. Concurrent changes can alter results; pagination is not a snapshot.

Project listing uses the server's default exclusion of archived projects. Task listing returns the endpoint's flat results; it does not reproduce a particular Kanban/Gantt view or apply a view's filters. This release does not manage views, labels, assignees, attachments, recurrence, reminders, subscriptions, webhooks or accounts, and does not run arbitrary API requests.

## Limits and failures

Requests are bounded to 1 MiB and responses to 8 MiB. Calls have a 45-second transport timeout within a 60-second operation deadline. The plugin uses only the deployment URL, rejects redirects, does not retry failed HTTP responses and disables mutation-body replay. Errors omit raw response bodies, service URLs and credentials.

Check token scopes and project/task access after 401/403, and field values after 422. After a timeout, inspect the target resource before repeating a write; the original operation may already have been applied. Self-hosting these operations needs no third-party service, but notifications or integrations configured separately in Vikunja can introduce external dependencies.

Tests include documentation-derived synthetic wire cases and real request/response captures from the disposable 2.7.1 instance. The real acceptance exercised all nine operations, pagination with virtual projects, and clearing false/zero/date values. It used a fresh local test account and removed its temporary project; no production account or data was accessed.

## Official references

- [Vikunja 2.7.1 release](https://github.com/go-vikunja/vikunja/releases/tag/v2.7.1)
- [AGPL-3.0 license](https://github.com/go-vikunja/vikunja/blob/v2.7.1/LICENSE)
- [API v2 and migration semantics](https://vikunja.io/docs/api-v2/)
- [API documentation and tokens](https://vikunja.io/docs/api-documentation/)
- [Tagged project API routes](https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/routes/api/v2/projects.go)
- [Tagged normal and virtual project pagination](https://github.com/go-vikunja/vikunja/blob/v2.7.1/pkg/models/project.go)
- [Saved filters](https://vikunja.io/help/saved-filters/)

For exact endpoint definitions, read `/api/v2/docs` or `/api/v2/openapi.json` on your own instance. The public demo may run a development build; this plugin's real acceptance used the released 2.7.1 specification.
