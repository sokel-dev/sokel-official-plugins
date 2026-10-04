// kbstore-pgvector：知识库存储引擎插件（Postgres + pgvector）。
//
// **它存在的理由是证伪**：v4 契约写着「存储引擎可替换」，但在只有 kbstore-es 一个实现时，
// 契约里混进了多少 Elasticsearch 的形状是看不出来的。第二家实现要么原样满足这 8 个操作，
// 要么把不合理之处顶出来——顶出来的都记在 docs/contract-notes.md，那才是这轮的产出。
//
// 与 ES 版的分工完全一致：只做存取。切分 / embedding / RRF 融合 / rerank 全在平台侧。
//
// 结构选择：**一库一表**（与 ES 的一库一索引对应）。不是风格问题——pgvector 的
// `vector(N)` 维度写死在列类型上，不同知识库维度不同，塞一张表里根本建不出索引。
// 契约恰好在 kb_create 就把 dims 传进来了，所以对得上。
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen
// 契约由 zz_sokel.go 提供（AST 生成，非运行时反射）。改了入/出参 struct 或其 tag 后须重新生成。

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
	// 能力自报——**这个插件正是这套机制的起因**（docs/contract-notes.md A/C 组）：
	// recency 没实现（ES 有 distance_feature 一把梭，PG 要自己写衰减表达式）；
	// 关键词腿是 trigram 相似度而不是带中文分词的 BM25。
	// 不报的话平台只能静默忽略，用户配了时效加权却毫无体现——那比"不支持"更坏。
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

// —— 连接 ——
//
// 连接池按 DSN 缓存：每次操作新建池的话，一次摄入几十批就是几十次握手。
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

// safeIdent：kb_id → 表名。
//
// 表名不能走参数绑定（SQL 标识符不是值），所以必须自己收口：只留 [a-z0-9_]，其余换 _。
// 这是本插件唯一一处拼接标识符的地方，注入面收在这一个函数里。
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
	// 扩展按需建（vector 必需；pg_trgm 供关键词腿——见 opKeywordQuery 的说明）。
	for _, ext := range []string{"vector", "pg_trgm"} {
		if _, err := st.pool.Exec(c, "CREATE EXTENSION IF NOT EXISTS "+ext); err != nil {
			return &KbCreateOut{}, fmt.Errorf("建扩展 %s 失败(需要超级用户或预装): %w", ext, err)
		}
	}
	// 元数据统一进 JSONB：契约里 fields 的键与类型由本库声明决定，运行期才知道，
	// 建成真列的话每加一个字段都要 DDL——而 ES 那边是动态 mapping。JSONB 是这里的对应物。
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
		// 元数据过滤的 ?| 走 GIN（jsonb_ops 支持 ?|）；老库在下一次 kb_create（幂等）时补上。
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_fields ON %s USING gin (fields)`, t, t),
		// 关键词腿走 trigram(理由见 opKeywordQuery)：GIN + gin_trgm_ops。
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_trgm ON %s USING gin (content gin_trgm_ops)`, t, t),
		// 向量索引用 HNSW（pgvector ≥0.5）：cosine 距离，与检索时的 <=> 对齐。
		// 建在空表上是刻意的：pgvector 的 HNSW 支持增量插入，不像 ivfflat 需要先有数据训练。
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

// —— 写入 ——

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

	// append=false（每篇文档的第一批）→ 先清掉该文档的旧分块 = 覆盖语义。
	// 平台按 NATS 单帧上限分批，只有第一批是 false；每批都清的话后一批会把前一批刚写的删掉。
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

// datetime 是**字符串**进来的（契约如此，ES 那边直接吃 ISO 串）。Postgres 要显式转换，
// 且空串必须先变 NULL——`''::timestamptz` 会直接报错。故 SQL 里写死 NULLIF($6,'')::timestamptz。

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

