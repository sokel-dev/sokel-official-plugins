# Paperless-ngx protocol provenance

There are no captured live Paperless responses in this plugin. Inline fixtures are **synthetic protocol and boundary variants** derived from the official stable v3.4.1 source inspected on 2026-11-04. They must not be presented as real-service acceptance.

The authoritative pinned sources are:

- [API guide](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/docs/api.md): Authorization Token; `Accept: application/json; version=10`; POST document multipart fields and repeated `tags`; HTTP 200 consumption-task UUID; task lookup; version-aware download; supported API versions 9 and 10 with default 10.
- [Views](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/views.py): `PostDocumentView`, `TasksViewSet`, `DocumentViewSet.download` / `file_response`, and `TagViewSet`; paginated API 10 tasks and exact `task_id` filtering; `original=true` behavior and attachment response.
- [Serializers](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/serialisers.py): `TaskSerializerV10` fields, `DocumentSerializer` metadata, `TagSerializer` color/inbox fields; task result document IDs are an integer array.
- [Models](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/src/documents/models.py): `PaperlessTask.Status` has pending/started/success/failure/revoked and `consume_file` task type.
- [Stable release](https://github.com/paperless-ngx/paperless-ngx/releases/tag/v3.4.1) and [GPLv3 license](https://github.com/paperless-ngx/paperless-ngx/blob/v3.4.1/LICENSE).

Official documented request examples establish `/api/documents/post_document/` with the `document` multipart field and `/api/tasks/?task_id={uuid}` for follow-up. Tests use an explicitly synthetic UUID, synthetic document bytes and IDs including 9007199254740993 to expose rounding. Unicode quoted multipart headers are inspected as raw bytes, not only through Go's multipart decoder. API 9 arrays, uppercase legacy statuses, missing counts, malformed receipts, redirects and external continuation URLs are deliberately synthetic failure cases. The development branch was not used to infer stable API support.
