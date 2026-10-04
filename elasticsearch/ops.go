package main

// 全部操作实现。
//
// 两条贯穿始终的约定：
//   - **「不存在」不是错误**：取文档没找到、删文档本来就没有——回出参（found/deleted），
//     那在画布上是正常分支。真错误留给连不上、查询写错、权限不够。
//   - **危险操作有闸**：按查询删除必须给查询，删索引不接受通配。ES 这两个接口
//     字面上都支持「全都删」，而工作流里手滑一次就没了。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/elasticsearch/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// —— 检索 ——

func opSearch(ctx plugin.Ctx, in *SearchIn) (*SearchOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	switch {
	case len(in.Query) > 0:
		body["query"] = in.Query
	case strings.TrimSpace(in.Q) != "":
		body["query"] = map[string]any{"query_string": map[string]any{"query": in.Q}}
	}
	if len(in.Aggs) > 0 {
		body["aggs"] = in.Aggs
	}
	// size 留空时：给了聚合就只回聚合（size=0，聚合场景的常规做法），否则 ES 默认的 10。
	// 这条互动规则是刻意的——数字字段留空到手就是 0，与「显式要 0 条」分不开，
	// 用「有没有给聚合」来断意图比猜一个哨兵值可靠。
	switch {
	case in.Size > 0:
		body["size"] = in.Size
	case len(in.Aggs) > 0:
		body["size"] = 0
	}
	if in.From > 0 {
		body["from"] = in.From
	}
	if len(in.SourceFields) > 0 {
		body["_source"] = in.SourceFields
	}
	if len(in.Sort) > 0 {
		var sorts []any
		for _, s := range in.Sort {
			f, ord, ok := strings.Cut(strings.TrimSpace(s), ":")
			if !ok {
				sorts = append(sorts, s)
				continue
			}
			sorts = append(sorts, map[string]any{f: map[string]any{"order": ord}})
		}
		body["sort"] = sorts
	}
	if in.ExactTotal {
		body["track_total_hits"] = true
	}
	res, err := esJSON(ctx, http.MethodPost, "/"+idx+"/_search", body)
	if err != nil {
		return nil, err
	}
	out := &SearchOut{TookMs: num(res, "took")}
	hits := mapAt(res, "hits")
	if total := mapAt(hits, "total"); total != nil {
		out.Total = num(total, "value")
		// relation=gte 意思是「至少这么多」——ES 默认只精确统计到 10000 条。
		out.TotalIsLowerBound = str(total, "relation") == "gte"
	}
	for _, h := range listAt(hits, "hits") {
		m, _ := h.(map[string]any)
		if m == nil {
			continue
		}
		src, _ := m["_source"].(map[string]any)
		out.Hits = append(out.Hits, schema.Hit{
			ID: str(m, "_id"), Index: str(m, "_index"), Score: flt(m, "_score"), Source: src,
		})
	}
	out.Count = len(out.Hits)
	if aggs := mapAt(res, "aggregations"); aggs != nil {
		out.Aggregations = aggs
	}
	return out, nil
}

func opCount(ctx plugin.Ctx, in *CountIn) (*CountOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	switch {
	case len(in.Query) > 0:
		body["query"] = in.Query
	case strings.TrimSpace(in.Q) != "":
		body["query"] = map[string]any{"query_string": map[string]any{"query": in.Q}}
	}
	res, err := esJSON(ctx, http.MethodPost, "/"+idx+"/_count", body)
	if err != nil {
		return nil, err
	}
	return &CountOut{Count: num(res, "count")}, nil
}

// —— 文档读写 ——

func opDocGet(ctx plugin.Ctx, in *DocGetIn) (*DocGetOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, fmt.Errorf("没给文档 ID")
	}
	raw, code, err := esCall(ctx, http.MethodGet, "/"+idx+"/_doc/"+url.PathEscape(in.ID), nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		// 文档不存在 **和** 索引不存在都会回 404。索引不存在是配置错，要说清楚；
		// 文档不存在是正常分支。ES 的应答体能区分：前者带 error.type。
		var probe map[string]any
		if json.Unmarshal(raw, &probe) == nil {
			if _, isErr := probe["error"]; isErr {
				return nil, esError(code, raw)
			}
		}
		return &DocGetOut{}, nil
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	var res map[string]any
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("应答不是 JSON: %s", clip(string(raw), 200))
	}
	src, _ := res["_source"].(map[string]any)
	return &DocGetOut{Found: boolAt(res, "found"), Source: src, Version: num(res, "_version")}, nil
}

