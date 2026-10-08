package main

import (
	"encoding/json"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/kbstore-es/schema"
)

// Only a real child chunk has a child number: role child whose parent is another chunk. The contract carries child_no
// as an int, so "no child number" arrives as 0; written as 0, the platform's "reading units only" browse
// ({field: child_no, missing: true}) never matched a parent and the list came back empty.
func TestChunkDocChildNoOnlyOnChildren(t *testing.T) {
	cases := []struct {
		name string
		c    schema.Chunk
		want bool
	}{
		{"parent", schema.Chunk{ID: "d#p0", Role: "parent", ParentID: "d#p0"}, false},
		{"general-mode chunk (its own parent)", schema.Chunk{ID: "d#p1", Role: "child", ParentID: "d#p1"}, false},
		{"first child", schema.Chunk{ID: "d#p0#c0", Role: "child", ParentID: "d#p0", ChildNo: 0}, true},
		{"later child", schema.Chunk{ID: "d#p0#c3", Role: "child", ParentID: "d#p0", ChildNo: 3}, true},
	}
	for _, c := range cases {
		_, has := chunkDoc(c.c)["child_no"]
		if has != c.want {
			t.Errorf("%s: child_no written = %v, want %v", c.name, has, c.want)
		}
	}
}

// Documents indexed before the fix carry child_no 0 on parents too, so "missing child_no" also takes role parent.
func TestMissingChildNoKeepsLegacyParents(t *testing.T) {
	q := buildBool([]schema.Filter{{Field: "child_no", Missing: true}}, schema.TimeRange{})
	got, _ := json.Marshal(q)
	want := `{"bool":{"filter":[{"bool":{"minimum_should_match":1,"should":[{"bool":{"must_not":[{"exists":{"field":"child_no"}}]}},{"term":{"role":"parent"}}]}}]}}`
	if string(got) != want {
		t.Fatalf("missing child_no should take parents written with child_no 0\n got %s\nwant %s", got, want)
	}
}
