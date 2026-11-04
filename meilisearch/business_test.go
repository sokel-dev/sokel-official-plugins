package main

import (
	"context"
	"strings"
	"testing"
)

func TestMalformedBusinessReceipts(t *testing.T) {
	t.Run("list_indexes", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.list_indexes(context.Background(), bind[ListIndexesIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("get_index", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.get_index(context.Background(), bind[GetIndexIn](t, `{"index_uid":"movies"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("create_index", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 202, body)
			_, e := c.create_index(context.Background(), bind[CreateIndexIn](t, `{"index_uid":"movies","primary_key":"id"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("search", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.search(context.Background(), bind[SearchIn](t, `{"index_uid":"movies","options":{"offset":0},"query":"ninja"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("get_document", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.get_document(context.Background(), bind[GetDocumentIn](t, `{"document_id":"9007199254740993","index_uid":"movies"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("add_documents", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 202, body)
			_, e := c.add_documents(context.Background(), bind[AddDocumentsIn](t, `{"documents_json":"[{\"id\":9007199254740993,\"available\":false}]","index_uid":"movies","primary_key":"id"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("delete_document", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 202, body)
			_, e := c.delete_document(context.Background(), bind[DeleteDocumentIn](t, `{"document_id":"9007199254740993","index_uid":"movies"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
	t.Run("get_task", func(t *testing.T) {
		for _, body := range []string{`{}`, `null`, `[]`, `{"message":"secret-test","type":"auth","code":"invalid_api_key"}`} {
			c := fresh()
			withReply(c, 200, body)
			_, e := c.get_task(context.Background(), bind[GetTaskIn](t, `{"task_uid":"0"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("body=%s err=%v", body, e)
			}
		}
	})
}
