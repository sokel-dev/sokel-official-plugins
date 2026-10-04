// kbstore-es: a knowledge-base storage engine plugin (Elasticsearch 8).
// v4 contract (aicraft-hub docs/knowledge-base-rag.md §0.5): storage and retrieval only — chunking
// /embedding/fusion/rerank all live on the platform.
// 8 Internal operations: kb_create/kb_drop/chunks_upsert/doc_delete/vector_query/keyword_query/
// chunks_browse/mget.
// The mapping/query construction is ported from rag-prototype v2 (one index + alias per knowledge
// base, flattened fields, the ik fallback chain, distance_feature recency).
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen
// The contract is provided by zz_sokel.go (AST-generated, not runtime reflection). Regenerate after
// changing an input/output struct or its tags.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/kbstore-es/schema"
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
		Name:     "kbstore-es",
	})
	RegisterCredential(p) // credential contract (generated from the schema declaration; Cred lives in zz_credential.go)
	// Self-reported capabilities: ES supports all four here (BM25 with ik tokenization,
	// distance_feature recency, range time filtering, multi_match field boosting). Reporting this
	// honestly is what makes comparing storage engines possible.
	p.SetCapabilities(map[string]bool{
		sokel.CapKeywordBM25: true, sokel.CapRecency: true, sokel.CapTimeRange: true, sokel.CapFieldBoosts: true,
		sokel.CapArrayFilters: true, // term queries match any element of an array field
	})
	p.SetDoc(usageDoc, "") // usage doc (docs/*.md): reported to the platform with the handshake
	reg := func(id, label string, h any) {}
	_ = reg
	OnHealthCheck(p, opHealthCheck)
	OnKbCreate(p, opKBCreate)
	OnKbDrop(p, opKBDrop)
	OnChunksUpsert(p, opUpsert)
	OnDocDelete(p, opDocDelete)
	OnVectorQuery(p, opVectorQuery)
	OnKeywordQuery(p, opKeywordQuery)
	OnChunksBrowse(p, opBrowse)
	OnMget(p, opMget)
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

// —— ES REST client ——

type es struct {
	base string
	key  string
	ns   string
	hc   *http.Client
}

func esOf(ctx sokel.Ctx) (*es, error) {
	c := ctx.Credential()
	if c == nil || c["es_url"] == "" {
		return nil, fmt.Errorf("缺少 ES 凭证(es_url)")
	}
	ns := c["namespace"]
	if ns == "" {
		ns = "sokel"
	}
	return &es{base: strings.TrimRight(c["es_url"], "/"), key: c["api_key"], ns: ns, hc: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (e *es) do(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "ApiKey "+e.key)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ES 不可达: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("ES %s %s: HTTP %d %.300s", method, path, resp.StatusCode, raw)
	}
	return out, nil
}

