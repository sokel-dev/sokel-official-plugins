# Protocol evidence and fixture provenance

Reviewed on 2026-11-04 against Apache Tika **4.2.0** source and official 4.2.x documentation:

- https://tika.apache.org/docs/4.2.x/using-tika/server/index.html — PUT endpoints and HTTP 200 partial parse results.
- https://tika.apache.org/docs/4.2.x/migration-to-4x/migrating-tika-server-4x.html — `/detect/stream` became `/detect`, Accept routing removed, explicit handler paths.
- https://github.com/apache/tika/blob/4.2.0/tika-server/tika-server-core/src/main/java/org/apache/tika/server/core/resource/TikaResource.java — `/tika/json/text`, CONCATENATE mode and filename handling.
- https://github.com/apache/tika/blob/4.2.0/tika-server/tika-server-core/src/main/java/org/apache/tika/server/core/resource/RecursiveMetadataResource.java — `/rmeta/text`, container followed by embedded entries.
- https://github.com/apache/tika/blob/4.2.0/tika-server/tika-server-core/src/main/java/org/apache/tika/server/core/resource/DetectorResource.java — raw PUT `/detect` and media type response.
- https://github.com/apache/tika/blob/4.2.0/tika-core/src/main/java/org/apache/tika/metadata/TikaCoreProperties.java — `tk:content`, exception/limit/deadline and truncation markers.
- https://github.com/apache/tika/blob/4.2.0/tika-serialization/src/main/java/org/apache/tika/serialization/serdes/MetadataSerializer.java — strings, multi-values and optional typed JSON values.
- https://github.com/apache/tika/blob/4.2.0/LICENSE.txt — Apache-2.0 and component notices.

All inline metadata responses are synthetic adaptations of these contracts, not live captures. Large integers, structured JSON, empty extraction, old `X-TIKA` keys, child exceptions, false flags and malformed payloads are intentional synthetic variants. They test the adapter and error handling, not Tika parser correctness or OCR quality. Real container acceptance is recorded separately when available.
