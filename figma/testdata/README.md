# Fixture provenance

These JSON files are **synthetic adaptations of official documentation**, not captured service responses. No authenticated account API was called. Official documentation was inspected on 2026-11-04:

- [Official reference 1](https://developers.figma.com/docs/rest-api/file-endpoints/)
- [Official reference 2](https://developers.figma.com/docs/rest-api/folders-endpoints/)
- [Official reference 3](https://developers.figma.com/docs/rest-api/comments-endpoints/)
- [Official reference 4](https://github.com/figma/rest-api-spec/blob/main/openapi/openapi.yaml)

Documented field names, endpoint paths and response envelopes are retained. Names, IDs, dates and URLs are replaced with test values, and irrelevant optional fields are omitted. URLs use example.invalid and are never fetched. Edge cases deliberately add missing fields, error bodies, size overflow and redirect destinations in Go tests; these are adversarial test adaptations, not claims about a captured API event. Transport stubs record the actual request URL, headers and encoded body built by production code.

Folder schemas come from the official v2 OpenAPI responses. Node and image null entries follow the documented missing-node/render-failure shapes. Only node summaries and direct children are exposed.
