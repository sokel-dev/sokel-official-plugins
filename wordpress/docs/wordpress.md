# WordPress

Read posts, categories and media, create draft posts, update post content, and upload media to one WordPress installation using an application password. WordPress 5.6 or later and an accessible REST API are required.

## Setup

Set the deployment environment variable `WORDPRESS_SITE_URL` to the site's HTTPS root, for example `https://example.com` or `https://example.com/blog`. Do not append `/wp-json/wp/v2`; the plugin adds it. One deployment targets one site. This address is deployment configuration, not a workflow input or credential field. Redirects are rejected, so use the canonical site URL. HTTP is allowed only for loopback development tests.

Create an application password in the WordPress user's profile and save **Username** and **Application password** in the credential. Use an account with the capabilities needed by the workflow; draft creation, editing existing posts, viewing private posts and uploading media require appropriate WordPress capabilities. A normal login password is not a substitute. Authentication uses HTTP Basic over HTTPS; no OAuth provider is declared.

Run **Check credentials**. It requests `/wp-json/wp/v2/users/me` and requires a positive authenticated user ID. This checks identity, not every editing or upload permission. Authentication or malformed response failures return `ok=false` and a safe message.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Verify the authenticated user. |
| `list_posts` | Read one page of posts; defaults to published posts. |
| `get_post` | Read a post by its numeric ID. |
| `create_draft` | Create a post with `status=draft` fixed in the request. |
| `update_post` | Replace title, content and excerpt while preserving publication status. |
| `list_categories` | Read category IDs, names, parent IDs and post counts. |
| `list_media` | Read media IDs, MIME types, URLs and alternative text. |
| `upload_media` | Upload a platform file as a WordPress attachment. |

IDs and page numbers are strings. List operations request 20 items and return `total`, `total_pages` and `next_page`. Start at page `1`, pass `next_page` until empty, and account for concurrent changes when combining pages. Reverse proxies must preserve `X-WP-Total` and `X-WP-TotalPages`; missing headers fail explicitly. Post status accepts `publish`, `draft`, `pending`, `private` and `future` according to account permissions. Text search is optional.

Post outputs include rendered HTML (`title_html`, `content_html`, `excerpt_html`), not original editor source. Editing rendered content can change editor-specific markup. The `modified_gmt` value follows WordPress's UTC date representation. Category ID `0` in `parent_id` means no parent.

Both create and update require a nonblank title and a content field; content may be an empty string to clear it. The excerpt defaults to an empty string, so omitting it during update clears it. Omit `categories` to preserve an existing post's categories; explicitly pass `[]` to clear them. Titles are limited to 4,096 bytes, excerpts to 64 KiB, and complete JSON requests to 1 MiB. Site validation and hooks still apply.

Creating a post always requests a draft and verifies draft status in the response. This version has no publish, schedule or delete operation. Updating a currently published post changes its live content immediately. If site hooks override draft status, the operation fails and asks you to inspect the created post; it does not perform a compensating delete.

## Media uploads

Pass a Sokel platform file, not an arbitrary URL. Files must contain 1 byte to 10 MiB and use JPEG, PNG, GIF, WebP, PDF or plain-text MIME types. The filename must not contain a path, CR/LF or NUL. The plugin obtains file bytes through the platform file API, builds a multipart upload, and never sends the WordPress credential to the source file URL. Optional `alt_text` is limited to 4,096 bytes. WordPress file policies and server upload limits may reject otherwise valid inputs.

## Errors and validation

Requests time out after 45 seconds, responses are bounded at 4 MiB, and redirects are not followed. A 429 error reports `Retry-After` or a 60-second fallback. No request is retried automatically. After a timeout or invalid write receipt, inspect the site before retrying to avoid duplicate posts or attachments. Errors omit upstream bodies, application passwords and signed file URLs.

The public Sokel Plugin SDK v0.7.8 is the only direct module dependency. Run `go generate ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, and `go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 check .` from this directory.

Synthetic HTTP fixtures verify Basic authentication, pagination headers, generated input/output binding, draft status, explicit empty values, multipart bytes, safe errors and redirect rejection. These checks do not constitute live WordPress acceptance.

## Official references

- [Application passwords and authentication](https://developer.wordpress.org/rest-api/using-the-rest-api/authentication/)
- [Posts](https://developer.wordpress.org/rest-api/reference/posts/)
- [Categories](https://developer.wordpress.org/rest-api/reference/categories/)
- [Media](https://developer.wordpress.org/rest-api/reference/media/)
- [Pagination](https://developer.wordpress.org/rest-api/using-the-rest-api/pagination/)
