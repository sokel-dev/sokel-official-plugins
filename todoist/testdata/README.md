# Fixture provenance

These JSON files are **synthetic adaptations of official documentation**, not captured service responses. No authenticated account API was called. Official documentation was inspected on 2026-11-04:

- [Official reference 1](https://developer.todoist.com/api/v1/)

Documented field names, endpoint paths and response envelopes are retained. Names, IDs, dates and URLs are replaced with test values, and irrelevant optional fields are omitted. URLs use example.invalid and are never fetched. Edge cases deliberately add missing fields, error bodies, size overflow and redirect destinations in Go tests; these are adversarial test adaptations, not claims about a captured API event. Transport stubs record the actual request URL, headers and encoded body built by production code.

Task/project fixtures follow API v1 SyncView response structures and results/next_cursor pagination. The close-task test uses the documented HTTP 200 and null example. Request priority descriptions and response priority descriptions conflict in the official specification; tests assert unchanged numeric transmission, not UI interpretation.
