package main

// 对着**真** pgvector 跑一遍 8 个操作。
//
// 存储插件的单测如果不连真库，测的就只是自己写的 SQL 字符串长什么样——
// 而这类插件出错的地方恰恰全在数据库那一侧（维度、算子类、NULL、JSONB、参数位次）。
// 没有 PGVECTOR_TEST_URL 就跳过，CI 与他人机器上不会因此变红。
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

// fakeCtx：只提供凭证的最小 sokel.Ctx 替身（本插件的操作实现只用到 Credential()；
// 文件层那几个方法在存储插件里用不上，直接给零值/报错即可）。
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

// vec：造一个 dims 维的单位向量，第 i 位为 1（互相正交，相似度可预期）。
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

// 全链：建库 → 写入 → 向量检索 → 关键词检索 → mget → 浏览 → 删文档 → 删库。
func TestPgvectorRoundTrip(t *testing.T) {
	ctx := liveCtx(t)
	const kb = "rt_case"
	const dims = 8

	// 收尾先挂上：中途失败也不留表。
	t.Cleanup(func() { _, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) })
	_, _ = opKBDrop(ctx, &KbDropIn{KbID: kb}) // 上一轮的残留

	if _, err := opKBCreate(ctx, &KbCreateIn{KbID: kb, Dims: dims}); err != nil {
		t.Fatalf("建库失败: %v", err)
	}
	// 幂等：契约里 kb_create 会被重复调用（平台侧不保证只建一次）。
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

	// —— 向量检索：查 c1 的向量，c1 必须排第一，且分数接近 1 ——
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
	// 没有向量的父块不该出现在向量检索里（p1 没带 embedding）。
	for _, h := range vq.Hits {
		if h.ID == "p1" {
			t.Error("无向量的块不该被向量检索召回")
		}
	}
	// 读路径不回流向量（几 KB 浮点穿 NATS）。
	if len(vq.Hits[0].Chunk.Embedding) != 0 {
		t.Error("命中不该带回 embedding")
	}

	// —— 关键词检索 ——
	kq, err := opKeywordQuery(ctx, &KeywordQueryIn{KbID: kb, Query: "光伏组件价格", K: 5})
	if err != nil {
		t.Fatalf("关键词检索失败: %v", err)
	}
	if len(kq.Hits) == 0 || kq.Hits[0].ID != "c3" {
		t.Fatalf("关键词应命中 c3, got %+v", ids(kq.Hits))
	}

	// —— 过滤：按元数据 + 反选 ——
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
	// 缺字段过滤：rating 只有 d1 的两条有
	f3, _ := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10,
		Filters: []schema.Filter{{Field: "rating", Missing: true}}})
	for _, h := range f3.Hits {
		if h.ID != "c3" {
			t.Errorf("missing=rating 应只剩 c3, got %s", h.ID)
		}
	}

	// —— 时间范围 ——
	tq, _ := opVectorQuery(ctx, &VectorQueryIn{KbID: kb, Embedding: toF64(vec(dims, 0)), K: 10,
		TimeRange: schema.TimeRange{From: "2026-04-01"}})
	for _, h := range tq.Hits {
		if h.ID == "c1" {
			t.Error("2026-03-01 的 c1 不该落在 from=2026-04-01 的范围里")
		}
	}

	// —— mget：**必须按请求的 id 顺序返回**（平台当真；SQL 的 = ANY 不保证顺序）——
	// 请求序**故意与写入序相反**(c3 后写、p1 先写):顺着写的话 SQL 会按物理序还给你,
	// 断言就会因为"恰好一致"而假绿——第一版就是这么写的,验齿时没红才发现。
	mg, err := opMget(ctx, &MgetIn{KbID: kb, IDs: []string{"c3", "p1", "不存在"}})
	if err != nil {
		t.Fatalf("mget 失败: %v", err)
	}
	if len(mg.Chunks) != 2 || mg.Chunks[0].ID != "c3" || mg.Chunks[1].ID != "p1" {
		t.Fatalf("mget 应按请求序返回且跳过不存在的, got %+v", chunkIDs(mg.Chunks))
	}
	// 透传字段要原样回来（fields/溯源/资产是 payload，不索引但必须完整）。
	if mg.Chunks[1].Fields["industry"] != "半导体" {
		t.Errorf("元数据没原样回来: %+v", mg.Chunks[1].Fields)
	}

	// —— 浏览 ——
	br, err := opBrowse(ctx, &ChunksBrowseIn{KbID: kb, K: 10})
	if err != nil {
		t.Fatalf("浏览失败: %v", err)
	}
	if len(br.Chunks) != 4 {
		t.Errorf("应浏览到 4 个块, got %d", len(br.Chunks))
	}

	// —— 覆盖写：同一 doc 再写一次(append=false)应替换而不是叠加 ——
	if _, err := opUpsert(ctx, &ChunksUpsertIn{KbID: kb, DocID: "d1",
		Chunks: []schema.Chunk{{ID: "c1", DocID: "d1", Content: "改过了", Embedding: vec(dims, 0)}}}); err != nil {
		t.Fatalf("覆盖写失败: %v", err)
	}
	br2, _ := opBrowse(ctx, &ChunksBrowseIn{KbID: kb, K: 10})
	if len(br2.Chunks) != 2 { // d1 只剩 c1，d2 的 c3 还在
		t.Errorf("覆盖写后应剩 2 个块, got %d (%v)", len(br2.Chunks), chunkIDs(br2.Chunks))
	}

	// —— 删文档 / 删库 ——
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

// 分批写入：平台按 NATS 帧上限切批，只有第一批 append=false。
// 每批都当覆盖的话，后一批会把前一批刚写的删掉——ES 版踩过，这里钉住。
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

// 维度不符必须**报错**，而不是写坏。
// pgvector 的 vector(N) 会自己拦，这里确认错误确实冒到调用方（而不是被吞成 ok:true）。
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

// 表名只能由 [a-z0-9_] 组成：kb_id 是外部给的，拼进 SQL 标识符的地方只有这一处。
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

// 数组字段与反选的语义（主仓开源计划 M3-3.4）：字段可以是标量或标量数组，任一元素相等即命中——平台把
// 成组的业务标签打平成 tags: ["kind:id"]、权限打平成 readers: ["u_1"] 来过滤；反选时缺这个字段的行要保留
// （ES 的 must_not 就是这样），此前 NOT (NULL = ANY) 会把它们整行丢掉。
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