func opDocIndex(ctx plugin.Ctx, in *DocIndexIn) (*DocIndexOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if len(in.Document) == 0 {
		return nil, fmt.Errorf("没给文档内容")
	}
	path := "/" + idx + "/_doc"
	method := http.MethodPost
	if id := strings.TrimSpace(in.ID); id != "" {
		path += "/" + url.PathEscape(id)
		method = http.MethodPut
	}
	if in.Refresh {
		path += "?refresh=true"
	}
	res, err := esJSON(ctx, method, path, in.Document)
	if err != nil {
		return nil, err
	}
	return &DocIndexOut{ID: str(res, "_id"), Result: str(res, "result"), Version: num(res, "_version")}, nil
}

func opDocUpdate(ctx plugin.Ctx, in *DocUpdateIn) (*DocUpdateOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, fmt.Errorf("没给文档 ID")
	}
	if len(in.Doc) == 0 {
		return nil, fmt.Errorf("没给要改的字段")
	}
	body := map[string]any{"doc": in.Doc}
	if in.Upsert {
		body["doc_as_upsert"] = true
	}
	path := "/" + idx + "/_update/" + url.PathEscape(in.ID)
	if in.Refresh {
		path += "?refresh=true"
	}
	res, err := esJSON(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return &DocUpdateOut{Result: str(res, "result"), Version: num(res, "_version")}, nil
}

func opDocDelete(ctx plugin.Ctx, in *DocDeleteIn) (*DocDeleteOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, fmt.Errorf("没给文档 ID")
	}
	path := "/" + idx + "/_doc/" + url.PathEscape(in.ID)
	if in.Refresh {
		path += "?refresh=true"
	}
	raw, code, err := esCall(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return &DocDeleteOut{}, nil // 本来就没有：不是错误
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	return &DocDeleteOut{Deleted: true}, nil
}

func opBulkIndex(ctx plugin.Ctx, in *BulkIndexIn) (*BulkIndexOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if len(in.Documents) == 0 {
		return nil, fmt.Errorf("没给文档——文档数组是空的")
	}
	// _bulk 是 NDJSON：一行动作、一行文档，最后必须有换行。
	// 它不是 JSON 数组——按 JSON 发过去 ES 会回 400 说 "The bulk request must be terminated"。
	var buf strings.Builder
	idField := strings.TrimSpace(in.IDField)
	for i, d := range in.Documents {
		doc, ok := d.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("第 %d 个元素不是对象（文档数组里每个元素都要是一篇文档的 JSON）", i+1)
		}
		action := map[string]any{"index": map[string]any{"_index": idx}}
		if idField != "" {
			v, has := doc[idField]
			if !has {
				return nil, fmt.Errorf("第 %d 篇文档里没有「%s」字段，取不到 ID", i+1, idField)
			}
			action["index"].(map[string]any)["_id"] = fmt.Sprintf("%v", v)
		}
		a, _ := json.Marshal(action)
		b, err := json.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("第 %d 篇文档序列化失败: %w", i+1, err)
		}
		buf.Write(a)
		buf.WriteByte('\n')
		buf.Write(b)
		buf.WriteByte('\n')
	}
	path := "/_bulk"
	if in.Refresh {
		path += "?refresh=true"
	}
	res, err := esNDJSON(ctx, path, buf.String())
	if err != nil {
		return nil, err
	}
	out := &BulkIndexOut{TookMs: num(res, "took")}
	for _, it := range listAt(res, "items") {
		m, _ := it.(map[string]any)
		result := mapAt(m, "index")
		if result == nil {
			continue
		}
		if e, has := result["error"]; has {
			out.Failed++
			if len(out.Errors) < 10 {
				em, _ := e.(map[string]any)
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %s", str(em, "type"), str(em, "reason")))
			}
			continue
		}
		out.Indexed++
	}
	return out, nil
}

