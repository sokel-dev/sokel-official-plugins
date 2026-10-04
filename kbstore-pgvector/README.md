# kbstore-pgvector

A knowledge-base storage engine plugin (Postgres + pgvector), implementing **the same** storage
contract as `kbstore-es`.

Its primary purpose is to **health-check the contract** — see
[docs/contract-notes.md](docs/contract-notes.md). Capability differences (most notably weaker
Chinese keyword search than ES) are covered in the usage doc inside `doc.go`.

## Run locally

```bash
docker run -d --name sokel-pgvector \
  -e POSTGRES_USER=sokel -e POSTGRES_PASSWORD=sokel -e POSTGRES_DB=kbstore \
  -p 5434:5432 pgvector/pgvector:pg16

# Live-database test cases (the whole group is skipped if this env var isn't set)
PGVECTOR_TEST_URL='postgres://sokel:sokel@localhost:5434/kbstore?sslmode=disable' go test ./...

# Register with the platform as a plugin
SOKEL_TOKEN=<access group token> SOKEL_ENDPOINT=http://localhost:8088 go run .
```

Two credential fields: `pg_url` (connection string), `namespace` (table name prefix, default kb).
One table per knowledge base.