func (e *es) bulk(ctx context.Context, lines []any) (map[string]any, error) {
	var buf bytes.Buffer
	for _, l := range lines {
		b, _ := json.Marshal(l)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", e.base+"/_bulk?refresh=wait_for", &buf)
	req.Header.Set("Content-Type", "application/x-ndjson")
	if e.key != "" {
		req.Header.Set("Authorization", "ApiKey "+e.key)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 400 || out["errors"] == true {
		return out, fmt.Errorf("bulk 失败: %s", firstBulkError(out, raw))
	}
	return out, nil
}

// firstBulkError extracts the id+reason of the first failed item from a bulk response (the whole
// response can run to hundreds of thousands of characters, and truncating it blindly would hide
// even the reason for the failure).
func firstBulkError(out map[string]any, raw []byte) string {
	items, _ := out["items"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		for _, op := range m {
			om, _ := op.(map[string]any)
			if om == nil || om["error"] == nil {
				continue
			}
			eb, _ := json.Marshal(om["error"])
			return fmt.Sprintf("%v: %.400s", om["_id"], eb)
		}
	}
	return fmt.Sprintf("%.300s", raw)
}

func (e *es) phys(kb string) string  { return e.ns + ".kb." + kb + ".v1" }
func (e *es) alias(kb string) string { return e.ns + ".kb." + kb }

// —— Filter construction (contract Filter → ES bool) ——

var coreFields = map[string]bool{"doc_id": true, "role": true, "parent_no": true, "child_no": true, "parent_id": true}

func fieldPath(f string) string {
	if coreFields[f] {
		return f
	}
	return "fields." + f
}

func buildBool(filters []schema.Filter, timeRange schema.TimeRange) map[string]any {
	var must, mustNot []any
	for _, f := range filters {
		field := f.Field
		if field == "" {
			continue
		}
		if f.Missing {
			mustNot = append(mustNot, map[string]any{"exists": map[string]any{"field": fieldPath(field)}})
			continue
		}
		if len(f.Values) == 0 {
			continue
		}
		strs := make([]any, len(f.Values))
		for i, v := range f.Values {
			strs[i] = v
		}
		clause := map[string]any{"terms": map[string]any{fieldPath(field): strs}}
		if f.Exclude {
			mustNot = append(mustNot, clause)
		} else {
			must = append(must, clause)
		}
	}
	{
		rng := map[string]any{}
		if timeRange.From != "" {
			rng["gte"] = timeRange.From
		}
		if timeRange.To != "" {
			rng["lte"] = timeRange.To
		}
		if len(rng) > 0 {
			must = append(must, map[string]any{"range": map[string]any{"datetime": rng}})
		}
	}
	b := map[string]any{}
	if len(must) > 0 {
		b["filter"] = must
	}
	if len(mustNot) > 0 {
		b["must_not"] = mustNot
	}
	return map[string]any{"bool": b}
}

// —— Operation implementations ——

var analyzerChain = [][2]string{{"ik_max_word", "ik_smart"}, {"smartcn", "smartcn"}, {"standard", "standard"}}

func opKBCreate(ctx sokel.Ctx, in *KbCreateIn) (*KbCreateOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	idx := e.phys(in.KbID)
	if out, _ := e.do(ctx, "HEAD", "/"+idx, nil); out != nil {
	}
	// HEAD has no body: use exists to check instead
	if _, err := e.do(ctx, "GET", "/"+idx, nil); err == nil {
		return &KbCreateOut{OK: true}, nil
	}
	fieldProps := map[string]any{}
	for _, f := range in.Fields {
		name := f.Field
		typ := f.Type
		if name == "" {
			continue
		}
		est := "keyword"
		switch typ {
		case "date":
			est = "date"
		case "number":
			est = "double"
		}
		fieldProps[name] = map[string]any{"type": est}
	}
	var lastErr error
	for _, an := range analyzerChain {
		text := map[string]any{"type": "text", "analyzer": an[0], "search_analyzer": an[1]}
		body := map[string]any{
			"settings": map[string]any{"number_of_shards": 1, "number_of_replicas": 0},
			"mappings": map[string]any{
				"_meta": map[string]any{"kbstore": "es", "analyzer": an[0]},
				// Undeclared metadata fields dynamically map to keyword — this guarantees that term
				// filtering works for "fields declared after the fact" (ES's default guess for a
				// dynamic string is text, which breaks terms filtering).
				"dynamic_templates": []any{map[string]any{
					"meta_fields_as_keyword": map[string]any{
						"path_match": "fields.*", "match_mapping_type": "string",
						"mapping": map[string]any{"type": "keyword"},
					},
				}},
				"properties": map[string]any{
					"content": text, "title": text, "summary": text,
					"datetime":      map[string]any{"type": "date", "format": "date_optional_time||yyyy-MM-dd HH:mm:ss||yyyy-MM-dd||epoch_millis"},
					"doc_id":        map[string]any{"type": "keyword"},
					"role":          map[string]any{"type": "keyword"},
					"parent_id":     map[string]any{"type": "keyword"},
					"parent_no":     map[string]any{"type": "integer"},
					"child_no":      map[string]any{"type": "integer"},
					"page_no":       map[string]any{"type": "integer"},
					"fields":        map[string]any{"properties": fieldProps, "dynamic": true},
					"images":        map[string]any{"type": "object", "enabled": false},             // legacy field (kept for read compatibility)
					"assets":        map[string]any{"type": "object", "enabled": false},             // block-level assets ({kind,url,…}), stored but not indexed
					"source_blocks": map[string]any{"type": "object", "enabled": false},             // provenance refs (block id/type/bbox), stored but not indexed
					"content_html":  map[string]any{"type": "text", "index": false, "norms": false}, // raw table HTML (for display), stored but not indexed
					"boundary":      map[string]any{"type": "keyword"},                              // cross-boundary child chunk direction (prev/next)
					"embedding":     map[string]any{"type": "dense_vector", "dims": in.Dims, "index": true, "similarity": "cosine"},
				},
			},
			"aliases": map[string]any{e.alias(in.KbID): map[string]any{}},
		}
		if _, err := e.do(ctx, "PUT", "/"+idx, body); err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "analyzer") || strings.Contains(err.Error(), an[0]) {
				continue
			}
			return nil, err
		}
		if an[0] == "standard" {
			log.Printf("kb_create(%s): IK/smartcn 不可用退 standard——中文 BM25 质量受损", in.KbID)
		}
		return &KbCreateOut{OK: true}, nil
	}
	return nil, fmt.Errorf("建索引失败: %v", lastErr)
}

