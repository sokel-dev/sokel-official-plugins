# Tianyancha

Query company registration details and registration changes using Tianyancha's commercial data APIs.

## Credentials and API entitlements

Obtain a token from the [Tianyancha data platform](https://open.tianyancha.com/) and enable **API 365** for company details and **API 822** for registration changes. Enter the token in **Commercial API token**. The plugin sends the raw token in the `Authorization` header to `https://open.api.tianyancha.com`; do not prefix the credential with `Bearer`.

A consumer website membership does not by itself establish access to these commercial APIs. Permissions, account balance, remaining calls and IP allowlists are managed by Tianyancha and can differ between endpoints. Confirm the current commercial terms in your account before running workflows.

Both operations can consume paid calls. The plugin intentionally has no automatic `health_check` or separate paid probe. Use a deliberate company lookup when validating the credential; success for API 365 does not prove access to API 822. The platform may report that automatic credential testing is unsupported. No refresh flow is included; replace the token externally when required.

## Operations

| Operation | Inputs | Result |
| --- | --- | --- |
| `get_company` | `keyword`: company name, company ID or registration code accepted by API 365 | `company`: company ID, name, registration status, credit code, legal representative, capital, industry, address, business scope, establishment time and embedded key personnel |
| `list_company_changes` | `keyword`: company name or ID; optional `page.page` and `page.page_size` | `changes`, `total`, `has_more` and `next_page` |

`get_company` uses `/services/v4/open/baseinfoV3`. IDs are strings, including IDs returned by the service as JSON numbers, so values beyond JavaScript's safe integer range are preserved. `established_at_ms` is a string containing the original Unix timestamp in milliseconds; negative values represent dates before 1970. Missing optional fields become empty strings. Capital remains the provider's formatted string with its original unit.

`company.staff` is the personnel subset embedded in the base-info response. Compare its length with `staff_total`: this endpoint can return fewer personnel than the reported total. This operation does not retrieve additional personnel pages. The first version does not provide general company search, shareholder or risk endpoints.

`list_company_changes` uses `/services/open/ic/changeinfo/2.0`, with `keyword`, `pageNum` and `pageSize` query parameters. The default is page 1 with 20 entries. The plugin permits page numbers 1..10000 and page sizes 1..20 as implementation bounds; these are not a claim about all service-side limits. Use the returned `next_page` only when `has_more` is true, retaining the same keyword and page size. Missing or inconsistent pagination data is an error rather than a silently truncated success. If more pages exist beyond the plugin's page bound, the operation returns an error.

Change entries expose `item`, `before`, `after`, `changed_at` and `created_at`. Text, markup and dates retain the upstream representation. A service code of `300000` means no results: change listing returns an empty array and total zero; company lookup returns a clear no-match error. The plugin rejects HTTP failures and nonzero business codes even when HTTP status is 200. Errors identify permission, expiry, rate, balance, remaining-call and IP restrictions without echoing the upstream reason or token.

Example change query:

```json
{"keyword":"Example company","page":{"page":1,"page_size":20}}
```

## Limits and validation

Requests stay on the fixed HTTPS origin, redirects are disabled and requests are not automatically retried, including paid reads. Responses are limited to 8 MiB. UI labels, instructions and errors default to English; Chinese translations are included.

Tests use synthetic fixtures based on official API examples. They verify raw-token authentication, exact IDs and timestamps, page parameters, empty results, business failures, generated platform output, secret redaction and response bounds. No paid request or live account acceptance was performed as part of these fixture tests. Before production use, validate both API entitlements and representative responses with an authorized account.

Official references: [API 365: company basic information](https://open.tianyancha.com/open/365) and [API 822: registration changes](https://open.tianyancha.com/open/822).
