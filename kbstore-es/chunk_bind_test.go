package main

import (
	"encoding/json"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/contract"
)

// Whatever shape a chunk arrives from the platform in, it has to be bindable.
//
// This guards against a real production failure: assets / source_blocks were declared as
// []string in the contract, while the platform actually sends **arrays of objects**
// ({kind,url,…} / {block id,type,bbox,page number}). The symptom was
// `json: cannot unmarshal object into Go value of type string`, and only documents carrying
// assets or provenance ever hit it — everything looks fine the rest of the time, which is exactly
// why this needs a test pinning it down.
func TestChunksUpsertBindsPlatformPayload(t *testing.T) {
	// Shape taken from BuildChunks in server/internal/knowledge/pipeline/wire.go
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
	// append decides whether to clear the doc's existing chunks first — binding it wrong turns
	// "whole-document replacement" into "append", and a repeated import piles up two copies in the
	// store
	if !in.Append {
		t.Error("append 没绑上")
	}
}
