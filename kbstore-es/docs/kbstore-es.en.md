# Knowledge base storage (Elasticsearch)

Storage and retrieval backend for knowledge bases: chunk writes, vector search, BM25, and hybrid search with RRF fusion.

## Credentials

- **Elasticsearch URL**: for example, `https://es.example.com:9200`.
- **API Key** (optional): provide it when ES authentication is enabled.
- **Index namespace**: defaults to `sokel`; use it to separate environments sharing one ES cluster.

## Notes

- **ES 8.x is recommended**: the `knn` and RRF features used for hybrid search are fully available in 8.x.
- **Rebuild the index when changing embedding models**. Old and new vectors occupy different spaces; mixing them produces unexpectedly poor retrieval.
- The number of chunks per write is limited by message size. The plugin batches writes automatically,
  but very large batches you construct yourself can still exceed the limit.
