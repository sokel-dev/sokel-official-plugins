# Salesforce

Query Salesforce records and metadata, and create, update or upsert records through the REST API pinned to **v65.0**. This stable version is explicit; the connector does not follow the `latest` alias.

## Credentials and permissions

Set `instance_url` to the HTTPS instance URL returned by your Salesforce OAuth flow, such as `https://example.my.salesforce.com`. Production My Domain, sandbox/developer My Domain subdomains and traditional instance hosts such as `https://na123.salesforce.com` are accepted. Login/test hosts, custom proxy domains, ports, userinfo, query parameters and paths are rejected.

Set `access_token` to an externally obtained OAuth token. The app needs the `api` scope, and the user needs API Enabled plus the relevant object, record and field permissions. Organization edition, session policy and connected/external client app policy also apply. This connector does not provide an OAuth provider, refresh tokens or username/password login; replace expired access tokens externally.

`health_check` reads authenticated API limits and returns `ok` and a sanitized `message`. It verifies API access, not permission to every Salesforce object.

## Metadata and record reads

`describe_object` requires an object API name such as `Account` or `MyObject__c`. It returns object name/label, create/update flags and field metadata including API name, type, nullability, write flags, external-ID flag and reference targets. Use this to determine writable fields in your organization.

`get_record` requires `object_name` and a 15- or 18-character `record_id`. Optional `field_names` contains simple API field names, not SOQL expressions. The connector always requests Id when selecting fields. The returned record is an object whose fields depend on your tenant schema and field permissions.

## Read-only SOQL and pagination

`query` accepts a SELECT expression in `soql`, up to 16,000 characters. For example: `SELECT Id, Name FROM Account LIMIT 100`. Query text is URL-encoded, not interpolated into an endpoint. DML, comments, semicolons and `FOR UPDATE`, `FOR VIEW` or `FOR REFERENCE` are rejected to keep this operation read-only. This conservative validation can also reject those tokens inside string literals.

Results expose `records`, `total_size`, `done` and `next_records_url`. Aggregate queries and selected relationship subqueries remain inside their returned record objects. When `done=false`, call `query_more` with the exact relative continuation path. Only `/services/data/v65.0/query/<locator>-<offset>` is accepted; absolute URLs and other endpoints are rejected. Each call reads one server page, which may contain fewer than 2,000 records. Query locators expire; this is not a durable synchronization mechanism. Nested relationship result sets are not automatically paginated.

## Writes and numeric precision

`create_record` requires object API name and a nonempty `fields` object. `update_record` also requires a record ID. Omitted fields stay unchanged; JSON null clears a nullable field. Update succeeds only on the documented empty HTTP 204 response. Object/field names use API names, not display labels. The plugin excludes `Id` and `attributes` from write fields and leaves permissions, validation rules and other business rules to Salesforce.

`upsert_record` requires object name, an `external_id_field`, an `external_id` value and field values. Configure a suitable external ID field in Salesforce. A matching record is updated; no match creates a record. Multiple matches fail. This version does not support the special Id/null insert form or `updateOnly`. External ID values are limited to 255 characters and cannot contain slash, backslash, percent, query/fragment markers or line breaks. The external ID must not also appear in `fields`. Modern Salesforce upsert returns JSON with `created=false` on HTTP 200 and `created=true` on HTTP 201; both are validated.

Opaque record fields are necessary because custom schemas and SOQL projections vary per organization. Response JSON numbers retain their exact decimal representation. The platform SDK binds opaque input numbers through float64, so send large integer or exact-decimal field values as strings where Salesforce permits it. Numeric inputs outside the exact integer range ±9,007,199,254,740,991 are rejected. This does not guarantee arbitrary decimal input precision.

## Reliability and limits

Each operation has a 60-second platform timeout and 55-second HTTP timeout. JSON requests/responses are bounded to 8 MiB. Requests remain on the validated Salesforce HTTPS origin, and redirects are rejected. Errors omit sensitive response bodies. There are no automatic retries, including writes. After an uncertain create/update/upsert result, read or query the record before retrying. Upsert requires a stable, preferably unique external ID and does not promise exactly-once execution of Salesforce automations. Writes can trigger flows, validation rules and other organization automation. Updates do not include a concurrency guard.

No live account calls were performed. Synthetic protocol and generated-binding tests are clearly labeled. Bulk APIs, Composite API, deletes, file transfer, triggers, Change Data Capture and subscriptions are outside this version.

## Official references

- [REST resources and requests](https://developer.salesforce.com/docs/platform/api-rest/guide/intro-rest-resources.html)
- [Query](https://developer.salesforce.com/docs/platform/api-rest/guide/resources-query.html)
- [Object describe](https://developer.salesforce.com/docs/platform/api-rest/guide/resources-sobject-describe.html)
- [Upsert and modern response statuses](https://developer.salesforce.com/docs/platform/api-rest/guide/dome-upsert.html)
