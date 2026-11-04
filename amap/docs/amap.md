# AMap

Geocode addresses, search places, plan routes, retrieve current weather and browse administrative districts through the AMap Web Service API.

## Credentials and quota

Create an application in the [AMap console](https://console.amap.com/) and obtain a **Web Service** API key. Browser, Android and iOS keys are different products. Enable the APIs your workflow uses and check your account's quota and commercial-use requirements. Enter the key in **Web Service API key**. If digital signatures are enabled for this key, also enter **Optional signing secret**; otherwise leave it blank. The signing secret is used to sign requests and is never sent as a separate parameter.

All calls target `https://restapi.amap.com`. AMap requires the key in query parameters, so infrastructure that logs outbound URLs must redact `key` and `sig`. The plugin does not include upstream bodies or credential-bearing URLs in errors. It does not follow redirects or automatically retry requests.

There is no automatic `health_check`: verification itself uses quota. Run `verify_credential` with a deliberate `address` to perform one geocoding request. Success verifies geocoding access only, not every other API's permissions or remaining quota.

## Operations

| Operation | Inputs and behavior |
| --- | --- |
| `geocode_address` | Required `address`, optional `city`; returns address matches and coordinates using `/v3/geocode/geo`. |
| `reverse_geocode` | Required `location`; optional `options.radius` from 0 to 3000 meters, default 1000; returns the formatted address and administrative components. |
| `search_places` | Required `query` of at most 80 characters; optional `options.region`, `city_limit`, category `types`, `page` and `page_size`; uses V5 text search. |
| `search_nearby` | Required `location`, optional `query` of at most 80 characters; accepts the same filters plus `radius` (0..50000 meters, default 5000) and `sort` (`distance` or `weight`, default `distance`). |
| `get_place` | Required `place_id`; returns basic V5 POI details as an array. An unknown ID can return an empty array. |
| `plan_route` | `mode` is `driving` or `walking`; provide `origin` and `destination`. Driving accepts `options.strategy` from 0 to 20, default 0. Walking rejects that option. |
| `get_weather` | Required six-digit `adcode`; returns current observations, not a forecast. |
| `list_districts` | Required district name or code in `query`; optional `options.depth` 0..3 (default 1) and `options.page` 1..10000 (default 1). Returns a flat district list with parent administrative codes. |
| `verify_credential` | Required probe `address`; performs one quota-consuming geocoding request. |

## Coordinates, pagination and output

Coordinates use AMap's GCJ-02 system in `longitude,latitude` order, with at most six decimal places. WGS-84/GPS coordinates must be converted outside this plugin. Longitude must be -180..180 and latitude -90..90. Routes return distances in meters, durations in seconds and tolls in yuan as strings, preserving upstream formatting. Walking routes are subject to AMap's 100 km service limit. The initial version does not provide transit routes or coordinate conversion.

Place search defaults to page 1 and 10 results; page size is 1..25 and page number is 1..200, further bounded by the 200-place recall window. `returned_count` is the actual number of returned places. A full page sets `has_more=true` and `next_page` to the following page, so the final request may return an empty array. AMap's reported count is not treated as a trustworthy total. When returned rows reach the 200-place recall cap, the operation sets `page_limit_reached=true`, `has_more=false` and `next_page=0`: the result is capped and may not be exhaustive. Requests starting beyond the first 200 places are rejected locally. A page size that does not divide 200 can produce a partial final page with the cap flag set. Narrow the query or region to retrieve additional matches.

District pages contain up to 20 root matches. Child districts are flattened with `parent_adcode`; child rows do not count toward the root page size. A full root page indicates a possible next page. The plugin bounds a response to 10000 district entries and pagination to 10000 pages. Street-level administrative codes can equal the parent's code, so `adcode` alone is not a unique identifier at every level.

The plugin normalizes documented missing textual fields (`[]` or `null`) to empty strings; unexpected nonempty arrays in textual fields cause errors. Empty result arrays are valid; missing collections or business failures are errors. Responses are limited to 8 MiB. Weather timestamps and administrative names retain the upstream format and language. UI labels and errors default to English, with Chinese translations available.

Example text search:

```json
{"query":"coffee","options":{"region":"110000","city_limit":false,"page":1,"page_size":10}}
```

## Validation and references

Tests use synthetic fixtures based on official response schemas. They exercise request parameters, optional zero/false values, pagination limits, signature encoding, generated platform bindings, oversized responses and redacted errors. They do not demonstrate live key permissions, quota or service availability.

Official documentation: [geocoding](https://lbs.amap.com/api/webservice/guide/api/georegeo), [V5 POI search](https://lbs.amap.com/api/webservice/guide/api-advanced/newpoisearch), [routing](https://lbs.amap.com/api/webservice/guide/api/direction), [weather](https://lbs.amap.com/api/webservice/guide/api/weatherinfo), [districts](https://lbs.amap.com/api/webservice/guide/api/district) and [digital signatures](https://lbs.amap.com/faq/quota-key/key/41181/).
