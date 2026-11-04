# Typeform

Read workspaces, form definitions and completed responses using the Typeform API. This version is read-only and uses a manually supplied personal access token.

## Credentials and region

Create a token in your Typeform account settings. Save the raw value as **Access token**, without a Bearer prefix. Grant `accounts:read` for health, `workspaces:read` for workspace operations, `forms:read` for form operations and `responses:read` for responses. Access remains subject to account and workspace permissions. Replace expired or revoked tokens externally; this plugin does not implement OAuth or token renewal.

The API origin is fixed to `https://api.typeform.com`. Accounts using the EU data center (`api.eu.typeform.com`) are outside this version's supported setup. No arbitrary base URL, redirect or external file fetch is allowed.

**Check credentials** reads `/me` and validates the returned account email. It returns `ok=false` for invalid credentials, missing account scope or an unusable response. A successful check proves account identity only; it does not prove access to each workspace or form.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Verify account identity without writing. |
| `list_workspaces` | Read a page of accessible workspaces, optionally searched by name. |
| `get_workspace` | Read one workspace's identity, account ID and shared flag. |
| `list_forms` | Read a page of form summaries, optionally filtered by workspace or search. |
| `get_form` | Read a form's questions and hidden-field names. |
| `list_responses` | Read a page of completed responses with optional time and answer filters. |

Use IDs returned by discovery, not URLs. `list_workspaces` and `list_forms` accept `page.page` (1-based, maximum 1,000,000) and `page.page_size` (1–200, default 100). Pass `next_page` as the next page number; zero means the upstream page count indicates completion. Search text is limited to 1,024 UTF-8 bytes. Lists are not snapshots: concurrent account changes can alter their counts and ordering.

## Form and answer shapes

Form summaries include IDs, titles, available creation/update timestamps and public display URLs. `get_form` returns typed top-level questions with ID, reference, title and type. A question's `properties` and `validations` remain dynamic because their keys vary by question type; group properties can contain recursively nested fields. Empty forms return an empty question array. Theme definitions, logic, translations and full editing payloads are not exposed.

Each response includes its cursor `token`, optional `response_id`, timestamps, answers, hidden values and calculated values. These identifiers are not assumed interchangeable. Each answer exposes `type`, question metadata and `value`: the latter is the value under the upstream answer's type key. Boolean false, numeric zero and empty text are preserved. Choice/payment payloads remain structured objects. Unknown future answer types can be returned if their matching value key exists; missing typed values fail explicitly. Calculated values remain dynamic. Respondent device metadata and form variables are not projected.

File-upload answers return their URL value; the plugin does not download them or send credentials to those URLs.

## Response pagination and freshness

`list_responses` always sends `response_type=completed`. Started and partial submissions are outside this version. `options.page_size` accepts 1–1,000 (default 100); `before` accepts the response cursor token returned by a previous page. Use `next_before` for the next call with unchanged form and filters. The cursor is based on the last returned response token, not on the response ID or a URL. No custom sort is sent: cursor pagination can use processing order, which may differ from the default `submitted_at` ordering. It is not a snapshot or an exactly-once checkpoint.

`may_have_more` is true when a page is full or the upstream page count reports more pages. A full final page may require one extra empty request. Repeated cursor boundaries or inconsistent empty continuation pages fail explicitly. This plugin does not loop or persist checkpoints.

Optional `since` and `until` take RFC3339 timestamps with whole-second precision. They are inclusive and normalized to UTC before transmission. `query` searches an answer/hidden/variable phrase and is limited to 1,024 bytes. Typeform warns that very recent responses, around the last 30 minutes, may be absent from this API. Use overlapping time windows and deduplication for scheduled ingestion. This plugin does not create webhooks or claim real-time delivery.

## Runtime limits and validation

HTTP requests time out after 45 seconds; normal operations have 60-second platform timeouts and health has 30 seconds. JSON requests are limited to 1 MiB and responses to 8 MiB. Credentials are sent only in the Authorization header to the fixed origin. Redirects and automatic retries are disabled. A 429 reports `Retry-After` or a 60-second fallback. Errors omit upstream bodies, tokens and transport URLs. Invalid business payloads and missing collections fail instead of returning success.

Tests use synthetic fixtures based on official documentation, not live account captures. They verify request bytes, generated binding, pagination, numeric precision, false/empty answer values, time filters, invalid payloads, size limits and cancellation. Live account acceptance has not been performed.

## Official references

- [Account identity and authentication](https://www.typeform.com/developers/get-started/hands-on/)
- [Scopes](https://developer.typeform.com/developers/get-started/scopes/)
- [List forms](https://www.typeform.com/developers/create/reference/retrieve-forms/)
- [Read form](https://www.typeform.com/developers/create/reference/retrieve-form/)
- [Workspaces](https://www.typeform.com/developers/create/reference/retrieve-workspaces/)
- [Responses reference](https://www.typeform.com/developers/responses/reference/retrieve-responses/)
- [Response cursor walkthrough](https://www.typeform.com/developers/responses/walkthroughs/)
