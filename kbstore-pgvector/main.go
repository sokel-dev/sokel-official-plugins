// kbstore-pgvector: a knowledge-base storage engine plugin (Postgres + pgvector).
//
// **It exists to falsify a claim**: the v4 contract says "the storage engine is swappable", but
// with only kbstore-es as an implementation, there's no way to tell how much of the contract's
// shape is secretly Elasticsearch-shaped. A second implementation either satisfies these 8
// operations as-is, or surfaces where the contract doesn't fit — and whatever gets surfaced is
// recorded in docs/contract-notes.md; that's the actual output of this round.
//
// Division of labor is identical to the ES version: only storage and retrieval. Chunking /
// embedding / RRF fusion / rerank all live on the platform side.
//
// Structural choice: **one table per knowledge base** (matching ES's one index per knowledge
// base). This isn't a style preference — pgvector's `vector(N)` bakes the dimension into the
// column type, and different knowledge bases have different dimensions, so a shared table simply
// couldn't have an index built on it. The contract conveniently already passes dims in at
// kb_create, so this lines up.
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen
// The contract is provided by zz_sokel.go (AST-generated, not runtime reflection). Regenerate after
// changing an input/output struct or its tags.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sokel-dev/sokel-official-plugins/kbstore-pgvector/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" {
		log.Fatal("请设置 SOKEL_TOKEN")
	}
	p := sokel.New(sokel.Config{
		Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "kbstore-pgvector",
	})
	RegisterCredential(p)
	// Self-reported capabilities — **this plugin is the actual reason this mechanism exists**
	// (docs/contract-notes.md groups A/C): recency isn't implemented (ES has distance_feature to
	// handle it in one shot, PG would need a hand-written decay expression); the keyword leg is
	// trigram similarity rather than BM25 with Chinese tokenization.
	// Without reporting this, the platform would silently ignore it, and a user who configured
	// recency weighting would see no effect at all — which is worse than "unsupported".
	p.SetCapabilities(map[string]bool{
		sokel.CapKeywordBM25: false, sokel.CapRecency: false,
		sokel.CapTimeRange: true, sokel.CapFieldBoosts: true,
		sokel.CapArrayFilters: true, // array fields match element-wise since 0812062
	})
	p.SetDoc(usageDoc, "")
	OnKbCreate(p, opKBCreate)
	OnKbDrop(p, opKBDrop)
	OnChunksUpsert(p, opUpsert)
	OnDocDelete(p, opDocDelete)
	OnVectorQuery(p, opVectorQuery)
	OnKeywordQuery(p, opKeywordQuery)
	OnChunksBrowse(p, opBrowse)
	OnMget(p, opMget)
	OnHealthCheck(p, opHealthCheck)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// —— Connections ——
//
// The connection pool is cached by DSN: creating a new pool per operation would mean dozens of
// handshakes for a single ingestion run of dozens of batches.
var pools = map[string]*pgxpool.Pool{}

type store struct {
	pool *pgxpool.Pool
	ns   string
}

func storeOf(ctx sokel.Ctx) (*store, error) {
	c := ctx.Credential()
	if c == nil || c["pg_url"] == "" {
		return nil, fmt.Errorf("缺少 Postgres 凭证(pg_url)")
	}
	ns := c["namespace"]
	if ns == "" {
		ns = "kb"
	}
	return openStore(c["pg_url"], ns)
}

func openStore(dsn, ns string) (*store, error) {
	if p, ok := pools[dsn]; ok {
		return &store{pool: p, ns: ns}, nil
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("连接串不合法: %w", err)
	}
	cfg.MaxConns = 8
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("连不上 Postgres: %w", err)
	}
	pools[dsn] = p
	return &store{pool: p, ns: ns}, nil
}

// safeIdent: kb_id → table name.
//
// The table name can't go through parameter binding (a SQL identifier isn't a value), so it has to
// be sanitized by hand: keep only [a-z0-9_], replace everything else with _. This is the only
// place in the plugin that concatenates an identifier, so the injection surface is contained to
// this one function.
var identBad = regexp.MustCompile(`[^a-z0-9_]`)

