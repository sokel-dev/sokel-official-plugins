# kbstore-pgvector (knowledge base storage: Postgres + pgvector)

Implements the same storage contract as **kbstore-es**, with eight internal operations, so it can replace the backend for an entire knowledge base.
Its primary purpose is actually to **exercise the contract**: with only one implementation, it is hard to see how much ES-specific structure has leaked into the contract.

## Dependencies

- Postgres 13+ with the `vector` extension (pgvector ≥0.5 for HNSW) and `pg_trgm`.
- Only two credential fields: the `pg_url` connection string and the `namespace` table prefix, which defaults to kb.

After configuration, click Check credentials to see the PostgreSQL version and whether `vector` / `pg_trgm` are available.
Missing extensions are the most common deployment problem. Without this check, the failure may appear only when creating a knowledge base.

Each knowledge base has its own table (`<namespace>_<kb_id>`), because pgvector's `vector(N)` fixes the dimension in the column type.
Knowledge bases with different dimensions cannot share one table with a usable index.

## Capability differences from the ES backend (read before choosing)

| Dimension | kbstore-es | This plugin |
|---|---|---|
| Vector search | HNSW / cosine | HNSW / cosine (equivalent) |
| Keyword search | BM25 + ik Chinese tokenization | **Trigram similarity** |
| Recency weighting | distance_feature | Not implemented; the recency input is ignored. |
| Field weighting | multi_match ^boost | Fixed weights: title ×1.5 / summary ×1.2. |

**The keyword-search difference matters**. PostgreSQL's built-in tokenizers do not handle Chinese effectively: 'simple' splits on whitespace,
so an entire Chinese sentence becomes one large token and to_tsquery has nearly zero recall. Chinese BM25 requires zhparser/pg_jieba.
Trigrams produce meaningful rankings for mixed Chinese and English, but retrieval quality is weaker than ik, especially for long queries.
For predominantly Chinese knowledge bases where keyword search is important, choose ES.
