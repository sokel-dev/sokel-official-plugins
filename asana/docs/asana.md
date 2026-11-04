# Asana

Browse workspaces and projects, read project tasks, create/update tasks and add comments through `https://app.asana.com/api/1.0`.

## Credential and access

Create a personal access token in Asana's developer console and enter it as `access_token`. It is sent as a Bearer token. The connector has no OAuth consent or refresh flow; replace expired/revoked tokens externally. Resource access follows the token owner's permissions. `health_check` reads `/users/me`; successful identity verification does not grant access to private projects.

The corresponding API scopes are `users:read` for health, `workspaces:read`, `projects:read`, `tasks:read`, `tasks:write`, and `stories:write` for comments. A personal token must still have access to the target workspace/project.

## Discovery and pagination

Use `list_workspaces`, then `list_projects` with `workspace_gid`, then `list_tasks` with `project_gid`. These operations return one page. `page.limit` defaults to 50, accepts 1–100, and `page.offset` is an opaque pagination token. While `has_more=true`, pass `next_offset` to the same operation with the same resource and filters. Tokens may expire; restart traversal if that happens. Only the offset is reused; provider-supplied continuation URLs are never fetched. Pagination is not a consistent snapshot during concurrent changes.

All resource GIDs are strings of digits. `get_task` reads one task. Task output includes notes, completion state, due date, assignee, creation/update times and URL. Missing optional dates become empty strings; an unassigned task has a null assignee.

## Writes

`create_task` requires `workspace_gid` and top-level `name`; optional `project_gids` attaches it to projects in that workspace. Optional details support notes, completion, `assignee_gid` and `due_on`. Do not repeat name inside details. `assignee_gid` may be a numeric GID or `me`.

`update_task` requires `task_gid` and at least one field in `changes`. Omitted fields are unchanged. Explicit `completed=false` reopens a task. Empty `assignee_gid` or `due_on` sends JSON null to clear that field; empty notes clears notes. A supplied date must be `YYYY-MM-DD`. Empty task names are rejected.

```json
{"task_gid":"123456789","changes":{"completed":false,"assignee_gid":"","due_on":"","notes":""}}
```

`add_comment` creates a top-level text story for a task. It returns the story GID, text, creation time and subtype. Thread replies, attachments, custom fields, deletion and automatic triggers are outside this version.

## Limits and validation

JSON requests and responses are bounded to 8 MiB. Requests use a fixed origin and never follow redirects or automatically retry writes. HTTP/rate-limit errors omit sensitive upstream bodies. After uncertain write results, inspect the task/comments before retrying to avoid duplicates. Malformed success responses or pagination tokens fail explicitly.

Tests cover synthetic request/response fixtures, clearing fields, pagination and generated platform binding. They are not live account acceptance.

References: [personal access tokens](https://developers.asana.com/docs/personal-access-token), [task listing](https://developers.asana.com/reference/gettasks), [task updates](https://developers.asana.com/reference/updatetask), [comments](https://developers.asana.com/reference/createstoryfortask).
