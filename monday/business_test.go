package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBusinessReceiptsAndErrorRedaction(t *testing.T) {
	t.Run("list_boards", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"boards":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_boards(context.Background(), bind[ListBoardsIn](t, `{}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"data":{"boards":[{"id":"9007199254740993","name":"Projects","state":"active","url":"https://acme.monday.com/boards/9007199254740993"},{"id":"9007199254740993","name":"Projects","state":"active","url":"https://acme.monday.com/boards/9007199254740993"}]}}`)
		if _, e := c.list_boards(context.Background(), bind[ListBoardsIn](t, `{}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("get_board", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"boards":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.get_board(context.Background(), bind[GetBoardIn](t, `{"board_id":"9007199254740993"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("list_items", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"boards":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.list_items(context.Background(), bind[ListItemsIn](t, `{"board_id":"9007199254740993"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
		c := fresh()
		withReply(c, 200, `{"data":{"boards":[{"id":"9007199254740993","items_page":{"cursor":null,"items":[{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":{"id":"topics","title":"Group"},"column_values":[{"id":"numbers","type":"numbers","text":"0","value":"0"}]},{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":{"id":"topics","title":"Group"},"column_values":[{"id":"numbers","type":"numbers","text":"0","value":"0"}]}]}}]}}`)
		if _, e := c.list_items(context.Background(), bind[ListItemsIn](t, `{"board_id":"9007199254740993"}`)); e == nil {
			t.Fatal("duplicate IDs accepted")
		}
	})
	t.Run("get_item", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"items":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.get_item(context.Background(), bind[GetItemIn](t, `{"item_id":"9007199254740995"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("create_item", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"create_item":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.create_item(context.Background(), bind[CreateItemIn](t, `{"board_id":"9007199254740993","name":"Task","column_values_json":"{\"numbers\":0}","options":{"create_labels":false}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("change_columns", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"change_multiple_column_values":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.change_columns(context.Background(), bind[ChangeColumnsIn](t, `{"board_id":"9007199254740993","item_id":"9007199254740995","column_values_json":"{\"numbers\":0}","options":{"create_labels":false}}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
	t.Run("add_update", func(t *testing.T) {
		for _, reply := range []string{`{}`, `null`, `[]`, `{"errors":[{"message":"secret-test"}],"data":{}}`, `{"error_code":400,"error_message":"secret-test"}`, `{"data":{"create_update":null}}`} {
			c := fresh()
			withReply(c, 200, reply)
			_, e := c.add_update(context.Background(), bind[AddUpdateIn](t, `{"item_id":"9007199254740995","body":"Hello"}`))
			if e == nil || strings.Contains(e.Error(), "secret-test") {
				t.Fatalf("reply=%s err=%v", reply, e)
			}
		}
	})
}

var _ = json.Valid
