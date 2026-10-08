package main

import (
	"encoding/json"
	"testing"

	"github.com/sokel-dev/sokel-official-plugins/kbstore-es/schema"
)

// Range bounds on metadata become an ES range clause on the field (ES compares numbers as numbers and dates as
// dates by the field's mapping); an excluded range goes to must_not, which keeps documents without the field.
func TestBuildBoolRange(t *testing.T) {
	q := buildBool([]schema.Filter{
		{Field: "rating", Gte: "4"},
		{Field: "published", Gte: "2026-10-01", Lte: "2026-10-31", Exclude: true},
	}, schema.TimeRange{})
	got, _ := json.Marshal(q)
	want := `{"bool":{"filter":[{"range":{"fields.rating":{"gte":"4"}}}],"must_not":[{"range":{"fields.published":{"gte":"2026-10-01","lte":"2026-10-31"}}}]}}`
	if string(got) != want {
		t.Fatalf("范围过滤的 ES 子句不对\n got %s\nwant %s", got, want)
	}
}
