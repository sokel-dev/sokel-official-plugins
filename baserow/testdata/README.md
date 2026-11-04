# Fixture provenance

`documented-wire.json` is synthetic. It adapts official Baserow 2.4.1 OpenAPI endpoint methods, paths, status codes and response shapes to small artificial tables, fields and rows. Sources: https://api.baserow.io/api/schema.json and https://github.com/baserow/baserow/blob/2.4.1/backend/src/baserow/contrib/database/api/tables/views.py, checked on 2026-11-04. The ID 9007199254740993 and false/zero values are deliberate precision/binding edge cases, not real records.

Tests inspect exact outgoing bytes through the actual generated Sokel registration path. Other tests deliberately corrupt response envelopes, continuation links or identifiers. No Baserow service recording or live acceptance is claimed; these fixtures cannot establish deployment-specific permissions or behavior by themselves.
