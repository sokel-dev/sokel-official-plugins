# HubSpot CRM

Use a HubSpot private-app access token to read and manage contacts, companies and deals through the CRM v3 APIs. This version uses manual credentials and does not implement OAuth, record deletion, associations or custom objects.

## Setup

Create a private app in the HubSpot account and grant `crm.objects.contacts.read`, `crm.objects.companies.read` and `crm.objects.deals.read` for the objects you use. Grant the corresponding `.write` permissions only for create/update workflows. Property metadata may also require the corresponding CRM schema read permissions. Store the access token in **Private-app access token**.

Run **Check credentials**. It reads one contact and returns `ok` and `message`; this proves contact read access only, not write access or access to every object. A rejected token, inaccessible account or malformed response returns `ok=false`.

The deployment uses `https://api.hubapi.com`. Operators may set `HUBSPOT_API_BASE_URL` to a trusted HTTPS API endpoint. The endpoint cannot be supplied by workflow inputs or credentials. HTTP is accepted only for loopback development tests. Redirects are rejected so authorization is never forwarded to another origin.

## Operations

| Operation | Behavior |
| --- | --- |
| `health_check` | Read one contact to check credentials. |
| `list_records` | List up to 100 active records of one object type. |
| `search_records` | Search up to 100 records with a text query and one AND group of conditions. |
| `get_record` | Read one numeric record ID, optionally selecting properties. |
| `create_record` | Create one record with explicit property values. |
| `update_record` | Patch property values on an existing record ID. |
| `list_properties` | Read the object's property definitions and enumeration options. |

Set `object_type` to `contacts`, `companies` or `deals`. Record IDs and cursors are strings. Read operations return typed record metadata plus `properties`, a dynamic map whose keys are defined by your HubSpot account and whose values are strings or null. The default returned property set is selected by HubSpot; pass `properties` to request specific names.

For a contact, `create_record` can use `{"email":"person@example.com","firstname":"Ada"}` as its `properties` input. Numeric, boolean and date property values must also use HubSpot's documented string representations. Write maps accept 1–100 properties; an empty string clears a property. Null, numeric and boolean JSON values are rejected. Required fields depend on the object and account; for deals, look up the pipeline and stage values used by your account before writing.

`list_records` returns `next_after`. Pass it unchanged as `after` until it is empty. List cursors are opaque. `search_records` uses numeric offset strings, returns `total`, and supports offsets below 10,000; partition a larger search by another filter. Each operation reads exactly one page. A repeated cursor or malformed response fails explicitly.

Search accepts up to six AND conditions. Use `property_name`, `operator`, and either `value` or `values`. `IN`/`NOT_IN` require an array of 1–100 strings; `HAS_PROPERTY`/`NOT_HAS_PROPERTY` accept no values. Scalar comparison and token operators use `value`. For string properties, HubSpot requires lowercase `IN`/`NOT_IN` values; enumeration filters are case-sensitive. Use property metadata to choose the correct values; the plugin does not change their case. The complete search body is limited to 3,000 bytes. Search results follow HubSpot's indexing behavior and may lag recent writes; use `get_record` to inspect a known write receipt.

## Errors and limits

Requests time out after 45 seconds. JSON requests are limited to 1 MiB and responses to 4 MiB. A 429 error reports a parsed `Retry-After` delay, or 60 seconds when the header is absent or invalid. Requests are never automatically retried, including writes. If a write times out or returns an invalid receipt, inspect the account before retrying because the record might already exist or have changed. Upstream error bodies, tokens and transport URLs are not included in errors.

There is no event source in this version. CRM v3 is deliberately pinned; newer date-based HubSpot API versions and service keys have not been accepted by live tests for this plugin.

## Development and validation

The module uses the public Sokel Plugin SDK v0.7.8. Run `go generate ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, and `go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .` from this directory.

Tests use synthetic HTTP fixtures based on official API contracts. They verify request bytes, auth, pagination, generated input/output binding, invalid receipts, bounded responses, 429 handling and rejected redirects. They are not live HubSpot acceptance. No plugin image or registry entry is published by these checks.

## Official references

- [Contacts API guide](https://developers.hubspot.com/blog/a-developers-guide-to-hubspot-crm-objects-contacts-object)
- [Deals API guide](https://developers.hubspot.com/blog/a-developers-guide-to-hubspot-crm-objects-deals-object)
- [CRM search and object API differences](https://developers.hubspot.com/blog/when-to-use-crm-search-api-vs.-object-specific-apis)
- [Official CRM v3 contact client](https://github.com/HubSpot/hubspot-api-python/blob/master/hubspot/crm/contacts/api/basic_api.py)
- [Official property metadata client](https://github.com/HubSpot/hubspot-api-python/blob/master/hubspot/crm/properties/api/core_api.py)