func opKBDrop(ctx sokel.Ctx, in *KbDropIn) (*KbDropOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := e.do(ctx, "DELETE", "/"+e.phys(in.KbID), nil); err != nil && !strings.Contains(err.Error(), "404") {
		return nil, err
	}
	return &KbDropOut{OK: true}, nil
}

func opUpsert(ctx sokel.Ctx, in *ChunksUpsertIn) (*ChunksUpsertOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	// Whole-document replacement: clear the doc's existing chunks first.
	// Skipped when append — that's a later batch of the same document, and deleting would wipe out
	// what the earlier batch just wrote.
	if !in.Append {
		_, _ = e.do(ctx, "POST", "/"+e.alias(in.KbID)+"/_delete_by_query?conflicts=proceed&refresh=false",
			map[string]any{"query": map[string]any{"term": map[string]any{"doc_id": in.DocID}}})
	}
	var lines []any
	for _, c := range in.Chunks {
		id := c.ID
		doc := chunkDoc(c)
		lines = append(lines, map[string]any{"index": map[string]any{"_index": e.alias(in.KbID), "_id": id}}, doc)
	}
	if len(lines) == 0 {
		return &ChunksUpsertOut{OK: true}, nil
	}
	if _, err := e.bulk(ctx, lines); err != nil {
		return nil, err
	}
	return &ChunksUpsertOut{OK: true, Count: len(in.Chunks)}, nil
}

func opDocDelete(ctx sokel.Ctx, in *DocDeleteIn) (*DocDeleteOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := e.do(ctx, "POST", "/"+e.alias(in.KbID)+"/_delete_by_query?conflicts=proceed&refresh=true",
		map[string]any{"query": map[string]any{"term": map[string]any{"doc_id": in.DocID}}}); err != nil {
		return nil, err
	}
	return &DocDeleteOut{OK: true}, nil
}

var srcFields = []string{"content", "title", "summary", "datetime", "doc_id", "role", "parent_id", "parent_no", "child_no", "page_no", "fields", "images", "assets", "source_blocks", "content_html", "boundary"}

func hitsOf(out map[string]any) []schema.Hit {
	var hits []schema.Hit
	ho, _ := out["hits"].(map[string]any)
	arr, _ := ho["hits"].([]any)
	for _, h := range arr {
		hm, _ := h.(map[string]any)
		if hm == nil {
			continue
		}
		hits = append(hits, schema.Hit{ID: asStr(hm["_id"]), Score: asFloat(hm["_score"]), Chunk: chunkOf(withID(hm))})
	}
	return hits
}

func withID(hm map[string]any) map[string]any {
	src, _ := hm["_source"].(map[string]any)
	if src == nil {
		src = map[string]any{}
	}
	src["id"] = hm["_id"]
	return src
}

func opVectorQuery(ctx sokel.Ctx, in *VectorQueryIn) (*VectorQueryOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	knn := map[string]any{"field": "embedding", "query_vector": in.Embedding, "k": in.K, "num_candidates": in.K * 5, "filter": buildBool(in.Filters, in.TimeRange)}
	out, err := e.do(ctx, "POST", "/"+e.alias(in.KbID)+"/_search",
		map[string]any{"knn": knn, "size": in.K, "_source": srcFields})
	if err != nil {
		return nil, err
	}
	return &VectorQueryOut{Hits: hitsOf(out)}, nil
}

func opKeywordQuery(ctx sokel.Ctx, in *KeywordQueryIn) (*KeywordQueryOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	boost := func(f string, d float64) float64 {
		if v, ok := in.FieldBoosts[f]; ok && v > 0 {
			return v
		}
		return d
	}
	fields := []string{
		fmt.Sprintf("title^%g", boost("title", 3)),
		fmt.Sprintf("summary^%g", boost("summary", 2)),
		fmt.Sprintf("content^%g", boost("content", 1)),
	}
	b := buildBool(in.Filters, in.TimeRange)["bool"].(map[string]any)
	b["must"] = []any{map[string]any{"multi_match": map[string]any{"query": in.Query, "fields": fields, "type": "best_fields"}}}
	if in.Recency.Pivot != "" {
		pivot, bst := in.Recency.Pivot, in.Recency.Boost
		if pivot == "" {
			pivot = "30d"
		}
		if bst <= 0 {
			bst = 2.0
		}
		b["should"] = []any{map[string]any{"distance_feature": map[string]any{"field": "datetime", "origin": "now", "pivot": pivot, "boost": bst}}}
	}
	out, err := e.do(ctx, "POST", "/"+e.alias(in.KbID)+"/_search",
		map[string]any{"query": map[string]any{"bool": b}, "size": in.K, "_source": srcFields})
	if err != nil {
		return nil, err
	}
	return &KeywordQueryOut{Hits: hitsOf(out)}, nil
}

