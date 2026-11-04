# Pipedrive

Read and create CRM persons and organizations, and read, create and update deals. Business operations use Pipedrive API v2; the current-user health check uses the supported v1 `users/me` endpoint. This plugin uses personal API-token authentication with the official `x-api-token` header.

## Credentials

In Pipedrive, obtain your personal API token from the account's API settings. Enter it as **API token** in Sokel. Enter only the company subdomain in **Company subdomain**: use `acme` for `https://acme.pipedrive.com`. A full URL, port, dotted hostname or custom domain is rejected. The plugin builds the HTTPS address itself and never includes the token in a query string.

The token acts with its owner's account permissions and visibility rules. Check API availability for your plan and account. Renewal and rotation happen outside the plugin; rotating a personal token also affects other integrations using the previous token. This release has no OAuth authorization or refresh flow.

Run `health_check`. It reads `/api/v1/users/me` and returns `ok` and `message`. Unusable credentials return `ok=false` without a handler error or a write. Successful identity access does not establish permission to change every CRM record.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Read the current user to check credentials. |
| `list_persons` | Optional `page`; returns persons and continuation fields. |
| `get_person` | `person_id`; returns ID, name, optional organization ID, email addresses and phone numbers. |
| `create_person` | `name`; optional `fields.org_id`, `fields.emails` and `fields.phones`. Returns the created person. |
| `list_organizations` | Optional `page`; returns organization IDs and names and continuation fields. |
| `create_organization` | `name`; returns the created organization. |
| `list_deals` | Optional `page`; returns deal ID/title/status/value/currency and optional person/organization IDs, plus continuation fields. Uses upstream default visibility and archive filtering. |
| `create_deal` | `title`; optional `fields.value`, `currency`, `person_id`, `org_id` and `status`. Returns the deal. |
| `update_deal` | `deal_id`; optional `title` and the same `fields` as creation. At least one change is required. Returns the updated deal. |

All resource IDs are positive decimal strings on the Sokel interface. Pipedrive JSON numbers are parsed without floating-point conversion; linked IDs are sent as exact JSON integers. The plugin preserves nullable relationship IDs rather than changing them to zero.

## Create and update examples

Create a person linked to an organization:

```json
{
  "name": "Alex Example",
  "fields": {
    "org_id": "123",
    "emails": [{"value": "alex@example.com", "primary": false, "label": "work"}]
  }
}
```

Email and phone entries contain `value`, optional `primary` and optional `label`. Explicit `primary=false` is preserved. Omit an array to leave it unspecified; an explicit empty array is sent as empty. At most 100 entries per array and at most one primary entry are accepted. Email addresses must be plain addresses without display names.

Update a deal to zero value without changing its other fields:

```json
{"deal_id": "456", "fields": {"value": "0"}}
```

Amounts use decimal **strings** in the interface and exact JSON numbers on the wire. Input amounts must be nonnegative plain decimal notation, at most 32 characters; scientific notation is not accepted for writes. Returned amounts preserve their exact upstream number text, including an exponent if present. Currency codes must be three uppercase letters, such as `USD`; Pipedrive determines which codes the account accepts. Status must be `open`, `won` or `lost`. Omitted fields remain unchanged on updates. This release does not expose null to clear relationship IDs or empty titles to clear a deal title.

## Cursor pagination

All three list operations accept `page.limit` (default 100, range 1–500) and optional `page.cursor`. Omit the cursor on the first request. While `has_more` is true, copy `next_cursor` exactly into the next call's `page.cursor` using the same operation and account. The upstream `additional_data.next_cursor=null` marks completion; the plugin then returns an empty `next_cursor` and `has_more=false`.

One operation fetches one page and never follows an upstream URL. Missing, repeated or malformed cursors fail instead of silently stopping early. Pagination is not a snapshot or exactly-once feed; deduplicate record IDs if records change during a scan. Lists expose the selected current records, not a deleted-record archive or complete custom-field export.

## Limits, failures and scope

Names and deal titles are limited to 1024 bytes. Requests are capped at 1 MiB and responses at 8 MiB. Calls have a 45-second transport timeout within the operation deadline. Use smaller pages for large contact collections. Pipedrive's token budget, rate limits, record visibility and plan limits continue to apply.

HTTP errors, `success=false`, missing data and malformed receipts are reported as errors. Upstream response bodies, URLs containing secrets and credentials are not echoed. The plugin accepts only the fixed Pipedrive company origin, rejects redirects and never retries requests automatically. After an uncertain write, inspect the CRM record before repeating it to avoid duplicate persons, organizations or deals.

This release does not update persons or organizations, delete records, manage activities or pipelines, upload files, expose arbitrary filters/custom fields, or register webhooks. Tests use synthetic fixtures adapted from official v2 OpenAPI and authentication/pagination examples; large IDs, exact money, optional false and errors are deliberate test variations. No live account acceptance has been performed.

## Official references

- [API-token authentication](https://pipedrive.readme.io/docs/core-api-concepts-authentication)
- [Company domain](https://pipedrive.readme.io/docs/how-to-get-the-company-domain)
- [Persons](https://developers.pipedrive.com/docs/api/v1/Persons)
- [Organizations](https://developers.pipedrive.com/docs/api/v1/Organizations)
- [Deals](https://developers.pipedrive.com/docs/api/v1/Deals)
- [Cursor pagination](https://pipedrive.readme.io/docs/core-api-concepts-pagination)
- [Official v2 OpenAPI](https://developers.pipedrive.com/docs/api/v1/openapi-v2.yaml)

The documentation site's `/docs/api/v1/` URL also hosts the v2 endpoint reference; the business requests themselves use `/api/v2/`.
