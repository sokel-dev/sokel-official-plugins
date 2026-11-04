# Linear

Connect a Linear workspace with a personal API key.

## Credential and permissions

Create a personal API key in Linear's account API settings and save it as `api_key` in a Sokel credential. Give the key access only to the teams and read/write operations needed by your workflow. This plugin uses Linear's personal-key Authorization format, not an OAuth access token. Organization policy may restrict personal keys.

`health_check` runs a readonly GraphQL viewer query. It returns `ok` and a sanitized `message`; a successful identity check does not prove access to every issue or team. The key is sent only to `https://api.linear.app/graphql`. Redirects are rejected.

## Operations

| Operation | Behavior |
| --- | --- |
| `list_teams` | Read accessible team IDs, keys and names. |
| `list_issues` | Read one page, optionally filtering by team, project, assignee, state or a case-insensitive title substring. |
| `get_issue` | Read an issue by UUID or identifier such as `ENG-123`. |
| `list_projects` | Read one page, optionally matching a case-insensitive name substring. |
| `create_issue` | Create with `team_id`, `title` and optional `details`. |
| `update_issue` | Modify only fields supplied in `changes`. |
| `health_check` | Check authenticated viewer access without writing. |

List operations accept `limit` (1–100, default 50) and `cursor`. Pass `next_cursor` to the next call while `has_more` is true, keeping filters unchanged. Each call returns one page; archived resources use Linear's default exclusion. This is filtered listing, not global full-text search.

Issue outputs include IDs, identifier, title, Markdown description, URL, priority, due date, update time, team, state, assignee and project. Project outputs include ID, name, description, URL, update time and target date. Timestamps are returned as supplied by Linear. Date inputs use `YYYY-MM-DD`.

## Creating and updating

First call `list_teams` to obtain a team UUID. A create input can be:

```json
{"team_id":"<team-uuid>","title":"Review earnings report","details":{"description":"Prepare the summary.","priority":2}}
```

`details` and `changes` support `title`, `description`, `state_id`, `assignee_id`, `project_id`, `priority`, and `due_date`. For create, put title only at the top level. State, assignee and project values are API IDs, not display names. State discovery is not included; obtain a valid state ID from an existing issue or Linear's API.

Omitted fields are unchanged on update. Priority `0` explicitly removes priority; values 1–4 mean urgent, high, normal and low. Empty description clears it. Empty assignee, project or due date clears that value. Title and state cannot be cleared. JSON `null` is treated as omitted by the typed contract; use the documented empty string for clearing.

```json
{"issue_id":"ENG-123","changes":{"priority":0,"assignee_id":"","description":""}}
```

## Limits and errors

Operations have a 60-second platform timeout and 55-second HTTP timeout. Requests are limited to 4 MiB and responses to 8 MiB. HTTP failures and GraphQL errors produce sanitized English errors; upstream messages and credentials are not included. Partial GraphQL responses with errors are rejected. A missing list is an error, while an explicit empty list is valid.

There are no automatic retries. After a timeout or an unconfirmed mutation, inspect Linear before retrying to avoid duplicate issues. Rate limits require waiting before retrying. This version does not create projects, upload attachments, manage comments, or provide event triggers. No live-account acceptance has been performed; current checks use synthetic protocol fixtures.
