# Sokel for Todoist

Manage projects and active tasks through the current Todoist `/api/v1` API. This integration is not created by, affiliated with, or supported by Todoist.

## Credential

Copy a personal API token from your Todoist integration settings into **Personal API token**. The plugin sends Bearer authentication only to `https://api.todoist.com/api/v1`. It does not use the retired REST v2 or Sync v9 endpoints, implement an OAuth provider, or refresh tokens. Replace revoked tokens externally. Account-plan and shared-project permissions still apply.

The token needs read access for project/task queries and user identity, and write access for creation, updates and completion. If an externally obtained scoped OAuth token is used, check the upstream `data:read`/`user:read`, `data:read_write` and completion permissions applicable to that token; token acquisition and scope negotiation are outside this plugin. `health_check` reads `GET /user` and returns `ok` and `message`, without changing account data. This verifies identity, not all project permissions.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Check the current user's identity. |
| `list_projects` | Return one page of active projects. |
| `get_project` | Read a project by its server-confirmed ID. |
| `create_project` | Create a project with name and optional description, parent, favorite flag and view style. |
| `list_tasks` | Return one page of active tasks, optionally filtered by project, section, parent task and label. |
| `get_task` | Read an active, non-completed task. |
| `create_task` | Create a task with required content and optional task details. |
| `update_task` | POST only supplied fields to an existing task. |
| `close_task` | Complete a regular task, or advance a recurring task to its next occurrence. |

Lists accept `limit` from 1 to 200 (omitted or 0 uses 50) and an opaque `cursor`. Copy `next_cursor` into the next call, preserving other filters. `has_more=false` marks the end. Each invocation sends one request; no returned URL is followed. Use actual server IDs, not entire URLs or temporary IDs beginning with `tmp-`. Completed tasks, deletions, reminders, attachments, comments, workspace administration and Sync commands are outside this release.

## Creating and changing tasks

For example, create `content: Buy milk`, with `details.project_id` set to a project ID and `details.due_date: 2030-01-02`. Omit project to use the Inbox. Optional create fields are `description`, `project_id`, `section_id`, `parent_id`, `order`, `labels`, `priority`, `due_date` and `due_datetime`.

Update fields are `content`, `description`, `labels`, `priority`, `due_date`, `due_datetime`, `child_order` and `is_collapsed`. At least one field must be supplied. Explicit `false`, order `0`, an empty description and `labels: []` are preserved. Omitted fields keep their existing values. Labels replace the whole label list; an empty list clears it. This release does not expose null-based clearing of assignees, durations or deadlines, or date removal.

Use either a `YYYY-MM-DD` due date or an RFC 3339 timestamp with an offset, not both. Timestamps are normalized to UTC. Natural-language due parsing is outside this release. Numeric priority values 1–4 are sent unchanged. The current official specification is inconsistent: request descriptions call 1 highest, while the task response describes 1 normal and 4 urgent. The plugin does not invert values; verify the intended priority against your account before automation.

Order values use signed 32-bit integers. Content is limited to 10,000 characters, labels to 100 entries of up to 512 characters, and project names to 120 characters (plugin limits; account limits may be lower). Project view style supports `list`, `board`, `calendar` and `table`, subject to account availability. An explicit `is_favorite: false` is sent.

`close_task` uses `POST /tasks/{task_id}/close`, expects HTTP 200 with JSON null, and returns `accepted=true`. For recurring tasks this advances the occurrence; it does not permanently close the series. Regular tasks and their subtasks move to history. Calling close again can affect another recurrence, so inspect state before repeating an uncertain call.

## Limits and verification

Operations allow 60 seconds; HTTP timeout is 50 seconds, request limit 1 MiB, response limit 8 MiB. The plugin blocks redirects, validates cursors, sanitizes errors, and never retries automatically. Creation is not made idempotent by this plugin. A timeout may occur after a write was applied: inspect the account before retrying. Todoist quotas and plan restrictions remain in force.

Fixtures are synthetic adaptations of official API schemas/examples, not captured account responses. No real Todoist account was accessed and no task was created or completed during development. Production acceptance should check actual token permissions, pagination, priority mapping and controlled create/update/complete behavior.

## Official reference

- [Todoist API v1: project, task, user, pagination and migration reference](https://developer.todoist.com/api/v1/)

The reference was checked on 2026-11-04. SDK v0.7.8 runtime `SetDoc` uploads the English guide without locale dictionaries. Registry installation packages the full Chinese translation from `locales/zh-CN.json`; a separate Chinese guide is available in this repository.
