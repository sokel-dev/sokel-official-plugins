# Browser Use Cloud

Run hosted Browser Use V4 agents from workflows. This connector uses the public REST API at `https://api.browser-use.com`; it does not install a local browser, Python agent or CDP runtime. EU endpoints and legacy V2/V3 tasks are outside this version.

## Credentials and connection check

Create a Browser Use Cloud project API key and save it in `api_key`. The key is sent as `X-Browser-Use-API-Key` without a Bearer prefix. Keys, project funding, model availability and spending limits remain managed in Browser Use.

`health_check` performs `GET /api/v4/runs?limit=1`. It returns `ok` and `message`, verifies an authenticated read and does not create browser work. A successful check does not prove that the project can fund a run or use a particular model.

## Start, poll and extract results

1. Call `create_run` with a task and a positive `max_cost_usd`. Creation starts asynchronous work and can incur charges or change external websites. The cost cap is passed to the service; it is not a promise of a total project budget or of rollback. Omit `model` to retain the service default.
2. Save the returned run ID, session ID and workspace ID. A creation receipt means that the request was accepted, not that the task finished. Reuse a V4 session ID for a follow-up after its previous run ends; a busy session can return 409. Workspace and session IDs are not interchangeable.
3. Use `get_run_status` until `terminal` is true. Poll with a bounded workflow loop, a deadline and at least a few seconds between calls. The terminal states are `completed`, `failed` and `cancelled`; the other states are `queued`, `dispatching` and `running`.
4. Call `get_run` to retrieve the result. Inspect `status` and `error`. `result` is text; `output_json` is the serialized arbitrary JSON value from structured output, including arrays, scalars and null. Parse and validate it against your schema. A completed agent run is not independent proof that an external business action succeeded.

Example operation inputs (creating this run can incur charges):

```json
{"task":"Read https://example.com and return its page title. Do not submit forms.","max_cost_usd":0.5,"output_schema":{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}}
```

`output_schema` is an arbitrary JSON Schema object, validated by Browser Use. The service currently limits it to 64 KiB, 64 nesting levels and approximately 4,000 values, with internal references only. The plugin sends no model parameters, stored credentials, secret bindings, payment wallet options or browser settings.

## Lists, events and cancellation

`list_runs` reads one page, optionally filtered by session. The plugin supports 1–100 rows, defaults to 50, and returns the opaque `next_cursor` and `has_more`. Pass the cursor unchanged with the same filters while `has_more` is true. Do not infer completion from a short or empty page.

`list_run_events` reads one ordered page for a run. Start `after` at 0 and pass back `next_after`; use the same `include_output` setting throughout pagination. The plugin supports 1–1,000 events per page and defaults to 200. Drain pages while `has_more` is true, even after the run reaches a terminal state. Event data is an event-specific JSON object. There is no automatic event source, background polling, durable queue or delivery guarantee in this connector.

`cancel_run` sends one cancellation request and returns the service's run summary. Already terminal runs are returned as-is. Cancellation does not undo completed actions, delete session data or guarantee that the browser has already stopped. Browser lifecycle and charges remain service-controlled.

## Errors and boundaries

HTTP 401/403 require credential or permission correction; 402 may indicate funding or spending limits; 409 may indicate a busy session; 429 includes a sanitized wait duration from `Retry-After` when available. No request is automatically retried. After a timeout or uncertain creation receipt, inspect existing runs and external state before creating duplicate work. A local timeout does not cancel server-side work.

Requests use only the fixed global API origin; redirects are rejected. No returned URL is fetched. JSON requests are limited to 1 MiB, responses to 4 MiB, and tasks to 65,536 bytes. Large result/event pages can exceed the response bound; use a smaller page or reduce requested output. Upstream HTTP error bodies and credentials are not included in errors. Task results and event payloads can contain sensitive content supplied by the task and should be handled accordingly.

## Official references and validation

- [V4 API](https://docs.browser-use.com/cloud/api-v4-overview)
- [Create run](https://docs.browser-use.com/cloud/api-v4/runs/create-run), [list runs](https://docs.browser-use.com/cloud/api-v4/runs/list-runs), [get run](https://docs.browser-use.com/cloud/api-v4/runs/get-run)
- [Status](https://docs.browser-use.com/cloud/api-v4/runs/get-run-status), [events](https://docs.browser-use.com/cloud/api-v4/runs/get-run-events), [cancel](https://docs.browser-use.com/cloud/api-v4/runs/cancel-run)
- [Structured output](https://docs.browser-use.com/cloud/agent/structured-output), [sessions](https://docs.browser-use.com/cloud/agent/sessions)

Validation uses synthetic HTTP fixtures derived from these public contracts. No real paid run or live service acceptance is claimed. Any first-release catalog fixture is temporary test input only, not a published image or verified registry digest.
