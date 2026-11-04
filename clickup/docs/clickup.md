# ClickUp

Discover workspaces, spaces, folders and lists; read, create and update tasks; add task comments through the fixed ClickUp v2 API origin.

## Credential

Generate a personal API token in ClickUp settings and paste it into `access_token`. The connector sends the raw token in `Authorization` as required for personal tokens. It does not implement an OAuth authorization or refresh flow. Tokens have the signed-in user's access; workspace permissions still apply. `health_check` calls `/user` and verifies an authenticated user, not access to every list.

## Operations and discovery

`list_workspaces` → `list_spaces` → `list_folders` → `list_lists` locates task lists. `list_lists` with `parent_type=space` returns only folderless lists; use `parent_type=folder` for lists in each folder. Folder discovery returns a flat collection of IDs and names, not a nested tree. These discovery endpoints use their standard active-resource defaults.

`list_tasks` accepts `list_id` and optional `options`: `page` starts at zero; `archived`, `include_closed`, `include_timl`, and `subtasks` preserve explicit false. A page contains at most 100 tasks. Continue using `next_page` only while `has_more=true`, retaining all filters. If the provider omits `last_page`, a full 100-row page indicates a possible next page, so one extra empty request may be needed. Pagination is not a snapshot. By default, only tasks whose home list matches the requested list are returned; use `include_timl=true` to include tasks added from other home lists.

`get_task`, `create_task`, `update_task` and `add_comment` use native task IDs, not custom IDs. Create requires a list and top-level name. Do not also set `details.name`; `details.archived` is update-only and is rejected on create.

Update requires at least one changed field. Omitted fields remain unchanged. Supported fields are name, description, status, priority, `due_date_ms`, `due_date_time` and archived. Priority accepts only 1–4. Due dates must be positive Unix millisecond timestamps. Zero-based clearing of priority/due dates is not documented by the upstream API and is rejected. Supply `due_date_time=true` together with a due date to retain a time component; false or omission means a date-only value, which ClickUp normalizes to 4 a.m. in the user's time zone. An empty description sends the single space required by ClickUp to clear it. Explicit `archived=false` is sent. Custom fields and assignee changes are outside this version. Optional task due dates and priorities can be empty/null in results.

```json
{"task_id":"abc123","changes":{"description":"","priority":3,"archived":false}}
```

`add_comment` requires task ID and text. `notify_all` defaults to false; true additionally notifies the comment author. Assignees and watchers can still be notified when false. Comment IDs are returned as strings to preserve large integers.

## Reliability

No automatic retries or redirects. JSON is bounded to 8 MiB. Errors do not echo upstream bodies or credentials. After a write timeout or invalid receipt, inspect the task before retrying; task creation and comments are not idempotent. Rate limits must be handled by delaying subsequent workflow calls. No triggers or webhooks are registered.

Tests use synthetic public-protocol fixtures and generated input/output binding, not live account traffic. Real account acceptance remains pending.

References: [authentication](https://developer.clickup.com/docs/authentication), [list tasks](https://developer.clickup.com/reference/gettasks), [update task](https://developer.clickup.com/reference/updatetask), [create comment](https://developer.clickup.com/reference/createtaskcomment), [official OpenAPI](https://developer.clickup.com/openapi/clickup-api-v2-reference.json).