func opBrowse(ctx sokel.Ctx, in *ChunksBrowseIn) (*ChunksBrowseOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"query": buildBool(in.Filters, in.TimeRange),
		"sort": []any{
			map[string]any{"datetime": map[string]any{"order": "desc", "missing": "_last"}},
			map[string]any{"parent_no": map[string]any{"order": "asc"}},
			map[string]any{"child_no": map[string]any{"order": "asc", "missing": "_first"}},
		},
		"size": in.K, "from": in.Offset, "_source": srcFields,
	}
	out, err := e.do(ctx, "POST", "/"+e.alias(in.KbID)+"/_search", body)
	if err != nil {
		return nil, err
	}
	var chunks []schema.Chunk
	for _, h := range hitsOf(out) {
		chunks = append(chunks, h.Chunk)
	}
	return &ChunksBrowseOut{Chunks: chunks}, nil
}

func opMget(ctx sokel.Ctx, in *MgetIn) (*MgetOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return nil, err
	}
	// _mget's _source filtering can only go through the query parameter (a top-level _source in the
	// request body triggers a parsing_exception)
	out, err := e.do(ctx, "POST", "/"+e.alias(in.KbID)+"/_mget?_source="+strings.Join(srcFields, ","),
		map[string]any{"ids": in.IDs})
	if err != nil {
		return nil, err
	}
	var chunks []schema.Chunk
	for _, d := range func() []any { a, _ := out["docs"].([]any); return a }() {
		dm, _ := d.(map[string]any)
		if dm == nil || dm["found"] != true {
			continue
		}
		chunks = append(chunks, chunkOf(withID(dm)))
	}
	return &MgetOut{Chunks: chunks}, nil
}

// —— Bridging between structs and ES documents ——
// ES's read/write surface is naturally a map (the shape of _source is whatever the index decides),
// while the contract side uses concrete types. Centralizing the conversion here means business code
// doesn't have to scatter assertions like c["id"].(string) everywhere.

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

// chunkDoc: Chunk → ES document (drops id, since it's the _id and doesn't go into _source).
func chunkDoc(c schema.Chunk) map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	delete(m, "id")
	return m
}

// chunkOf: ES document → Chunk. Undeclared fields land in Fields, interpreted per the knowledge
// base's configuration.
func chunkOf(m map[string]any) schema.Chunk {
	b, _ := json.Marshal(m)
	var c schema.Chunk
	_ = json.Unmarshal(b, &c)
	return c
}

// opHealthCheck checks the credential — connects to the cluster and reports version/cluster
// name/health color.
//
// When unavailable, returns ok=false + message, **not** an error: the platform treats an error as
// "this plugin can't run its health check", and ok=false as "the check concluded the plugin is
// unavailable" — the latter is what should be said here, and the raw upstream message (connection
// refused / 401 / certificate error) is the one genuinely useful thing for a human troubleshooting
// this.
func opHealthCheck(ctx sokel.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	e, err := esOf(ctx)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	root, err := e.do(ctx, http.MethodGet, "/", nil)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	out := &HealthCheckOut{OK: true}
	if v, ok := root["version"].(map[string]any); ok {
		out.Version, _ = v["number"].(string)
	}
	out.ClusterName, _ = root["cluster_name"].(string)
	// Cluster health is **extra intel**: failing to fetch it doesn't overturn the "it connects"
	// conclusion (a cluster with tightened permissions might not expose this endpoint).
	if h, herr := e.do(ctx, http.MethodGet, "/_cluster/health", nil); herr == nil {
		out.Status, _ = h["status"].(string)
	}
	st := out.Status
	if st == "" {
		st = "未知"
	}
	out.Message = fmt.Sprintf("连接正常（Elasticsearch %s，集群 %s，状态 %s）", out.Version, out.ClusterName, st)
	return out, nil
}
