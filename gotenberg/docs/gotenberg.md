# Gotenberg

Convert HTML and office documents to PDF, or merge PDF files, using your own Gotenberg service. No SaaS account is required. This release targets the Gotenberg 8 API, verified against the official 8.39.0 documentation and source. Gotenberg's own code is MIT licensed; its bundled conversion engines have their own licenses.

## Deployment and authentication

Set the required deployment environment variable `SOKEL_GOTENBERG_URL` to the service base URL, for example `http://gotenberg:3000` or `https://documents.example.org/gotenberg`. Internal HTTP, HTTPS and a reverse-proxy path prefix are supported. The address must be reachable from the plugin process. No service is started automatically and there is no public fallback. User information, query strings, fragments, percent-encoded paths and dot segments are rejected in this URL.

Leave both credential fields empty for a service without authentication, or provide both **Basic auth username** and **Basic auth password**. They are used only for HTTP Basic authentication to the configured service or its reverse proxy. Use HTTPS when the network is not trusted. Arbitrary headers, browser cookies, bearer tokens and OAuth are not supported. Redirects are rejected, including redirects on the same host, so configure the final service address directly.

`health_check` reads `/health` and returns `ok` plus `message`. It requires the documented `status: up` response to report success; failures return `ok=false`. The health endpoint can be public even when conversion routes require authentication. A successful check therefore confirms service health, not conversion permission or support for a particular document.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Check the configured service without converting a document. |
| `html_to_pdf` | Required `html`; optional `assets`, `options` and `output_name`. Uploads the HTML as `index.html` and returns a platform PDF file. |
| `office_to_pdf` | Required platform `file`; optional `output_name`. Converts one office document through LibreOffice and returns a platform PDF file. |
| `merge_pdfs` | Required `files` array containing 2–20 platform PDFs in the required order; optional `output_name`. Returns one merged platform PDF file. |

All successful conversion results use the `file` output. Connect it directly to another operation accepting a platform file. Inputs must carry a platform file ID and a plain filename, rather than a local path or an external download URL. Small inline file data is accepted by the SDK as well. File bytes are read through the platform file service; this plugin never opens an input path or downloads a user-supplied remote URL.

## HTML and assets

Example input:

```json
{
  "html": "<!doctype html><html><meta charset='utf-8'><body><h1>Report</h1><img src='logo.png'></body></html>",
  "assets": [{"id": "<platform-file-id>", "name": "logo.png", "size": 1234}],
  "options": {"landscape": false, "print_background": true, "prefer_css_page_size": false},
  "output_name": "report.pdf"
}
```

Use the actual reference produced by an upstream upload or file operation; the ID and size above are placeholders. Asset files share one flat directory with `index.html`. Reference them by filename only, such as `logo.png`, and include fonts needed for your document. Filenames must be unique ignoring case. `index.html`, `header.html` and `footer.html` are reserved and cannot be assets. Chinese filenames and quoted filenames are supported.

The three rendering options are optional booleans. An explicit `false` is transmitted; omission leaves Gotenberg's default. `prefer_css_page_size` allows CSS `@page` dimensions to take precedence. This release does not expose arbitrary Chromium options, waits, headers, cookies, URL conversion or JavaScript expressions.

HTML itself can contain scripts and references to external resources. Those are handled by Chromium inside Gotenberg, not fetched by this plugin. Configure the Gotenberg service's outbound URL filters and network access for your trust model. Service Basic credentials are never copied into browser headers. For self-contained conversion, use uploaded assets or data URIs and avoid remote dependencies. The plugin does not guarantee that an HTML document is isolated from the network.

## Office conversion and PDF order

Supported extensions are `.doc`, `.docx`, `.docm`, `.odt`, `.rtf`, `.txt`, `.xls`, `.xlsx`, `.xlsm`, `.ods`, `.csv`, `.ppt`, `.pptx`, `.pptm` and `.odp`. The installed LibreOffice engine, fonts and document contents determine actual support and layout. Password-protected documents and per-format import options are not exposed. Modern Gotenberg disables macros and omits linked content; embed images and other content rather than depending on external links.

Gotenberg merges PDFs by filename. The plugin uploads merge inputs as `0001.pdf`, `0002.pdf` and so on, preserving the order of the `files` array even when original filenames would sort differently or collide. It does not modify the original platform files. Merge inputs must begin with a PDF signature; Gotenberg performs the actual PDF parsing.

`output_name` is the platform output filename, not a server filesystem path. It must be a plain name ending in `.pdf`. Defaults are `document.pdf` for conversions and `merged.pdf` for merges. The upstream Content-Disposition filename is not used. PDF responses must have `application/pdf`, a PDF header and an end-of-file marker before they are stored in the platform; this is a transport sanity check, not a complete PDF validator. A platform storage failure fails the operation.

## Limits and errors

HTML is limited to 2 MiB, with up to 20 assets. Each input file and all files combined are limited to 32 MiB; multipart framing is capped at 33 MiB. The result is limited to 64 MiB. Declared file sizes are checked before fetching, and actual bytes are checked afterward. The SDK fetches files into memory, so these operations are intended for bounded documents rather than large archives.

Conversion operations have a 300-second platform timeout and a 240-second HTTP timeout. Gotenberg and reverse-proxy timeouts still apply; configure them to allow your expected workloads. The service may reject a request earlier. HTTP failures, invalid PDF responses, oversized data and platform file failures produce sanitized errors. A 429 response reports a bounded retry delay without automatically retrying. Upstream response bodies, credentials and connection URLs are excluded from errors.

No service call is automatically retried. A caller may repeat a failed conversion deliberately, but this consumes conversion resources again and may store another output file. This release does not offer PDF/A, encryption, page splitting, webhooks, file-system access or server deployment management. Fixture tests and real service acceptance are recorded separately in the repository.

## Official references

- [Gotenberg installation](https://gotenberg.dev/docs/getting-started/installation)
- [HTML to PDF](https://gotenberg.dev/docs/convert-with-chromium/convert-html-to-pdf)
- [LibreOffice conversion](https://gotenberg.dev/docs/convert-with-libreoffice/convert-to-pdf)
- [Merge PDFs](https://gotenberg.dev/docs/manipulate-pdfs/merge-pdfs)
- [Health endpoint](https://gotenberg.dev/docs/system/get-health-check)
- [Gotenberg 8.39.0 source and license](https://github.com/gotenberg/gotenberg/tree/v8.39.0)