func opDeleteByQuery(ctx plugin.Ctx, in *DeleteByQueryIn) (*DeleteByQueryOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if len(in.Query) == 0 {
		// ES 侧「不给查询」= match_all = 删光整个索引。这一步必须是显式的。
		return nil, fmt.Errorf("按查询删除必须给查询条件——不给等于删光索引；" +
			"真要清空请用「删索引」再重建（那样还能顺便换 mapping）")
	}
	res, err := esJSON(ctx, http.MethodPost, "/"+idx+"/_delete_by_query?conflicts=proceed",
		map[string]any{"query": in.Query})
	if err != nil {
		return nil, err
	}
	return &DeleteByQueryOut{Deleted: num(res, "deleted"), TookMs: num(res, "took")}, nil
}

// —— 索引管理 ——

func opIndicesList(ctx plugin.Ctx, in *IndicesListIn) (*IndicesListOut, error) {
	path := "/_cat/indices"
	if p := strings.TrimSpace(in.Pattern); p != "" {
		if strings.Contains(p, "/") {
			return nil, fmt.Errorf("匹配模式里不能有斜杠")
		}
		path += "/" + p
	}
	path += "?format=json&h=index,health,status,docs.count,store.size&s=index"
	raw, code, err := esCall(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("_cat 应答解析失败: %s", clip(string(raw), 200))
	}
	out := &IndicesListOut{}
	for _, r := range rows {
		name := str(r, "index")
		if strings.HasPrefix(name, ".") {
			continue // 系统索引（.kibana 之类）不摆到画布上
		}
		out.Indices = append(out.Indices, schema.IndexInfo{
			Name: name, Health: str(r, "health"), Status: str(r, "status"),
			Docs: atoiSafe(str(r, "docs.count")), StoreSize: str(r, "store.size"),
		})
	}
	out.Count = len(out.Indices)
	return out, nil
}

