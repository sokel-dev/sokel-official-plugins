# Elasticsearch

Connect ES to workflows to query logs and documents, ingest data, and manage indices. **ES 7/8/9 and OpenSearch are supported**:
the plugin uses standard REST APIs without pinning a client version.

## Configure credentials

| Field | What to enter |
|---|---|
| URL | Cluster URL with scheme and port, such as `https://es.internal:9200`. For multiple nodes, one node's address is sufficient. |
| Username / password | Basic authentication; the default ES 8 account is `elastic`. |
| API Key | The encoded value generated in Kibana → Stack Management → **API Keys**. When provided, username/password are not used. **Recommended** because access can be restricted by index and revoked independently. |
| Skip certificate verification | Select `on` for a self-hosted cluster with a self-signed certificate. The connection remains encrypted, but the peer's identity is not verified. |

After configuration, click Check credentials. A successful connection shows the distribution, version, cluster name, and cluster health.

## Choose an operation

- **Find data** → Search. Choose one of two query forms: **Query** accepts raw ES Query DSL
  (follow the official documentation, for example `{"bool":{"must":[…]}}`); **Query string** accepts a one-line Lucene expression
  such as `status:error AND host:a1`, useful for exploratory queries. Use `@timestamp:desc` for sorting.
- **Get only a count** → Count documents. Its count is **exact**; the search total may not be, as explained below.
- **Access documents by ID** → Get / Index / Update / Delete document.
  Update changes only the supplied fields; indexing replaces the whole document.
- **Ingest a batch** → Bulk index. One request writes multiple documents, roughly an order of magnitude faster than individual requests.
- **Calculate distributions or trends** → Supply **Aggregations** in Search, such as `{"by_host":{"terms":{"field":"host.keyword"}}}`.
  Leave the document count empty to return only aggregations, without documents.
- **Manage indices** → List / Create / Delete indices, Get / Add mappings, Switch aliases, or Reindex.
- **Use an API not listed above** → Generic call, for `_tasks`, `_ilm`, `_snapshot`, `_sql`, and others.

## Idempotency: prevent duplicate data on reruns

Workflows may retry or be triggered repeatedly. **Use a business primary key as the document ID** so a rerun overwrites the same document instead of creating another.
Set Document ID for Index document or ID field for Bulk index. Without an ID, ES generates a new one each time,
so running the same batch twice creates two copies.

## Common pitfalls

- **Search hit totals are exact only up to 10000 by default**. Above that, ES returns a lower bound, and the
  Total is a lower bound output is true. Enable Track exact total for an accurate total, at extra cost, or use Count documents.
- **Writes are not immediately searchable**. ES normally refreshes visibility after about one second. A missing result immediately after a write is often due to this delay.
  Enable Immediate search visibility for an individual write when needed, but **not inside a bulk loop**: forcing a shard refresh each time is slow.
- **Existing field types cannot be changed**. This is an ES limitation. The path is
  Create a new index with new mappings → Reindex → Switch aliases. All three operations are available here.
  Readers that consistently use the alias need no changes.
- **`text` fields cannot be aggregated or sorted directly**. Use their `.keyword` subfield,
  such as `host.keyword` for `host`. Get mappings shows the actual field types.
- **Destructive operations have safeguards**. Delete by query requires a query; omitting it would delete the whole index. Delete index rejects `*` and `_all`,
  which would delete many indices. The plugin rejects these requests and explains which operation to use.
- **Bulk indexing can partially succeed**. Failed entries do not roll back the batch. Outputs include Successful count, Failed count, and Failure reasons.
  **Check Failed count in the workflow**, rather than only checking whether the operation returned an error.
- **Pagination reaches at most the first 10000 documents**, the hard limit for from + size. For a full export, use Reindex,
  or use Generic call with `search_after` / PIT.
