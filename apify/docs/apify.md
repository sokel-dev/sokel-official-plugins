# Apify

Create an API token in the Apify console and save it in the plugin credential. Actor execution can incur charges; inspect the Actor's input schema and pricing before starting a run.

1. `run_actor` accepts an Actor ID or `owner~name`, optional JSON `input`, a build tag/number and optional run limits. It returns immediately with `run.id`, `run.status` and the available `run.default_dataset_id`. An omitted input sends an empty JSON object. The plugin sets `waitForFinish=0` and `restartOnError=false`.
2. `get_run` reads the latest state. FAILED, TIMED-OUT or ABORTED are returned as data; branch on `run.status`. Poll using workflow delays. No synchronous Actor endpoint or internal polling loop is used.
3. `list_runs` lists one Actor's runs with `page.offset`, `page.limit` and optional `page.desc`. Use `next_offset` only while `has_more` is true.
4. `get_dataset` reads dataset metadata. Item counts can lag by up to five seconds after writes.
5. `list_dataset_items` returns one JSON page. Pass a default dataset ID from a run, or a dataset ID/name. The output preserves arbitrary Actor-defined row fields. Pagination uses official response headers and validates them against the actual array. If the server omits or contradicts pagination metadata, the operation fails visibly. Dataset changes between requests can shift pages; this is not a snapshot.
6. `health_check` reads private account identity with `GET /v2/users/me`; it starts no Actor and creates no storage. It confirms token identity, not every Actor's permissions or available spending budget.

Example run input:

```json
{"actor_id":"owner~actor-name","input":{"startUrls":[{"url":"https://example.com"}]},"options":{"timeout_seconds":300,"max_items":100,"max_total_charge_usd":2}}
```

Run options preserve omitted values versus explicit zero. `timeout_seconds` accepts 0–604800; 0 follows Apify's unlimited-time convention. `max_items` accepts 0–1000000000 and caps paid result charges for pay-per-result Actors; it does not cap returned records. `max_total_charge_usd` is a nonnegative provider spending cap. When limits are omitted, the Actor/account configuration applies. Build/input compatibility and pricing remain Actor-specific.

Example dataset page:

```json
{"dataset_id":"dataset-id","page":{"offset":0,"limit":100,"desc":false}}
```

Page size defaults to 100 and accepts 1–1000; offset accepts 0–1000000000. Results are raw JSON objects, including hidden fields. Transformations such as clean/unwind/skip-empty, CSV exports, writes to datasets, abort/delete operations and token-in-query authentication are not implemented. Writes are never automatically retried. A timeout after submission is ambiguous: inspect Actor runs before resubmitting.

Deployment uses `SOKEL_APIFY_BASE_URL`, default `https://api.apify.com`. Credentials belong in the credential form. HTTPS is required except on loopback. All redirects are refused and tokens are sent only in Authorization headers to the deployment-configured API. HTTP requests time out after 45 seconds (health: 20 seconds). Request bodies are capped at 1 MiB and responses at 16 MiB; request fewer records if a page is too large. Error messages do not echo raw responses or tokens.

Tests use documentation-derived wire examples and synthetic edge cases, not live Actor runs. Official reference: https://docs.apify.com/api/v2/actors-runs-post
