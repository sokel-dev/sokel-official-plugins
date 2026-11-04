# Meilisearch

Use your own Meilisearch community instance to create indexes, ingest documents, search content and inspect asynchronous indexing tasks. No Meilisearch Cloud account or paid feature is required. This release was exercised against self-hosted Meilisearch 1.56.1 and uses its standard REST endpoints. It does not configure an external AI provider.

## Deployment and credentials

Set the required deployment environment variable `SOKEL_MEILISEARCH_URL` to your service URL, such as `http://meilisearch:7700` or `https://search.example.internal/meili`. There is no public default or fallback. Internal HTTP and reverse-proxy path prefixes are supported. URLs must not contain embedded credentials, query parameters, fragments or dot path segments. Use HTTPS when traffic crosses an untrusted network.

Inside a container, `localhost` identifies that container, not the host or another service container. Use a reachable container-network service name or an appropriate host address for your deployment. The plugin does not install or start Meilisearch.

Enter an **API key** if authentication is enabled. It is sent as `Authorization: Bearer ...`. A blank key is supported only for an instance intentionally deployed without authentication. Use a regular API key with the required index scope and actions; the master key is not required by this plugin. Typical actions are `indexes.get`, `indexes.create`, `search`, `documents.get`, `documents.add`, `documents.delete` and `tasks.get`. Configure keys and rotate them outside the plugin.

`health_check` reads one index-list page and returns `ok` and `message`. It uses an authenticated route rather than the public `/health` endpoint, so a successful public liveness response cannot disguise an invalid key. Failure returns `ok=false` without creating data. A search-only key may fail this check while still being usable for search; health requires `indexes.get` and does not guarantee write or task permissions.

## Operations

| Operation | Inputs and result |
| --- | --- |
| `health_check` | Check service configuration and index-list access. |
| `list_indexes` | Optional `page`; return index metadata, `total`, `next_offset` and `has_more`. |
| `get_index` | `index_uid`; return UID, optional primary key and creation/update timestamps. |
| `create_index` | `index_uid` and optional `primary_key`; return an enqueued task receipt. |
| `search` | `index_uid`, optional `query` and `options`; return document JSON hits, estimated total hits, processing time, `next_offset` and `may_have_more`. An empty query performs placeholder search. |
| `get_document` | `index_uid` and `document_id`; return the document as an exact JSON object string. |
| `add_documents` | `index_uid`, `documents_json` and optional `primary_key`; enqueue an add-or-replace operation and return its task receipt. |
| `delete_document` | `index_uid` and one `document_id`; enqueue deletion and return its task receipt. |
| `get_task` | A `task_uid` string; return task metadata, `finished` and `succeeded`. It makes one request and does not poll. |

Index and document identifiers accept 1–512 ASCII letters, digits, hyphens or underscores. Use resource identifiers rather than URLs. Task IDs are canonical nonnegative decimal strings, including `"0"`; the plugin preserves their numeric precision. Returned `primary_key`, task index UID and task timestamps may be absent when the service returns null.

## Indexing and document JSON

Documents have application-defined fields, so `documents_json` is a string containing an array of nonempty JSON objects. This preserves nested values and integers larger than JavaScript's exact integer range. The plugin does not impose a fixed business-document schema. For example:

```json
{
  "index_uid": "books",
  "primary_key": "id",
  "documents_json": "[{\"id\":\"book-1\",\"title\":\"An Example\",\"available\":false}]"
}
```

`add_documents` uses **add or replace**, not partial update: an existing document with the same primary key is replaced, and omitted fields are removed. Meilisearch can create an index during ingestion when permissions allow. If no primary key is supplied or already configured, Meilisearch tries to infer it. Prefer an explicit primary key for predictable ingestion. The plugin validates the JSON array; Meilisearch validates primary-key values and document semantics asynchronously. A syntactically valid batch can therefore produce a failed task.

Use `get_document` or `search` after the task succeeds to read indexed content. Document and hit outputs are JSON strings, with false, null, nested values and large integers preserved. The plugin does not turn a document's own `error` or `code` field into an API failure. Search output reflects the index's displayed-attribute settings and requested projection; it is not necessarily the full source document.

## Task submission is not completion

Creation, ingestion and deletion normally return HTTP 202 with `status=enqueued`. That receipt only confirms that a task was accepted. Store its `task.uid`, then call `get_task` in a later workflow step or scheduled check. The plugin does not wait or automatically retry a write.

