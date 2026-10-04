package main

// Runs all 8 operations against a **real** pgvector.
//
// If a storage plugin's unit tests don't connect to a real database, all they test is what the
// SQL string you wrote looks like to yourself — and this class of plugin fails precisely on the
// database side (dimensions, operator classes, NULL, JSONB, parameter positions).
// Skipped when PGVECTOR_TEST_URL isn't set, so CI and other people's machines don't go red over it.
//
//	docker run -d --name sokel-pgvector -e POSTGRES_USER=sokel -e POSTGRES_PASSWORD=sokel \
//	  -e POSTGRES_DB=kbstore -p 5434:5432 pgvector/pgvector:pg16
//	PGVECTOR_TEST_URL=postgres://sokel:sokel@localhost:5434/kbstore?sslmode=disable go test ./...

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/kbstore-pgvector/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// fakeCtx is a minimal sokel.Ctx stand-in that only provides the credential (this plugin's
// operations only use Credential(); the file-layer methods aren't used in a storage plugin, so
// they can just return a zero value / error).
type fakeCtx struct {
	context.Context
	cred map[string]string
}

func (f fakeCtx) Credential() map[string]string { return f.cred }
func (f fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("测试替身不支持文件层")
}
func (f fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("测试替身不支持文件层")
}
func (f fakeCtx) Fetch(*plugin.File) ([]byte, error) {
	return nil, errors.New("测试替身不支持文件层")
}

func liveCtx(t *testing.T) fakeCtx {
	t.Helper()
	dsn := os.Getenv("PGVECTOR_TEST_URL")
	if dsn == "" {
		t.Skip("未设 PGVECTOR_TEST_URL，跳过真库用例")
	}
	return fakeCtx{Context: context.Background(), cred: map[string]string{"pg_url": dsn, "namespace": "kbt"}}
}

// vec builds a dims-dimensional unit vector with a 1 in position i (mutually orthogonal, so
// similarity is predictable).
func vec(dims, i int) []float32 {
	v := make([]float32, dims)
	v[i%dims] = 1
	return v
}

func toF64(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, f := range v {
		out[i] = float64(f)
	}
	return out
}

