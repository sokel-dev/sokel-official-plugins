# Gitea

Connect workflows to your own Gitea instance to read repositories, track issues and create pull requests. This plugin targets Gitea 28.3.0 and its `/api/v1` REST API. Gitea is MIT licensed. No hosted account or public Gitea endpoint is required.

## Configure the service

Set the deployment environment variable `SOKEL_GITEA_URL` to the Gitea **site root**, for example `http://gitea:3000` or `https://git.example.org/team`. Do not append `/api/v1`. HTTP is supported for trusted internal networks; use HTTPS when traffic crosses an untrusted network. A reverse-proxy path prefix is preserved. The URL cannot contain credentials, query parameters, fragments, encoded path segments or `.` / `..` segments. There is no public-service fallback.

Add an **API token** in the plugin credential form. Generate it in Gitea under User Settings → Applications → Manage Access Tokens. The plugin sends `Authorization: token <token>` only to the configured service. It does not implement OAuth login, token creation or token refresh. Choose only the scopes your workflow needs: `read:user` for health checks, repository access for repository operations, and issue access for issue and pull-request operations. Write operations require the corresponding write scopes and the account's repository permissions; `write:repository` and `write:issue` cover this plugin's repository collaboration features. Token scopes do not grant permissions the account lacks. A health check reads `/api/v1/user`; success verifies the token, not access to every repository.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Returns `ok` and `message` after reading the authenticated user. |
| `list_repositories` | Lists repositories accessible to the token's user. Optional `pagination.page` and `pagination.limit`; returns `repositories` and `next_page`. |
| `get_repository` | Takes `owner` and `repository`; returns the repository ID, full name, visibility, default branch and web URL. |
| `list_issues` | Takes the repository, optional `state` (`open`, `closed`, `all`) and pagination. Defaults to open issues and explicitly excludes pull requests. |
| `get_issue` | Takes the repository and decimal `number`. Gitea shares issue numbering with pull requests; inspect `is_pull_request`. |
| `create_issue` | Takes the repository and `title`. Optional `options.body`, `closed`, `assignees` and `label_ids` are forwarded only when supplied. |
| `add_comment` | Takes the repository, issue or pull-request `number`, and nonempty `body`. Creates one comment. |
| `list_pull_requests` | Takes the repository, optional `state` and pagination. Defaults to open pull requests. |
| `create_pull_request` | Takes the repository, `title`, `head`, `base` and optional `body`. `head` may be a branch name or `owner:branch` for a fork. Branches must already exist with changes that Gitea can compare. |

Repository names and owners are individual names, not URLs or `owner/repository` paths. Unicode letters and digits, `_`, `-` and `.` are supported. IDs, label IDs and issue or pull-request numbers use decimal **strings**, including values above JavaScript's safe integer range. They must be positive 64-bit integers without leading zeros.

An issue creation input can be:

```json
{"owner":"team","repository":"project","title":"Review report","options":{"body":"Review the attached report.","closed":false,"assignees":["reviewer"],"label_ids":["12"]}}
```

An explicitly supplied `closed: false` remains false on the wire. Omitted options are not filled with false or zero. Titles are limited to 255 UTF-8 bytes; descriptions and comments to 1 MiB. Up to 100 assignees and 100 label IDs are accepted per issue. Creating issues, comments or pull requests writes immediately and can trigger the Gitea instance's notifications, webhooks or automations. This plugin does not merge pull requests, create repositories, manage users or run administration operations.

## Continue a list

Pages start at 1; the default and maximum page size is 50. Valid page numbers are 1–100000. Keep the repository, state and page size unchanged, and pass the returned `next_page` as `pagination.page` until it is 0. The plugin uses Gitea's `Link` header and checks `X-Total-Count`; it does not infer completion from a short page. A next URL must retain the configured origin, endpoint, filters and page size and advance exactly one page. Only the next page number is exposed. Missing required pagination headers produce an error, so configure the reverse proxy to preserve them.

Lists are not snapshots. Concurrent changes can move results between pages; deduplicate by returned object ID when needed. Each call returns one page and never automatically fetches an entire collection.

## Failures and deployment limits

Redirects are rejected, cookies are not reused, and credentials are never forwarded to another host. Service error bodies and transport URLs are excluded from error messages. Requests have no automatic retry loop, failed HTTP responses are not retried, and mutation bodies cannot be replayed automatically. On a timeout after a write, inspect Gitea before repeating it; these create operations have no plugin-provided idempotency key. A 429 error includes a bounded wait suggestion from `Retry-After` but does not schedule another request.

JSON responses are limited to 8 MiB and operation timeouts are 60 seconds. The plugin does not download repository files, clone Git repositories or fetch arbitrary URLs. The self-hosted Gitea service may have its own outbound integrations; those remain under your deployment's control.

## Validation and references

The integration was exercised against an isolated Gitea 28.3.0 service with synthetic repositories, issues, comments and a pull request, including two-page issue reads. Offline tests also cover captured responses, generated dispatch, Unicode names, precise IDs, optional false, bad pagination, redirect refusal and error redaction. This is local service acceptance, not a deployment to a production platform.

Official sources: [Gitea 28.3.0 release](https://github.com/go-gitea/gitea/releases/tag/v28.3.0), [pinned API specification](https://github.com/go-gitea/gitea/blob/v28.3.0/templates/swagger/v1-swagger.generated.json), [API authentication and pagination](https://docs.gitea.com/development/api-usage/), and [MIT license](https://github.com/go-gitea/gitea/blob/v28.3.0/LICENSE).
