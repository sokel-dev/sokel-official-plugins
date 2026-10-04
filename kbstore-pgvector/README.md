# kbstore-pgvector

知识库存储引擎插件（Postgres + pgvector），与 `kbstore-es` 实现**同一份**存储契约。

它的第一用途是**给契约做体检**——见 [docs/contract-notes.md](docs/contract-notes.md)。
能力差异（尤其中文关键词检索弱于 ES）见 `doc.go` 里的使用说明。

## 本地跑

```bash
docker run -d --name sokel-pgvector \
  -e POSTGRES_USER=sokel -e POSTGRES_PASSWORD=sokel -e POSTGRES_DB=kbstore \
  -p 5434:5432 pgvector/pgvector:pg16

# 真库用例（不设这个环境变量则整组跳过）
PGVECTOR_TEST_URL='postgres://sokel:sokel@localhost:5434/kbstore?sslmode=disable' go test ./...

# 作为插件接入平台
SOKEL_TOKEN=<接入组 token> SOKEL_ENDPOINT=http://localhost:8088 go run .
```

凭证两项：`pg_url`（连接串）、`namespace`（表名前缀，默认 kb）。一库一表。