// Full chain: create kb → write → vector query → keyword query → mget → browse → delete doc →
// delete kb.
func TestPgvectorRoundTrip(t *testing.T) {
	ctx := liveCtx(t)
	const kb = "rt_case"
	const dims = 8

	// Cleanup is registered up front: no leftover table even if a mid-test step fails.
	t.Cleanup(func() { _, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) })
	_, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) // leftover from a previous run

	if _, err := opKBCreate(ctx, &KbCreateIn{KbID: kb, Dims: dims}); err != nil {
		t.Fatalf("建库失败: %v", err)
	}
	// Idempotency: kb_create can be called repeatedly per the contract (the platform doesn't
	// guarantee it's only created once).
	if _, err := opKBCreate(ctx, &KbCreateIn{KbID: kb, Dims: dims}); err != nil {
		t.Fatalf("重复建库应幂等: %v", err)
	}

	chunks := []schema.Chunk{
		{ID: "c1", DocID: "d1", Content: "英伟达数据中心业务营收创新高", Title: "英伟达季报", Role: "child",
			ParentID: "p1", ParentNo: 1, ChildNo: 1, PageNo: 3, Datetime: "2026-03-01T00:00:00Z",
			Fields:    map[string]any{"industry": "半导体", "rating": "买入"},
			Embedding: vec(dims, 0)},
		{ID: "c2", DocID: "d1", Content: "毛利率环比下滑,主因产品结构变化", Title: "英伟达季报", Role: "child",
			ParentID: "p1", ParentNo: 1, ChildNo: 2, PageNo: 4,
			Fields:    map[string]any{"industry": "半导体", "rating": "买入"},
			Embedding: vec(dims, 1)},
		{ID: "p1", DocID: "d1", Content: "英伟达数据中心业务营收创新高。毛利率环比下滑。", Title: "英伟达季报", Role: "parent",
			ParentNo: 1, PageNo: 3, Fields: map[string]any{"industry": "半导体"}},
		{ID: "c3", DocID: "d2", Content: "光伏组件价格触底,行业开工率回升", Title: "光伏周报", Role: "child",
			ParentID: "p2", ParentNo: 1, ChildNo: 1, Datetime: "2026-05-20T00:00:00Z",
			Fields:    map[string]any{"industry": "新能源"},
			Embedding: vec(dims, 2)},
	}
	up, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d1", Chunks: chunks[:3]})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if up.Count != 3 {
		t.Errorf("应写入 3 条, got %d", up.Count)
	}
	if _, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d2", Chunks: chunks[3:]}); err != nil {
		t.Fatalf("写入第二篇失败: %v", err)
	}

	// —— Vector query: query c1's vector, c1 must rank first with a score near 1 ——
	vq, err := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 5})
	if err != nil {
		t.Fatalf("向量检索失败: %v", err)
	}
	if len(vq.Hits) == 0 || vq.Hits[0].ID != "c1" {
		t.Fatalf("最相似的应是 c1, got %+v", ids(vq.Hits))
	}
	if math.Abs(vq.Hits[0].Score-1) > 0.01 {
		t.Errorf("自身相似度应≈1, got %v", vq.Hits[0].Score)
	}
	// A parent chunk with no vector shouldn't show up in vector query results (p1 has no embedding).
	for _, h := range vq.Hits {
		if h.ID == "p1" {
			t.Error("无向量的块不该被向量检索召回")
		}
	}
	// The read path doesn't send the vector back (that would be a few KB of floats over NATS).
	if len(vq.Hits[0].Chunk.Embedding) != 0 {
		t.Error("命中不该带回 embedding")
	}

	// —— Keyword query ——
	kq, err := opKeywordQuery(ctx, &KeywordQueryIn{KbID: kb, Query: "光伏组件价格", K: 5})
	if err != nil {
		t.Fatalf("关键词检索失败: %v", err)
	}
	if len(kq.Hits) == 0 || kq.Hits[0].ID != "c3" {
		t.Fatalf("关键词应命中 c3, got %+v", ids(kq.Hits))
	}

	// —— Filtering: by metadata + exclusion ——
	f1, _ := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10,
		Filters: []schema.Filter{{Field: "industry", Values: []string{"新能源"}}}})
	if len(f1.Hits) != 1 || f1.Hits[0].ID != "c3" {
		t.Errorf("按 industry=新能源 过滤应只剩 c3, got %+v", ids(f1.Hits))
	}
	f2, _ := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10,
		Filters: []schema.Filter{{Field: "industry", Values: []string{"新能源"}, Exclude: true}}})
	for _, h := range f2.Hits {
		if h.ID == "c3" {
			t.Error("反选应把 c3 排除")
		}
	}
	// Missing-field filter: only d1's two chunks have rating
	f3, _ := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10,
		Filters: []schema.Filter{{Field: "rating", Missing: true}}})
	for _, h := range f3.Hits {
		if h.ID != "c3" {
			t.Errorf("missing=rating 应只剩 c3, got %s", h.ID)
		}
	}

	// —— Time range ——
	tq, _ := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10,
		TimeRange: schema.TimeRange{From: "2026-04-01"}})
	for _, h := range tq.Hits {
		if h.ID == "c1" {
			t.Error("2026-03-01 的 c1 不该落在 from=2026-04-01 的范围里")
		}
	}

	// —— mget: **must return in the order the ids were requested** (the platform takes this
	// literally; SQL's = ANY makes no ordering guarantee) ——
	// The request order is **deliberately the reverse of the write order** (c3 written last, p1
	// written first): writing in the same order would let SQL hand results back in physical order,
	// and the assertion would pass for the wrong reason — "coincidentally matching" — which is
	// exactly how the first version was written, and it took a mutation-testing pass that stayed
	// green to notice.
	mg, err := opMget(ctx, &MgetIn{KbID: kb, IDs: []string{"c3", "p1", "不存在"}})
	if err != nil {
		t.Fatalf("mget 失败: %v", err)
	}
	if len(mg.Chunks) != 2 || mg.Chunks[0].ID != "c3" || mg.Chunks[1].ID != "p1" {
		t.Fatalf("mget 应按请求序返回且跳过不存在的, got %+v", chunkIDs(mg.Chunks))
	}
	// Pass-through fields must come back unchanged (fields/provenance/assets are payload — not
	// indexed, but must stay complete).
	if mg.Chunks[1].Fields["industry"] != "半导体" {
		t.Errorf("元数据没原样回来: %+v", mg.Chunks[1].Fields)
	}

	// —— Browse ——
	br, err := opBrowse(ctx, &ChunksBrowseIn{KbID: kb, K: 10})
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}
	if len(br.Chunks) != 4 {
		t.Errorf("应浏览到 4 个块, got %d", len(br.Chunks))
	}

	// —— Overwrite write: writing the same doc again (append=false) should replace, not append ——
	if _, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d1",
		Chunks: []schema.Chunk{{ID: "c1", DocID: "d1", Content: "改过了", Embedding: vec(dims, 0)}}}); err != nil {
		t.Fatalf("覆盖写失败: %v", err)
	}
	br2, _ := opBrowse(ctx, &ChunksBrowseIn{KbID: kb, K: 10})
	if len(br2.Chunks) != 2 { // d1 should be down to just c1; d2's c3 is still there
		t.Errorf("覆盖写后应剩 2 个块, got %d (%v)", len(br2.Chunks), chunkIDs(br2.Chunks))
	}

	// —— Delete document / delete knowledge base ——
	if _, err := opDocDelete(ctx, &DocDeleteIn{KbID: kb, DocID: "d1"}); err != nil {
		t.Fatalf("删文档失败: %v", err)
	}
	br3, _ := opBrowse(ctx, &ChunksBrowseIn{KbID: kb, K: 10})
	if len(br3.Chunks) != 1 || br3.Chunks[0].DocID != "d2" {
		t.Errorf("删完 d1 应只剩 d2 的块, got %v", chunkIDs(br3.Chunks))
	}
	if _, err := opKBDrop(ctx, &KbDropIn{KbID: kb}); err != nil {
		t.Fatalf("删库失败: %v", err)
	}
}

