# Protocol evidence and fixture provenance

Reviewed on 2026-11-04 against Gotenberg **v8.39.0**. These references define the request/response contract:

- https://gotenberg.dev/docs/convert-with-chromium/convert-html-to-pdf — multipart `files`, mandatory `index.html`, flat asset directory, boolean options, PDF response.
- https://gotenberg.dev/docs/convert-with-libreoffice/convert-to-pdf — single office document conversion and server behavior.
- https://gotenberg.dev/docs/manipulate-pdfs/merge-pdfs — PDF route and filename ordering.
- https://gotenberg.dev/docs/system/get-health-check — HTTP 200 `status: up`, HTTP 503 unhealthy.
- https://github.com/gotenberg/gotenberg/blob/v8.39.0/pkg/modules/api/formdata.go — original filename sorting in `FormData.paths`.
- https://github.com/gotenberg/gotenberg/blob/v8.39.0/pkg/modules/pdfengines/routes.go — `/forms/pdfengines/merge` handler.
- https://github.com/gotenberg/gotenberg/blob/v8.39.0/LICENSE — MIT license.

The inline JSON health response is a shortened synthetic adaptation of the documented response. The `%PDF-1.4` sample contains deliberately synthetic text and is not a valid rendered PDF. It exercises MIME/signature/EOF/storage checks, not the PDF engines. All redirect, failure, oversize, unicode, quoted filename and explicit-false variants are synthetic boundary tests. Live container artifacts, if collected by the batch acceptance run, are separate evidence and are not implied by these fixtures.