// —— 检索 ——

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
	// 相似度用 cosine：与建索引时的 vector_cosine_ops 必须一致，否则索引根本不会被用上
	// （pgvector 按算子类选索引，用错算子就是全表扫，安静地慢）。
	// score = 1 - 距离，与 ES 的 knn score 同向（越大越相关），平台侧 RRF 只看序，但显示要看值。
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
	// 关键词腿用 **trigram 相似度**，不是 to_tsvector。
	//
	// 这是本插件与 ES 差距最大的一处，得写清楚：Postgres 自带的分词器对中文无能为力
	// （'simple' 按空白/标点切，一句中文就是一个巨型 token，`to_tsquery` 命中率接近 0），
	// 而 ES 那边挂的是 ik。要在 PG 上做中文 BM25 得装 zhparser/pg_jieba——那是部署要求，
	// 不该由插件偷偷假设。trigram 至少对中英混排都能给出**有意义的排序**，代价是
	// 召回质量弱于 ik，长查询尤甚。选它是清醒的取舍，不是没想到。
	// title/summary 加权：与 ES 版 field_boosts 的意图对齐（那边是 multi_match^boost）。
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
	// 顺序按请求的 ids 还原：平台按父块 id 列表取，回来的顺序它是当真的
	// （ES 的 _mget 保证按请求序返回，SQL 的 = ANY 不保证——这类"另一边刚好有的保证"
	//   是第二实现最容易踩的坑，也正是做它的意义）。
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

// —— 公共 ——

// chunkCols：读路径的列清单。**不含 embedding**——它只在写入时上行，
// 回流的话每条命中都要背着几 KB 的浮点数穿过 NATS（ES 版用 srcFields 白名单排除，同一口径）。
const chunkCols = `id, doc_id, content, title, summary,
	COALESCE(to_char(datetime, 'YYYY-MM-DD"T"HH24:MI:SSOF'), '') AS datetime,
	role, parent_id, parent_no, child_no, page_no, content_html, boundary,
	fields, images, assets, source_blocks`

// buildWhere：过滤条件 + 时间范围 → SQL 片段（参数从 $1 起编号）。
func buildWhere(filters []schema.Filter, tr schema.TimeRange) (string, []any) {
	var sb strings.Builder
	args := []any{}
	for _, f := range filters {
		if f.Field == "" {
			continue
		}
		// 元数据统一在 fields JSONB 里。字段可以是标量或标量数组，任一元素相等即命中（契约语义，
		// 与 ES 的 terms 一致）：fields->'x' ?| $n 对字符串数组按元素、对字符串标量按值匹配，
		// fields->>'x' = ANY($n) 兜住数字 / 布尔标量（按文本比）。
		key := quoteLit(f.Field)
		switch {
		case f.Missing:
			sb.WriteString(fmt.Sprintf(" AND (fields->>%s IS NULL)", key))
		case len(f.Values) > 0:
			args = append(args, f.Values)
			hit := fmt.Sprintf("(fields->>%s = ANY($%d) OR fields->%s ?| $%d)", key, len(args), key, len(args))
			if f.Exclude {
				// COALESCE：缺这个字段的行 hit 为 NULL，NOT NULL 会把整行丢掉；反选的语义是保留它们。
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

// quoteLit：把字段名安全地嵌进 SQL 字符串字面量（JSONB 取键不能用参数绑定的位置）。
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

// vecLiteral：pgvector 的输入形态是 '[1,2,3]' 文本，不是数组参数。
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

var _ = time.Now // 保留：日后加超时用

// —— 凭证体检 ——

// opHealthCheck：连一下库，报版本与关键扩展。
//
// 不可用时返回 ok=false + message 而**不是** error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」——后者才是这里要说的话，且 message 里那句 Postgres 原文
// （连不上 / 密码认证失败 / 数据库不存在）正是人排查时唯一有用的东西。
func opHealthCheck(ctx sokel.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	st, err := storeOf(ctx)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	// 这里用 ctx 而非本插件其余地方的 context.Background()：连不上时 pgx 会一路重试，
	// 只有跟着平台的操作超时走，凭证页那个转圈才会有个了结。
	var version string
	if err := st.pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return &HealthCheckOut{Message: fmt.Sprintf("连不上 Postgres: %v", err)}, nil
	}
	out := &HealthCheckOut{OK: true, Version: shortPGVersion(version)}
	// 扩展是**额外情报**：读 pg_extension 失败（权限收紧的库）不推翻「连得上」这个结论。
	// 但缺 vector 必须说出来——它是本插件跑起来的前提，不体检的话要等到建知识库那一刻才炸。
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

// shortPGVersion：version() 回的是一整行编译信息（"PostgreSQL 16.2 (Debian …) on x86_64 …"），
// 界面上只要前两段——后面那串编译器/平台细节挤掉的正是人要看的东西。
func shortPGVersion(v string) string {
	f := strings.Fields(v)
	if len(f) >= 2 {
		return f[0] + " " + f[1]
	}
	return strings.TrimSpace(v)
}
