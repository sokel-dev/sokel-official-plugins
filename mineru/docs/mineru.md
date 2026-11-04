# MinerU

Configure a token created in the MinerU API dashboard. This plugin implements only the public `mineru.net` v4 precision extraction protocol; it does not claim compatibility with self-hosted servers or the anonymous lightweight API.

1. `submit` sends a publicly downloadable document URL and returns `task_id`. Options include pipeline/vlm/MinerU-HTML, OCR, tables/formulas, language and page ranges.
2. `status` reads asynchronous status. When `done`, `full_zip_url` points to the result archive containing Markdown and structured JSON. The plugin returns the link without downloading or extracting arbitrary external archives. Progress, state, file name, correlation ID and failure reason are typed fields.
3. `batch_submit` accepts 1–200 URLs; `batch_status` reads each document by `batch_id`.
4. `health_check` reads an existing `task_id` supplied by the caller; it never submits a parse or spends parsing quota. Without an ID it returns an unverified explanation, never a false success. Use the debug panel to provide an existing ID when the credential page has no task input.

`extra_formats` supports docx/html/latex; Markdown and JSON are included by default. HTML inputs must explicitly select MinerU-HTML. File/page limits depend on the current account and service; the plugin never downloads source files. Poll every 2–5 seconds. Submissions are not automatically retried.

Set deployment endpoint `SOKEL_MINERU_BASE_URL`, default `https://mineru.net`. HTTPS is required; HTTP is limited to loopback tests. The endpoint is not a credential field. Requests time out after 45 seconds, responses are capped at 16 MiB and all redirects are rejected. Errors expose only status and safe error codes, never keys.

Official reference: https://mineru.net/apiManage/docs
