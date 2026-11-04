package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/qdrant/schema"
	"os"
	"strings"
	"testing"
)

func TestCapturedServiceResponses(t *testing.T) {
	b, e := os.ReadFile("testdata/local-service-captures.json")
	if e != nil {
		t.Fatal(e)
	}
	var entries []captureEntry
	if e = json.Unmarshal(b, &entries); e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	for _, r := range entries {
		if r.Method == "DELETE" {
			continue
		}
		c := fresh()
		withReply(c, r.Status, string(r.Response))
		parts := strings.Split(r.Path, "/")
		name := ""
		if len(parts) > 2 {
			name = parts[2]
		}
		ctx := context.Background()
		kind := ""
		switch {
		case r.Path == "/collections":
			kind = "list"
			_, e = c.list_collections(ctx, &ListCollectionsIn{})
		case len(parts) == 3 && r.Method == "GET":
			kind = "metadata"
			_, e = c.get_collection(ctx, &GetCollectionIn{Collection: name})
		case len(parts) == 3 && r.Method == "PUT":
			kind = "create"
			_, e = c.create_collection(ctx, &CreateCollectionIn{Collection: name, Size: 3, Distance: "Cosine"})
		case strings.HasSuffix(r.Path, "/points/query"):
			kind = "query"
			_, e = c.query_points(ctx, &QueryPointsIn{Collection: name, Vector: []float64{1, 0, 0}, Options: schema.QueryOptions{Limit: ptr(1)}})
		case strings.HasSuffix(r.Path, "/points/scroll"):
			kind = "scroll"
			var v struct {
				Limit  int             `json:"limit"`
				Offset json.RawMessage `json:"offset"`
			}
			if e = json.Unmarshal(r.Request, &v); e != nil {
				t.Fatal(e)
			}
			in := &ScrollPointsIn{Collection: name, Options: schema.ScrollOptions{Limit: &v.Limit}}
			if len(v.Offset) > 0 {
				s, err := readID(v.Offset)
				if err != nil {
					t.Fatal(err)
				}
				in.Options.Offset = &s
			}
			_, e = c.scroll_points(ctx, in)
		case strings.HasSuffix(r.Path, "/points/delete"):
			kind = "delete"
			_, e = c.delete_points(ctx, &DeletePointsIn{Collection: name, IDs: []string{"0"}})
		case strings.HasSuffix(r.Path, "/points") && r.Method == "PUT":
			kind = "upsert"
			_, e = c.upsert_points(ctx, &UpsertPointsIn{Collection: name, Points: []schema.PointInput{{ID: "0", Vector: []float64{1, 0, 0}}}})
		case strings.HasSuffix(r.Path, "/points") && r.Method == "POST":
			kind = "retrieve"
			var v struct {
				IDs []json.RawMessage `json:"ids"`
			}
			if e = json.Unmarshal(r.Request, &v); e != nil {
				t.Fatal(e)
			}
			ids := []string{}
			for _, raw := range v.IDs {
				s, err := readID(raw)
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, s)
			}
			_, e = c.retrieve_points(ctx, &RetrievePointsIn{Collection: name, IDs: ids})
		default:
			t.Fatalf("unrecognized captured route %s %s", r.Method, r.Path)
		}
		if e != nil {
			t.Fatalf("%s %s: %v", r.Method, r.Path, e)
		}
		counts[kind]++
	}
	if len(counts) != 8 {
		t.Fatal("missing captured operation family", counts)
	}
}