| Task status | `finished` | `succeeded` | Meaning |
| --- | --- | --- | --- |
| `enqueued` or `processing` | false | false | Still pending. |
| `succeeded` | true | true | The task completed successfully. |
| `failed` | true | false | Inspect `task.error_code` and the instance's task details. |
| `canceled` | true | false | The task was canceled. |

Raw task error messages are omitted because they can contain document data or other sensitive details; the stable error code is returned. `get_task` exposes failed and canceled states as normal typed results so workflows can branch on them. Check both completion and success before treating a mutation as applied. Deleting an ID that did not exist may still complete successfully; this is not an existence check.

## Search and pagination

Search options include `limit` (default 20, 1–1000), `offset` (default 0, 0–1000000), a `filter` expression, `sort` strings such as `year:desc`, and `attributes_to_retrieve`. Explicit zero offset and empty attribute arrays are preserved. Filters and sort fields must already be configured as filterable or sortable in Meilisearch; this plugin does not change index settings. It supports expression-string filters rather than the complete nested filter-array grammar.

```json
{
  "index_uid": "books",
  "query": "example",
  "options": {
    "limit": 20,
    "offset": 0,
    "filter": "available = false",
    "sort": ["year:desc"],
    "attributes_to_retrieve": ["id", "title"]
  }
}
```

Search fetches one page. A full page sets `may_have_more=true` and returns the next offset; this is deliberately a possibility, not a total-count guarantee. Continue with the same query/options and the returned offset until a short or empty page is received. `estimated_total_hits` is an estimate and is not used to prematurely end traversal. Meilisearch's `maxTotalHits` setting limits the searchable window, so search is not a full database export.

`list_indexes.page` uses the same limit/offset bounds. Continue while `has_more=true`, copying `next_offset` into `page.offset`; completion returns zero. The index-list total and offset are checked for consistency. Neither search nor index pagination is a snapshot: concurrent indexing or index changes can alter results.

## Limits, errors and scope

Each ingestion batch contains 1–1000 nonempty document objects and must fit a 1 MiB HTTP request. Responses are capped at 8 MiB. Query/filter strings are limited to 16 KiB, primary keys to 512 bytes and sort/projection lists to 100 entries. Calls have a 45-second transport timeout within the 60-second operation deadline. Meilisearch's own memory, indexing, key and search limits remain in effect.

Requests use only the configured service and reject redirects. The plugin does not retry failed HTTP responses or replay mutation request bodies. HTTP failures and malformed metadata/receipts fail explicitly without echoing service URLs, credentials or upstream error bodies. A timeout can leave a write uncertain: inspect the task list or target document before submitting again. Fix invalid document content before retrying a failed task.

This release does not delete indexes, change settings, create API keys, manage dumps/snapshots, invoke arbitrary API routes, enable enterprise features or configure AI embedders. Tests include documentation-derived synthetic request fixtures and recorded responses from a disposable local Meilisearch 1.56.1 community container. The local acceptance test verified both successful and failed asynchronous tasks; no production account or data was used.

## Official references and license

- [Meilisearch 1.56.1 release](https://github.com/meilisearch/meilisearch/releases/tag/v1.56.1)
- [Repository license split](https://github.com/meilisearch/meilisearch/blob/v1.56.1/LICENSE) and [MIT community license](https://github.com/meilisearch/meilisearch/blob/v1.56.1/LICENSE-MIT)
- [Authorization](https://www.meilisearch.com/docs/reference/api/authorization)
- [List indexes](https://www.meilisearch.com/docs/reference/api/indexes/list-all-indexes)
- [Create index](https://www.meilisearch.com/docs/reference/api/indexes/create-index)
- [Search](https://www.meilisearch.com/docs/reference/api/search/search-with-post)
- [Add or replace documents](https://www.meilisearch.com/docs/reference/api/documents/add-or-replace-documents)
- [Delete document](https://www.meilisearch.com/docs/reference/api/documents/delete-document)
- [Get task](https://www.meilisearch.com/docs/reference/api/async-task-management/get-task)

The repository contains both MIT community code and BUSL-1.1 enterprise code. This plugin uses community REST capabilities and does not require the enterprise edition; the entire upstream repository should not be described as MIT-only.