// Batched writes: the platform splits into batches by the NATS frame size limit, and only the
// first batch has append=false. Treating every batch as an overwrite would make the next batch
// delete what the previous one just wrote — the ES version hit this, pinned down here.
func TestPgvectorAppendBatches(t *testing.T) {
	ctx := liveCtx(t)
	const kb = "batch_case"
	const dims = 4
	t.Cleanup(func() { _, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) })
	_, _ = opKBDrop(ctx, &KbDropIn{KbID: kb})
	if _, err := opKBCreate(ctx, &KbCreateIn{KbID: kb, Dims: dims}); err != nil {
		t.Fatal(err)
	}
	first := []schema.Chunk{{ID: "b1", DocID: "d", Content: "第一批", Embedding: vec(dims, 0)}}
	second := []schema.Chunk{{ID: "b2", DocID: "d", Content: "第二批", Embedding: vec(dims, 1)}}
	if _, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d", Chunks: first}); err != nil {
		t.Fatal(err)
	}
	if _, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d", Chunks: second, Append: true}); err != nil {
		t.Fatal(err)
	}
	br, _ := opBrowse(ctx, &ChunksBrowseIn{KbID: kb, K: 10})
	if len(br.Chunks) != 2 {
		t.Errorf("两批都该留下, got %v", chunkIDs(br.Chunks))
	}
}

// A dimension mismatch must **error**, not silently corrupt data.
// pgvector's vector(N) rejects it on its own; this confirms the error actually surfaces to the
// caller (rather than being swallowed into ok:true).
func TestPgvectorDimsMismatchFails(t *testing.T) {
	ctx := liveCtx(t)
	const kb = "dims_case"
	t.Cleanup(func() { _, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) })
	_, _ = opKBDrop(ctx, &KbDropIn{KbID: kb})
	if _, err := opKBCreate(ctx, &KbCreateIn{KbID: kb, Dims: 4}); err != nil {
		t.Fatal(err)
	}
	_, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d",
		Chunks: []schema.Chunk{{ID: "x", Content: "维度不对", Embedding: vec(8, 0)}}})
	if err == nil {
		t.Fatal("8 维向量写进 4 维库必须报错(悄悄写坏是最坏的结果)")
	}
}