func opIndexCreate(ctx plugin.Ctx, in *IndexCreateIn) (*IndexCreateOut, error) {
	name, err := indexPath(in.Name)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if len(in.Mappings) > 0 {
		body["mappings"] = in.Mappings
	}
	if len(in.Settings) > 0 {
		body["settings"] = in.Settings
	}
	var payload any
	if len(body) > 0 {
		payload = body
	}
	raw, code, err := esCall(ctx, http.MethodPut, "/"+name, payload)
	if err != nil {
		return nil, err
	}
	if code == http.StatusBadRequest && strings.Contains(string(raw), "resource_already_exists") {
		return &IndexCreateOut{Existed: true}, nil // 已存在：幂等地告诉调用方，不是错误
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	return &IndexCreateOut{Created: true}, nil
}

func opIndexDelete(ctx plugin.Ctx, in *IndexDeleteIn) (*IndexDeleteOut, error) {
	name, err := indexPath(in.Name)
	if err != nil {
		return nil, err
	}
	// DELETE /* 在 ES 里是合法请求，意思是删光集群。工作流里手滑一次就没了。
	if strings.ContainsAny(name, "*?,") || name == "_all" {
		return nil, fmt.Errorf("删索引只接受一个具体的索引名，不接受通配或 _all（那会删掉一大片）；"+
			"要删多个请分别调用。你给的是 %q", name)
	}
	raw, code, err := esCall(ctx, http.MethodDelete, "/"+name, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return &IndexDeleteOut{}, nil
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	return &IndexDeleteOut{Deleted: true}, nil
}

func opMappingGet(ctx plugin.Ctx, in *MappingGetIn) (*MappingGetOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	res, err := esJSON(ctx, http.MethodGet, "/"+idx+"/_mapping", nil)
	if err != nil {
		return nil, err
	}
	return &MappingGetOut{Mappings: res}, nil
}

func opMappingPut(ctx plugin.Ctx, in *MappingPutIn) (*MappingPutOut, error) {
	idx, err := indexPath(in.Index)
	if err != nil {
		return nil, err
	}
	if len(in.Properties) == 0 {
		return nil, fmt.Errorf("没给字段定义")
	}
	if _, err := esJSON(ctx, http.MethodPut, "/"+idx+"/_mapping",
		map[string]any{"properties": in.Properties}); err != nil {
		return nil, err
	}
	return &MappingPutOut{OK: true}, nil
}

func opAliasSwitch(ctx plugin.Ctx, in *AliasSwitchIn) (*AliasSwitchOut, error) {
	alias := strings.TrimSpace(in.Alias)
	target, err := indexPath(in.ToIndex)
	if err != nil {
		return nil, err
	}
	if alias == "" {
		return nil, fmt.Errorf("没给别名")
	}
	actions := []any{}
	out := &AliasSwitchOut{}
	if !in.KeepOthers {
		// 先查别名当前指向谁：**一次 _aliases 请求里 remove+add 才是原子的**，
		// 分两次调用中间会有一瞬别名指向空/两处，查询方正好撞上就会读到错的数据。
		raw, code, err := esCall(ctx, http.MethodGet, "/_alias/"+url.PathEscape(alias), nil)
		if err != nil {
			return nil, err
		}
		if code < 400 {
			var cur map[string]any
			if json.Unmarshal(raw, &cur) == nil {
				for idx := range cur {
					if idx == target {
						continue
					}
					out.RemovedFrom = append(out.RemovedFrom, idx)
					actions = append(actions, map[string]any{
						"remove": map[string]any{"index": idx, "alias": alias}})
				}
			}
		} else if code != http.StatusNotFound {
			return nil, esError(code, raw)
		}
	}
	actions = append(actions, map[string]any{
		"add": map[string]any{"index": target, "alias": alias}})
	if _, err := esJSON(ctx, http.MethodPost, "/_aliases", map[string]any{"actions": actions}); err != nil {
		return nil, err
	}
	out.OK = true
	return out, nil
}

func opReindex(ctx plugin.Ctx, in *ReindexIn) (*ReindexOut, error) {
	src, err := indexPath(in.Source)
	if err != nil {
		return nil, err
	}
	dest, err := indexPath(in.Dest)
	if err != nil {
		return nil, err
	}
	source := map[string]any{"index": src}
	if len(in.Query) > 0 {
		source["query"] = in.Query
	}
	body := map[string]any{"source": source, "dest": map[string]any{"index": dest}}
	path := "/_reindex"
	if in.Async {
		path += "?wait_for_completion=false"
	}
	res, err := esJSON(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	out := &ReindexOut{
		Created: num(res, "created"), TookMs: num(res, "took"), Task: str(res, "task"),
	}
	out.Failures = len(listAt(res, "failures"))
	return out, nil
}

func opClusterHealth(ctx plugin.Ctx, _ *ClusterHealthIn) (*ClusterHealthOut, error) {
	res, err := esJSON(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return nil, err
	}
	return &ClusterHealthOut{
		Status: str(res, "status"), ClusterName: str(res, "cluster_name"),
		Nodes: num(res, "number_of_nodes"), ActiveShards: num(res, "active_shards"),
		UnassignedShards: num(res, "unassigned_shards"),
	}, nil
}

// —— 保底 ——

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, fmt.Errorf("没给路径（如 /_cat/nodes?format=json）")
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body any
	if len(in.Body) > 0 {
		body = in.Body
	}
	raw, code, err := esCall(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, esError(code, raw)
	}
	out := &CallOut{Status: code}
	if len(raw) > 0 {
		var data any
		if err := json.Unmarshal(raw, &data); err != nil {
			out.Data = string(raw) // _cat 的默认格式是纯文本，原样给出去
		} else {
			out.Data = data
		}
	}
	return out, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	res, err := esJSON(ctx, http.MethodGet, "/", nil)
	if err != nil {
		// 体检的答案是「不可用 + 为什么」，不是抛错——抛错在界面上只剩一个红叉。
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	ver := mapAt(res, "version")
	out := &HealthCheckOut{
		OK:           true,
		Version:      str(ver, "number"),
		Distribution: str(ver, "distribution"),
		ClusterName:  str(res, "cluster_name"),
	}
	if out.Distribution == "" {
		out.Distribution = "elasticsearch" // ES 不回这个字段，OpenSearch 回 "opensearch"
	}
	if h, err := esJSON(ctx, http.MethodGet, "/_cluster/health", nil); err == nil {
		out.Status = str(h, "status")
	}
	out.Message = fmt.Sprintf("连接正常（%s %s，集群 %s，状态 %s）",
		out.Distribution, out.Version, out.ClusterName, orDash(out.Status))
	return out, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
