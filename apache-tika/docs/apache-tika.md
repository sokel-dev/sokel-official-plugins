# Apache Tika

Extract document text, metadata and embedded-document content, or detect a file's media type, using your own Apache Tika Server. No SaaS account is required. This release targets **Apache Tika Server 4.2.0-compatible endpoints and metadata**. Apache Tika is licensed under Apache-2.0, with additional notices for bundled dependencies. Tika 3.x has different endpoints and metadata keys and is not supported by this release.

## Deployment and credentials

Set the required deployment environment variable `SOKEL_TIKA_URL` to the service base URL, for example `http://tika:9998` or `https://documents.example.org/tika`. The plugin supports internal HTTP, HTTPS and a reverse-proxy path prefix. The configured service must already be running and reachable from the plugin. There is no public fallback and no automatic deployment. URLs containing user information, a query, a fragment, percent-encoded paths or dot segments are rejected.

For an unprotected internal service, leave both credential fields blank. If a reverse proxy uses HTTP Basic authentication, provide both **Basic auth username** and **Basic auth password**. This does not enable native Tika user management; it supplies the standard Authorization header to the configured endpoint. Use HTTPS on untrusted networks. Custom headers, bearer tokens and OAuth are not exposed. All redirects are rejected to keep documents and credentials at the configured address.

Tika 4.x parsing runs in forked JVM workers and requires a working Pipes configuration on the server. Install the parsers, fonts, OCR programs and language data your documents require, and configure resource limits in Tika. This plugin does not install parsers, enable `/pipes`, submit external fetch URLs, or change per-request parser settings. An empty extraction is not proof that OCR was available.

## Operations

| Operation | Request and output |
| --- | --- |
| `extract_text` | Upload the platform `file` to `PUT /tika/json/text`. Returns plain `text` and document `metadata`; Tika concatenates extracted embedded content in this mode. |
| `extract_metadata` | Upload `file` to `PUT /meta`, requesting JSON. Returns container-document `metadata` without extracted body text or a recursive document list. |
| `extract_recursive` | Upload `file` to `PUT /rmeta/text`. Returns `documents`: the container first, then embedded documents in upstream order. Each entry contains `text` and `metadata`. |
| `detect_type` | Upload `file` to `PUT /detect`. Returns `media_type`, for example `application/pdf`. Detection does not promise that a full parser is available. |

This release does not provide a health-check shortcut. `detect_type` with a small known file exercises the actual configured service without modifying a business resource. Parsing and detection consume service resources and may create temporary server files according to Tika's configuration.

Each input uses a platform file reference, for example:

```json
{"file":{"id":"<platform-file-id>","name":"report.pdf","mime":"application/pdf","size":1234}}
```

Use the reference from an actual upstream file operation; the ID and size shown are placeholders. A platform file ID and a plain filename are required. Small SDK inline file data is also accepted. Local filesystem paths and URL-only references are rejected. The plugin reads bytes through the platform file service and sends the raw bytes as the HTTP body, not a multipart envelope. The filename is a detection hint; the file MIME value, when present, is sent as Content-Type. No arbitrary file or resource URL is fetched by this plugin.

## Stable metadata output

Metadata depends on the detected format, so it is returned as a sorted list of typed entries:

```json
[
  {"name":"Content-Type","values":["application/pdf"]},
  {"name":"dc:creator","values":["Alex","Sam"]}
]
```

Every entry has `name` and a `values` string array. A single upstream value becomes a one-element array; an upstream array becomes one string per member. Strings retain their text. Non-string values, including numbers, booleans and structured JSON, become compact JSON strings; large integer text is preserved without floating-point conversion. This provides stable workflow fields while retaining values from unknown parser-specific metadata keys. The normalization does not preserve the original JSON scalar types.

The special Tika 4 `tk:content` field becomes the `text` output and is removed from the metadata list. Missing text becomes an empty string, which can be valid for images without OCR, blank documents or documents without extractable text. Other metadata is retained. Embedded resource paths remain metadata only; they are never interpreted as local file paths. `extract_recursive` does not return embedded file bytes or download attachments.

## Partial results and errors

Tika may return HTTP 200 with a parse exception, a write limit, an embedded-resource limit, a task timeout or truncated metadata. The plugin rejects a nonempty `tk:exception:*` or `tk:warn:*` marker, except an explicit `false` value. It fails the entire operation, including when a recursive child has a marker, rather than returning a partial result as a successful extraction. This conservative policy also rejects parser warnings that may be nonfatal in Tika. Exception detail strings are not echoed in the error.

Responses must have the expected media type and valid JSON structure. Each document must include a valid `Content-Type`. Legacy `X-TIKA:*` fields produce a clear version-compatibility error. A 200 response without a reported warning is not proof of complete extraction: server-side filters may remove fields, parsers have format limits, and deployment settings can affect results. Keep Tika's failure and truncation metadata enabled for these checks to work.

HTTP failures, invalid document metadata, repeated JSON documents, invalid UTF-8, oversized results and platform file failures fail explicitly. Upstream error bodies, credentials and connection URLs are excluded from errors. A 429 response reports a bounded retry delay, without automatically retrying. No request is replayed automatically; a caller may deliberately retry after reviewing the service condition.

## Limits and scope

Input files are limited to 32 MiB. Declared sizes are checked before retrieval and actual bytes afterward; nonzero declared sizes must match the bytes. The SDK loads the file into memory. Extraction responses are limited to 16 MiB and recursive results to 10000 entries. Detection responses are limited to 4 KiB. Larger inputs or outputs fail rather than being silently truncated.

Operations have a 300-second platform timeout and a 240-second HTTP timeout. Server and proxy limits can end requests earlier. The plugin does not override Tika timeouts, OCR configuration or maximum embedded-resource limits. It does not expose arbitrary server paths, external fetchers, config endpoints, language detection, archives of extracted files or asynchronous Pipes jobs. Fixtures and real service acceptance are recorded separately in the repository.

## Official references

- [Tika Server 4.2 documentation](https://tika.apache.org/docs/4.2.x/using-tika/server/index.html)
- [Tika Server 4.x migration](https://tika.apache.org/docs/4.2.x/migration-to-4x/migrating-tika-server-4x.html)
- [Tika 4.2.0 extraction endpoint source](https://github.com/apache/tika/blob/4.2.0/tika-server/tika-server-core/src/main/java/org/apache/tika/server/core/resource/TikaResource.java)
- [Tika 4.2.0 metadata keys and exception markers](https://github.com/apache/tika/blob/4.2.0/tika-core/src/main/java/org/apache/tika/metadata/TikaCoreProperties.java)
- [Tika 4.2.0 metadata serialization](https://github.com/apache/tika/blob/4.2.0/tika-serialization/src/main/java/org/apache/tika/serialization/serdes/MetadataSerializer.java)
- [License and dependency notices](https://github.com/apache/tika/blob/4.2.0/LICENSE.txt)
