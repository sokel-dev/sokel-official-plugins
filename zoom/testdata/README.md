# Fixture provenance

These JSON files are **synthetic adaptations of official documentation**, not captured service responses. No authenticated account API was called. Official documentation was inspected on 2026-11-04:

- [Official reference 1](https://developers.zoom.us/docs/api/meetings/)
- [Official reference 2](https://developers.zoom.us/docs/api/users/)

Documented field names, endpoint paths and response envelopes are retained. Names, IDs, dates and URLs are replaced with test values, and irrelevant optional fields are omitted. URLs use example.invalid and are never fetched. Edge cases deliberately add missing fields, error bodies, size overflow and redirect destinations in Go tests; these are adversarial test adaptations, not claims about a captured API event. Transport stubs record the actual request URL, headers and encoded body built by production code.

The meeting fixture uses an artificial ID above 2^53 to prove numeric precision and an artificial host start URL to prove it is not exposed. Pagination tokens containing a URL are test-only adversarial values.
