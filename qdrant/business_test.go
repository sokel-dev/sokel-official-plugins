package main

import (
	"context"
	"strings"
	"testing"
)

func TestMalformedBusinessReceipts(t *testing.T) {
	t.Run("list_collections", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.list_collections(context.Background(), bind[ListCollectionsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("get_collection", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.get_collection(context.Background(), bind[GetCollectionIn](t, `{"collection":"books"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("create_collection", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.create_collection(context.Background(), bind[CreateCollectionIn](t, `{"collection":"books","distance":"Cosine","size":3}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("query_points", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.query_points(context.Background(), bind[QueryPointsIn](t, `{"collection":"books","options":{"score_threshold":0},"vector":[0.9,0.1,0.1]}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("upsert_points", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.upsert_points(context.Background(), bind[UpsertPointsIn](t, `{"collection":"books","points":[{"id":"18446744073709551615","payload_json":"{\"count\":9007199254740993,\"flag\":false}","vector":[0.9,0.1,0.1]}]}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("retrieve_points", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.retrieve_points(context.Background(), bind[RetrievePointsIn](t, `{"collection":"books","ids":["18446744073709551615","76874cce-1fb9-4e16-9b0b-f085ac06ed6f"]}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("scroll_points", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.scroll_points(context.Background(), bind[ScrollPointsIn](t, `{"collection":"books","options":{"limit":1,"offset":"0"}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("delete_points", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"status":{"error":"secret-test"},"result":{}}`, `{"status":"ok","result":null}`, `{"status":"ok","result":{}}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.delete_points(context.Background(), bind[DeletePointsIn](t, `{"collection":"books","ids":["18446744073709551615"]}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
}