func (s *store) table(kbID string) string {
	id := identBad.ReplaceAllString(strings.ToLower(kbID), "_")
	if len(id) > 48 {
		id = id[:48]
	}
	return fmt.Sprintf("%s_%s", identBad.ReplaceAllString(strings.ToLower(s.ns), "_"), id)
}

// —— kb_create / kb_drop ——

func opKBCreate(ctx sokel.Ctx, in *KbCreateIn) (*KbCreateOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &KbCreateOut{}, err
	}
	dims := in.Dims
	if dims <= 0 {
		dims = 1024
	}
	t := st.table(in.KbID)
	c := context.Background()
	// Extensions are created as needed (vector is required; pg_trgm backs the keyword leg — see the
	// comment on opKeywordQuery).
	for _, ext := range []string{"vector", "pg_trgm"} {
		if _, err := st.pool.Exec(c, "CREATE EXTENSION IF NOT EXISTS "+ext); err != nil {
			return &KbCreateOut{}, fmt.Errorf("建扩展 %s 失败(需要超级用户或预装): %w", ext, err)
		}
	}
	// Metadata is unified into JSONB: the keys and types of `fields` in the contract are decided by
	// the knowledge base's declaration, only known at runtime. Making them real columns would mean a
	// DDL change for every new field — ES's counterpart here is dynamic mapping, and JSONB plays
	// that role on this side.
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id            TEXT PRIMARY KEY,
		doc_id        TEXT NOT NULL DEFAULT '',
		content       TEXT NOT NULL DEFAULT '',
		title         TEXT NOT NULL DEFAULT '',
		summary       TEXT NOT NULL DEFAULT '',
		datetime      TIMESTAMPTZ,
		role          TEXT NOT NULL DEFAULT '',
		parent_id     TEXT NOT NULL DEFAULT '',
		parent_no     INT  NOT NULL DEFAULT 0,
		child_no      INT  NOT NULL DEFAULT 0,
		page_no       INT  NOT NULL DEFAULT 0,
		content_html  TEXT NOT NULL DEFAULT '',
		boundary      TEXT NOT NULL DEFAULT '',
		fields        JSONB NOT NULL DEFAULT '{}',
		images        JSONB NOT NULL DEFAULT '[]',
		assets        JSONB NOT NULL DEFAULT '[]',
		source_blocks JSONB NOT NULL DEFAULT '[]',
		embedding     vector(%d)
	)`, t, dims)
	if _, err := st.pool.Exec(c, ddl); err != nil {
		return &KbCreateOut{}, fmt.Errorf("建表失败: %w", err)
	}
	for _, idx := range []string{
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_doc ON %s (doc_id)`, t, t),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_parent ON %s (parent_id)`, t, t),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_dt ON %s (datetime)`, t, t),
		// The ?| used for metadata filtering goes through GIN (jsonb_ops supports ?|); a pre-existing
		// table gets this added on its next (idempotent) kb_create.
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_fields ON %s USING gin (fields)`, t, t),
		// The keyword leg uses trigram (reasoning in the opKeywordQuery comment): GIN + gin_trgm_ops.
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_trgm ON %s USING gin (content gin_trgm_ops)`, t, t),
		// The vector index uses HNSW (pgvector ≥0.5): cosine distance, matching the <=> operator used
		// at query time. Building it on an empty table is deliberate: pgvector's HNSW supports
		// incremental inserts, unlike ivfflat, which needs data to train on first.
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_vec ON %s USING hnsw (embedding vector_cosine_ops)`, t, t),
	} {
		if _, err := st.pool.Exec(c, idx); err != nil {
			return &KbCreateOut{}, fmt.Errorf("建索引失败: %w", err)
		}
	}
	return &KbCreateOut{OK: true}, nil
}

func opKBDrop(ctx sokel.Ctx, in *KbDropIn) (*KbDropOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &KbDropOut{}, err
	}
	if _, err := st.pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+st.table(in.KbID)); err != nil {
		return &KbDropOut{}, fmt.Errorf("删库失败: %w", err)
	}
	return &KbDropOut{OK: true}, nil
}

// —— Writes ——

func opUpsert(ctx sokel.Ctx, in *ChunksUpsertIn) (*ChunksUpsertOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &ChunksUpsertOut{}, err
	}
	t := st.table(in.KbID)
	c := context.Background()
	tx, err := st.pool.Begin(c)
	if err != nil {
		return &ChunksUpsertOut{}, err
	}
	defer func() { _ = tx.Rollback(c) }()

	// append=false (the first batch of each document) → clear that document's old chunks first,
	// which gives overwrite semantics. The platform batches by the NATS single-frame size limit, and
	// only the first batch has append=false; clearing on every batch would delete what the previous
	// batch just wrote.
	if !in.Append && in.DocID != "" {
		if _, err := tx.Exec(c, fmt.Sprintf("DELETE FROM %s WHERE doc_id = $1", t), in.DocID); err != nil {
			return &ChunksUpsertOut{}, fmt.Errorf("清旧分块失败: %w", err)
		}
	}
	n := 0
	for _, ch := range in.Chunks {
		if ch.ID == "" {
			continue
		}
		docID := ch.DocID
		if docID == "" {
			docID = in.DocID
		}
		var vec any
		if len(ch.Embedding) > 0 {
			vec = vecLiteral(ch.Embedding)
		}
		_, err := tx.Exec(c, fmt.Sprintf(`
			INSERT INTO %s (id, doc_id, content, title, summary, datetime, role, parent_id,
			                parent_no, child_no, page_no, content_html, boundary,
			                fields, images, assets, source_blocks, embedding)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::timestamptz,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18::vector)
			ON CONFLICT (id) DO UPDATE SET
			  doc_id=EXCLUDED.doc_id, content=EXCLUDED.content, title=EXCLUDED.title,
			  summary=EXCLUDED.summary, datetime=EXCLUDED.datetime, role=EXCLUDED.role,
			  parent_id=EXCLUDED.parent_id, parent_no=EXCLUDED.parent_no, child_no=EXCLUDED.child_no,
			  page_no=EXCLUDED.page_no, content_html=EXCLUDED.content_html, boundary=EXCLUDED.boundary,
			  fields=EXCLUDED.fields, images=EXCLUDED.images, assets=EXCLUDED.assets,
			  source_blocks=EXCLUDED.source_blocks,
			  -- 向量为空时保留原值:平台分批写入时并非每批都带 embedding。
			  embedding=COALESCE(EXCLUDED.embedding, %s.embedding)`, t, t),
			ch.ID, docID, ch.Content, ch.Title, ch.Summary, ch.Datetime, ch.Role, ch.ParentID,
			ch.ParentNo, ch.ChildNo, ch.PageNo, ch.ContentHTML, ch.Boundary,
			jsonOf(ch.Fields), jsonOf(ch.Images), jsonOf(ch.Assets), jsonOf(ch.SourceBlocks), vec)
		if err != nil {
			return &ChunksUpsertOut{}, fmt.Errorf("写入分块 %s 失败: %w", ch.ID, err)
		}
		n++
	}
	if err := tx.Commit(c); err != nil {
		return &ChunksUpsertOut{}, err
	}
	return &ChunksUpsertOut{OK: true, Count: n}, nil
}

// datetime comes in as a **string** (that's how the contract defines it — ES just eats the ISO
// string directly). Postgres needs an explicit cast, and an empty string has to become NULL first —
// `''::timestamptz` errors outright. Hence the hardcoded NULLIF($6,'')::timestamptz in the SQL.

func opDocDelete(ctx sokel.Ctx, in *DocDeleteIn) (*DocDeleteOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &DocDeleteOut{}, err
	}
	if _, err := st.pool.Exec(context.Background(),
		fmt.Sprintf("DELETE FROM %s WHERE doc_id = $1", st.table(in.KbID)), in.DocID); err != nil {
		return &DocDeleteOut{}, fmt.Errorf("删文档失败: %w", err)
	}
	return &DocDeleteOut{OK: true}, nil
}

// —— Retrieval ——

func opVectorQuery(ctx sokel.Ctx, in *VectorQueryIn) (*VectorQueryOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &VectorQueryOut{}, err
	}
	if len(in.Embedding) == 0 {
		return &VectorQueryOut{}, fmt.Errorf("向量为空")
	}
	k := in.K
	if k <= 0 {
		k = 50
	}
	where, args := buildWhere(in.Filters, in.TimeRange)
	args = append(args, vecLiteral(toF32(in.Embedding)))
	vecArg := len(args)
	// Similarity uses cosine: it must match the vector_cosine_ops used when building the index,
	// otherwise the index won't be used at all (pgvector picks an index by operator class — using
	// the wrong operator means a silent, slow full table scan).
	// score = 1 - distance, pointing the same way as ES's knn score (bigger = more relevant). The
	// platform-side RRF only looks at rank, but the display needs the actual value.
	q := fmt.Sprintf(`SELECT %s, 1 - (embedding <=> $%d) AS score
		FROM %s WHERE embedding IS NOT NULL %s
		ORDER BY embedding <=> $%d LIMIT %d`, chunkCols, vecArg, st.table(in.KbID), where, vecArg, k)
	return &VectorQueryOut{Hits: scanHits(context.Background(), st, q, args)}, nil
}

func opKeywordQuery(ctx sokel.Ctx, in *KeywordQueryIn) (*KeywordQueryOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &KeywordQueryOut{}, err
	}
	k := in.K
	if k <= 0 {
		k = 50
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return &KeywordQueryOut{}, nil
	}
	where, args := buildWhere(in.Filters, in.TimeRange)
	args = append(args, query)
	qArg := len(args)
	// The keyword leg uses **trigram similarity**, not to_tsvector.
	//
	// This is the single biggest gap between this plugin and the ES version, and it needs to be
	// spelled out: Postgres's built-in tokenizer is useless for Chinese ('simple' splits on
	// whitespace/punctuation, so a Chinese sentence becomes one giant token and `to_tsquery` hit
	// rates approach 0), whereas the ES side has ik attached. Doing Chinese BM25 on PG requires
	// installing zhparser/pg_jieba — that's a deployment requirement the plugin shouldn't silently
	// assume. Trigram at least gives a **meaningful ranking** for mixed Chinese/English text, at the
	// cost of weaker recall than ik, especially for longer queries. Choosing it is a deliberate
	// tradeoff, not an oversight.
	// title/summary weighting: aligned in intent with the ES version's field_boosts (which uses
	// multi_match^boost).
	q := fmt.Sprintf(`SELECT %s,
		  GREATEST(similarity(content, $%d), similarity(title, $%d) * 1.5, similarity(summary, $%d) * 1.2) AS score
		FROM %s
		WHERE (content %% $%d OR title %% $%d OR summary %% $%d) %s
		ORDER BY score DESC LIMIT %d`,
		chunkCols, qArg, qArg, qArg, st.table(in.KbID), qArg, qArg, qArg, where, k)
	return &KeywordQueryOut{Hits: scanHits(context.Background(), st, q, args)}, nil
}

func opBrowse(ctx sokel.Ctx, in *ChunksBrowseIn) (*ChunksBrowseOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &ChunksBrowseOut{}, err
	}
	k := in.K
	if k <= 0 {
		k = 20
	}
	where, args := buildWhere(in.Filters, in.TimeRange)
	q := fmt.Sprintf(`SELECT %s, 0::float8 AS score FROM %s WHERE TRUE %s
		ORDER BY doc_id, parent_no, child_no LIMIT %d OFFSET %d`,
		chunkCols, st.table(in.KbID), where, k, in.Offset)
	hits := scanHits(context.Background(), st, q, args)
	chunks := make([]schema.Chunk, 0, len(hits))
	for _, h := range hits {
		chunks = append(chunks, h.Chunk)
	}
	return &ChunksBrowseOut{Chunks: chunks}, nil
}

func opMget(ctx sokel.Ctx, in *MgetIn) (*MgetOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &MgetOut{}, err
	}
	if len(in.IDs) == 0 {
		return &MgetOut{Chunks: []schema.Chunk{}}, nil
	}
	q := fmt.Sprintf(`SELECT %s, 0::float8 AS score FROM %s WHERE id = ANY($1)`, chunkCols, st.table(in.KbID))
	hits := scanHits(context.Background(), st, q, []any{in.IDs})
	// Order is restored to match the requested ids: the platform fetches by a list of parent-chunk
	// ids and takes the returned order at face value (ES's _mget guarantees request-order results,
	// SQL's = ANY makes no such guarantee — this kind of "the other side just happens to guarantee
	// it" is exactly the trap a second implementation is most likely to fall into, which is also the
	// whole point of building one).
	byID := map[string]schema.Chunk{}
	for _, h := range hits {
		byID[h.Chunk.ID] = h.Chunk
	}
	out := make([]schema.Chunk, 0, len(in.IDs))
	for _, id := range in.IDs {
		if ch, ok := byID[id]; ok {
			out = append(out, ch)
		}
	}
	return &MgetOut{Chunks: out}, nil
}

// —— Shared ——

// chunkCols: the column list for the read path. **Excludes embedding** — it only travels upstream
// on writes; sending it back would mean every hit drags a few KB of floats across NATS (the ES
// version excludes it via the srcFields allowlist, same principle).
const chunkCols = `id, doc_id, content, title, summary,
	COALESCE(to_char(datetime, 'YYYY-MM-DD"T"HH24:MI:SSOF'), '') AS datetime,
	role, parent_id, parent_no, child_no, page_no, content_html, boundary,
	fields, images, assets, source_blocks`

// buildWhere: filters + time range → a SQL fragment (parameters numbered starting at $1).
func buildWhere(filters []schema.Filter, tr schema.TimeRange) (string, []any) {
	var sb strings.Builder
	args := []any{}
	for _, f := range filters {
		if f.Field == "" {
			continue
		}
		// Metadata lives uniformly in the fields JSONB. A field can be a scalar or an array of
		// scalars, and matching any one element counts as a hit (contract semantics, matching ES's
		// terms): fields->'x' ?| $n matches string arrays element-wise and string scalars by value;
		// fields->>'x' = ANY($n) covers numeric/boolean scalars (compared as text).
		key := quoteLit(f.Field)
		switch {
		case f.Missing:
			sb.WriteString(fmt.Sprintf(" AND (fields->>%s IS NULL)", key))
		case len(f.Values) > 0:
			args = append(args, f.Values)
			hit := fmt.Sprintf("(fields->>%s = ANY($%d) OR fields->%s ?| $%d)", key, len(args), key, len(args))
			if f.Exclude {
				// COALESCE: a row missing this field evaluates hit as NULL, and NOT NULL would drop
				// the whole row — but exclusion semantics mean these rows should be kept.
				sb.WriteString(fmt.Sprintf(" AND NOT COALESCE(%s, false)", hit))
			} else {
				sb.WriteString(" AND " + hit)
			}
		}
	}
	if tr.From != "" {
		args = append(args, tr.From)
		sb.WriteString(fmt.Sprintf(" AND datetime >= $%d::timestamptz", len(args)))
	}
	if tr.To != "" {
		args = append(args, tr.To)
		sb.WriteString(fmt.Sprintf(" AND datetime <= $%d::timestamptz", len(args)))
	}
	return sb.String(), args
}

// quoteLit safely embeds a field name into a SQL string literal (JSONB key access can't be
// parameter-bound in that position).
func quoteLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func scanHits(c context.Context, st *store, q string, args []any) []schema.Hit {
	rows, err := st.pool.Query(c, q, args...)
	if err != nil {
		log.Printf("查询失败: %v\nSQL: %s", err, q)
		return []schema.Hit{}
	}
	defer rows.Close()
	out := []schema.Hit{}
	for rows.Next() {
		var ch schema.Chunk
		var fields, images, assets, blocks []byte
		var score float64
		if err := rows.Scan(&ch.ID, &ch.DocID, &ch.Content, &ch.Title, &ch.Summary, &ch.Datetime,
			&ch.Role, &ch.ParentID, &ch.ParentNo, &ch.ChildNo, &ch.PageNo, &ch.ContentHTML, &ch.Boundary,
			&fields, &images, &assets, &blocks, &score); err != nil {
			log.Printf("扫描失败: %v", err)
			continue
		}
		_ = json.Unmarshal(fields, &ch.Fields)
		_ = json.Unmarshal(images, &ch.Images)
		_ = json.Unmarshal(assets, &ch.Assets)
		_ = json.Unmarshal(blocks, &ch.SourceBlocks)
		out = append(out, schema.Hit{ID: ch.ID, Score: score, Chunk: ch})
	}
	return out
}

// vecLiteral: pgvector takes input as '[1,2,3]' text, not an array parameter.
func vecLiteral(v []float32) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%g", f)
	}
	sb.WriteByte(']')
	return sb.String()
}

func toF32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(f)
	}
	return out
}

func jsonOf(v any) []byte {
	if v == nil {
		return []byte("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

var _ = time.Now // kept: for a future timeout

// —— Credential health check ——

// opHealthCheck connects to the database and reports its version and key extensions.
//
// When unavailable, returns ok=false + message, **not** an error: the platform treats an error as
// "this plugin can't run its health check", and ok=false as "the check concluded the plugin is
// unavailable" — the latter is what should be said here, and the raw Postgres message (connection
// refused / password authentication failed / database doesn't exist) is the one genuinely useful
// thing for a human troubleshooting this.
func opHealthCheck(ctx sokel.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	// This uses ctx rather than the context.Background() used elsewhere in the plugin: when the
	// connection fails, pgx keeps retrying, and only tying it to the platform's operation timeout
	// gives the spinner on the credential page a way to actually finish.
	var version string
	if err := st.pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return &HealthCheckOut{Message: fmt.Sprintf("连不上 Postgres: %v", err)}, nil
	}
	out := &HealthCheckOut{OK: true, Version: shortPGVersion(version)}
	// Extensions are **extra intel**: failing to read pg_extension (a database with tightened
	// permissions) doesn't overturn the "it connects" conclusion. But a missing vector extension must
	// be called out — it's a precondition for this plugin to work at all, and without this check it
	// wouldn't blow up until the moment a knowledge base gets created.
	if rows, qerr := st.pool.Query(ctx,
		`SELECT extname, extversion FROM pg_extension WHERE extname IN ('vector','pg_trgm') ORDER BY extname`); qerr == nil {
		var exts []string
		for rows.Next() {
			var name, ver string
			if rows.Scan(&name, &ver) == nil {
				exts = append(exts, name+" "+ver)
			}
		}
		rows.Close()
		out.Extensions = strings.Join(exts, ", ")
	}
	out.Message = fmt.Sprintf("连接正常（%s）", out.Version)
	switch {
	case out.Extensions == "":
		out.Message += "；未能读到扩展清单（权限不足或确实没装，建库时会再报一次）"
	case !strings.Contains(out.Extensions, "vector"):
		out.Message += "；⚠️ 缺 vector 扩展，建知识库会失败（需超级用户执行 CREATE EXTENSION vector）"
	default:
		out.Message += "；扩展：" + out.Extensions
	}
	return out, nil
}

// shortPGVersion: version() returns a whole line of build info ("PostgreSQL 16.2 (Debian …) on
// x86_64 …"), and the UI only needs the first two words — the trailing compiler/platform details
// would just crowd out what people actually want to see.
func shortPGVersion(v string) string {
	f := strings.Fields(v)
	if len(f) >= 2 {
		return f[0] + " " + f[1]
	}
	return strings.TrimSpace(v)
}
