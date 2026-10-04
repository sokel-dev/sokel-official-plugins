package main

import (
	"encoding/json"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/contract"
)

// 平台发过来的 chunk 长什么样，就得能绑进来。
//
// 这条守的是一次真实故障：assets / source_blocks 在契约里被声明成 []string，
// 而平台发的是**对象数组**（{kind,url,…} / {块 id,type,bbox,页码}）。
// 症状是 `json: cannot unmarshal object into Go value of type string`，
// 而且只有带资产或溯源的文档才会踩到——平时看着一切正常，所以更该有条用例钉住。
func TestChunksUpsertBindsPlatformPayload(t *testing.T) {
	// 形状取自 server/internal/knowledge/pipeline/wire.go 的 BuildChunks
	raw := json.RawMessage(`{
	  "kb_id": "kb_1", "doc_id": "d1", "append": true,
	  "chunks": [{
	    "id": "d1#p0", "role": "parent", "parent_id": "d1#p0", "parent_no": 0,
	    "doc_id": "d1", "title": "研报", "content": "正文", "page_no": 3,
	    "fields": {"industry": "半导体", "score": 7, "tags": ["A", "B"]},
	    "assets": [{"kind": "image", "url": "https://x/1.png", "bbox": [1, 2, 3, 4]}],
	    "source_blocks": [{"id": "b7", "type": "table", "page": 3, "bbox": [0, 0, 1, 1]}],
	    "embedding": [0.1, -0.23]
	  }]
	}`)
	var in ChunksUpsertIn
	if err := contract.BindInput(raw, &in); err != nil {
		t.Fatalf("绑定平台载荷失败: %v", err)
	}
	if len(in.Chunks) != 1 {
		t.Fatalf("chunks 没绑上: %+v", in)
	}
	c := in.Chunks[0]
	if len(c.Assets) != 1 || c.Assets[0]["kind"] != "image" {
		t.Errorf("assets 是对象数组: %+v", c.Assets)
	}
	if len(c.SourceBlocks) != 1 || c.SourceBlocks[0]["type"] != "table" {
		t.Errorf("source_blocks 是对象数组: %+v", c.SourceBlocks)
	}
	if c.Fields["industry"] != "半导体" {
		t.Errorf("fields 是自由结构: %+v", c.Fields)
	}
	// append 决定要不要先清掉该 doc 的既有 chunk——绑错了就是「整篇替换」变成追加，
	// 重复导入会在库里堆两份
	if !in.Append {
		t.Error("append 没绑上")
	}
}