// A table name may only consist of [a-z0-9_]: kb_id comes from outside, and this is the only
// place it gets spliced into a SQL identifier.
func TestTableNameSanitized(t *testing.T) {
	s := &store{ns: "kb"}
	got := s.table(`x"; DROP TABLE users; --`)
	for _, r := range got {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			t.Fatalf("表名混进了危险字符: %q", got)
		}
	}
}

func ids(hits []schema.Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func chunkIDs(cs []schema.Chunk) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// Semantics of array fields and exclusion (main-repo open-source plan M3-3.4): a field can be a
// scalar or an array of scalars, and matching any one element counts as a hit — the platform
// flattens grouped business tags into tags: ["kind:id"] and permissions into readers: ["u_1"] to
// filter on them; on exclusion, rows missing the field should be kept (that's how ES's must_not
// behaves), whereas the earlier NOT (NULL = ANY) dropped those rows entirely.
func TestPgvectorArrayAndExcludeFilters(t *testing.T) {
	ctx := liveCtx(t)
	const kb = "arr_case"
	const dims = 4
	t.Cleanup(func() { _, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) })
	_, _ = opKBDrop(ctx, &KbDropIn{KbID: kb})
	if _, err := opKBCreate(ctx, &KbCreateIn{KbID: kb, Dims: dims}); err != nil {
		t.Fatal(err)
	}
	chunks := []schema.Chunk{
		{ID: "a", DocID: "d1", Content: "甲的合同", Role: "child", Fields: map[string]any{"readers": []any{"u_1", "u_2"}, "tags": []any{"kind:contract"}, "year": 2026}, Embedding: vec(dims, 0)},
		{ID: "b", DocID: "d2", Content: "乙的合同", Role: "child", Fields: map[string]any{"readers": []any{"u_2"}, "tags": []any{"kind:contract", "kind:draft"}}, Embedding: vec(dims, 1)},
		{ID: "c", DocID: "d3", Content: "公开的说明", Role: "child", Fields: map[string]any{"owner": "u_3"}, Embedding: vec(dims, 2)},
	}
	for _, c := range chunks {
		if _, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: c.DocID, Chunks: []schema.Chunk{c}}); err != nil {
			t.Fatal(err)
		}
	}
	q := func(fs ...schema.Filter) []string {
		out, err := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10, Filters: fs})
		if err != nil {
			t.Fatal(err)
		}
		got := ids(out.Hits)
		sort.Strings(got)
		return got
	}
	cases := []struct {
		name string
		fs   []schema.Filter
		want []string
	}{
		{"array field: any element matches", []schema.Filter{{Field: "readers", Values: []string{"u_1"}}}, []string{"a"}},
		{"array field: several values", []schema.Filter{{Field: "readers", Values: []string{"u_9", "u_2"}}}, []string{"a", "b"}},
		{"scalar field still matches", []schema.Filter{{Field: "owner", Values: []string{"u_3"}}}, []string{"c"}},
		{"number scalar compares as text", []schema.Filter{{Field: "year", Values: []string{"2026"}}}, []string{"a"}},
		{"exclude on an array field", []schema.Filter{{Field: "tags", Values: []string{"kind:draft"}, Exclude: true}}, []string{"a", "c"}},
		{"exclude keeps rows without the field", []schema.Filter{{Field: "owner", Values: []string{"u_3"}, Exclude: true}}, []string{"a", "b"}},
		{"rows are ANDed", []schema.Filter{{Field: "tags", Values: []string{"kind:contract"}}, {Field: "readers", Values: []string{"u_2"}}, {Field: "tags", Values: []string{"kind:draft"}, Exclude: true}}, []string{"a"}},
	}
	for _, c := range cases {
		if got := q(c.fs...); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
