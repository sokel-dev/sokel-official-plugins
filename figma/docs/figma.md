# Figma

Read file metadata and node summaries, obtain rendered-image URLs, browse folders, and read or post comments.

## Credential and scopes

Create a Figma personal access token outside this plugin and enter it in **Personal access token**. Requests use `X-Figma-Token` against `https://api.figma.com` only. The plugin does not exchange or refresh tokens; replace expired tokens externally. OAuth and plan tokens are outside this release.

Enable `current_user:read` for the read-only `health_check`, `file_metadata:read` for file metadata, `file_content:read` for nodes and image rendering, `folders:read` for folder browsing, `file_comments:read` for reading comments, and `file_comments:write` for posting them. Your token owner must also have resource access. Figma plan, seat, approval and rate-limit rules apply. Folder APIs requiring `folders:read` are unavailable to public OAuth apps; this plugin uses a personal token. A successful health check confirms `/v1/me`, not access to every file or team.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Read the current user and return `ok` and `message`. |
| `get_file_metadata` | Read `/v1/files/{file_key}/meta`, including name, editor type, creator and available version/URLs. |
| `get_nodes` | Return selected nodes and their direct children as typed summaries, using fixed `depth=1`. |
| `render_images` | Return rendered-image URL metadata for selected nodes; no image download occurs. |
| `list_team_folders` | List visible top-level folders using `/v2/teams/{team_id}/folders`. |
| `list_subfolders` | List direct children using `/v2/folders/{folder_id}/folders`. |
| `list_folder_files` | List direct files using `/v2/folders/{folder_id}/files`. |
| `list_comments` | Return comments on a file. |
| `create_comment` | Post a plain text comment or reply to a root comment. |

The deprecated project endpoints are not used. Obtain a team ID from the Figma team-page URL. Folder and comment list endpoints currently have no cursor parameters; this plugin returns the API's complete direct list in one request, subject to the response-size limit. Nested folders are traversed explicitly with `list_subfolders`; the plugin does not recurse automatically.

## Inputs and results

Obtain `file_key` from a Figma file URL; pass only its key, not the complete URL. Node IDs come from Figma node links; URL forms such as `1-2` commonly correspond to API IDs such as `1:2`. Supply 1–100 unique API node IDs per call. `get_nodes` optionally accepts a saved `version`. Output preserves requested order, returns `found=false` for documented null entries, and provides each found node's ID, name, type, optional visibility/text and direct child summaries. It does not expose a full recursive design document, vector geometry, component/style maps or plugin data.

`render_images` supports `png` (default), `jpg`, `svg` and `pdf`; `options.scale` accepts 0.01–4. Optional `contents_only` and `use_absolute_bounds` preserve explicit `false`. Each result has `node_id`, `rendered`, and an optional URL. A null URL means that node could not be rendered. Figma currently documents a 30-day image URL lifetime and a 32-megapixel export ceiling; larger images may be scaled down. Treat URLs as temporary access links. Returned URLs are never fetched by this plugin and credentials are never sent to them.

For a comment, provide `file_key` and `message` (1–10,000 characters, a plugin limit). To reply, set `reply_to` to a root comment ID; replies to replies are rejected by Figma. Canvas-positioned comments, comment deletion, resolved-state changes, design edits and file duplication are outside this release.

## Limits and verification

Operations allow 60 seconds; HTTP timeout is 50 seconds, request limit 1 MiB, response limit 8 MiB. Large files or comment lists can exceed the limit. Redirects and automatic retries are disabled. Errors omit raw response bodies and token values. A timed-out comment may already exist: check the file before posting again. API rate limits vary by endpoint tier, token owner, plan and seat.

Fixtures are synthetic adaptations of the official REST documentation and OpenAPI specification, including null nodes and render failures. No real Figma account, file or comment was accessed during development. Verify actual token scopes, team visibility, rendering and a controlled comment before production.

## Official references

- [File endpoints](https://developers.figma.com/docs/rest-api/file-endpoints/)
- [Folder endpoints](https://developers.figma.com/docs/rest-api/folders-endpoints/)
- [Comment endpoints](https://developers.figma.com/docs/rest-api/comments-endpoints/)
- [User endpoints](https://developers.figma.com/docs/rest-api/users-endpoints/)
- [Official OpenAPI specification](https://github.com/figma/rest-api-spec/blob/main/openapi/openapi.yaml)

References were checked on 2026-11-04. SDK v0.7.7 runtime `SetDoc` uploads the English guide without locale dictionaries. Registry installation packages the exact full-guide Chinese translation from `locales/zh-CN.json`; a separate Chinese guide is included for repository readers.
