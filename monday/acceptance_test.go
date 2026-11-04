package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func bind[T any](t *testing.T, s string) *T {
	t.Helper()
	var x T
	if e := contract.BindInput(json.RawMessage(s), &x); e != nil {
		t.Fatal(e)
	}
	return &x
}
func wire(t *testing.T, c *client, method, path, query, body, reply string) {
	t.Helper()
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != method || r.URL.Path != path || r.URL.RawQuery != query {
			t.Fatalf("unexpected target %s %s", r.Method, r.URL)
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != body {
			t.Fatalf("body %s want %s", b, body)
		}
		if strings.Contains(r.URL.String(), "secret-test") {
			t.Fatal("credential in URL")
		}
		if r.URL.Host != "api.monday.com" || r.Header.Get("Authorization") != "secret-test" || r.Header.Get("API-Version") != "2026-10" {
			t.Fatal("authentication/version")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})
}

func TestDocumentedWireRequests(t *testing.T) {
	t.Run("health_check", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"query { me { id name } }","variables":{}}`, `{"data":{"me":{"id":"9007199254740993","name":"Alex"}}}`)
		o, e := c.health_check(context.Background(), bind[HealthCheckIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
		if !o.OK {
			t.Fatal(o)
		}
	})
	t.Run("list_boards", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"query ($page: Int!, $limit: Int!) { boards(page: $page, limit: $limit) { id name state url } }","variables":{"limit":100,"page":1}}`, `{"data":{"boards":[{"id":"9007199254740993","name":"Projects","state":"active","url":"https://acme.monday.com/boards/9007199254740993"}]}}`)
		o, e := c.list_boards(context.Background(), bind[ListBoardsIn](t, `{}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_board", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"query ($ids: [ID!]!) { boards(ids: $ids) { id name state url groups { id title } columns { id title type } } }","variables":{"ids":["9007199254740993"]}}`, `{"data":{"boards":[{"id":"9007199254740993","name":"Projects","state":"active","url":"https://acme.monday.com/boards/9007199254740993","groups":[{"id":"topics","title":"Group"}],"columns":[{"id":"numbers","title":"Amount","type":"numbers"}]}]}}`)
		o, e := c.get_board(context.Background(), bind[GetBoardIn](t, `{"board_id":"9007199254740993"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("list_items", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"query ($ids: [ID!]!, $limit: Int!, $cursor: String) { boards(ids: $ids) { id items_page(limit: $limit, cursor: $cursor) { cursor items { id name state url board { id } group { id title } column_values { id type text value } } } } }","variables":{"cursor":null,"ids":["9007199254740993"],"limit":100}}`, `{"data":{"boards":[{"id":"9007199254740993","items_page":{"cursor":null,"items":[{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":{"id":"topics","title":"Group"},"column_values":[{"id":"numbers","type":"numbers","text":"0","value":"0"}]}]}}]}}`)
		o, e := c.list_items(context.Background(), bind[ListItemsIn](t, `{"board_id":"9007199254740993"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("get_item", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"query ($ids: [ID!]!) { items(ids: $ids) { id name state url board { id } group { id title } column_values { id type text value } } }","variables":{"ids":["9007199254740995"]}}`, `{"data":{"items":[{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":{"id":"topics","title":"Group"},"column_values":[{"id":"numbers","type":"numbers","text":"0","value":"0"}]}]}}`)
		o, e := c.get_item(context.Background(), bind[GetItemIn](t, `{"item_id":"9007199254740995"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("create_item", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"mutation ($board: ID!, $name: String!, $group: String, $values: JSON, $labels: Boolean) { create_item(board_id: $board, item_name: $name, group_id: $group, column_values: $values, create_labels_if_missing: $labels) { id name state url board { id } group { id title } column_values { id type text value } } }","variables":{"board":"9007199254740993","group":null,"labels":false,"name":"Task","values":"{\"numbers\":0}"}}`, `{"data":{"create_item":{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":{"id":"topics","title":"Group"},"column_values":[{"id":"numbers","type":"numbers","text":"0","value":"0"}]}}}`)
		o, e := c.create_item(context.Background(), bind[CreateItemIn](t, `{"board_id":"9007199254740993","name":"Task","options":{"create_labels":false},"column_values_json":"{\"numbers\":0}"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("change_columns", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"mutation ($board: ID!, $item: ID!, $values: JSON!, $labels: Boolean) { change_multiple_column_values(board_id: $board, item_id: $item, column_values: $values, create_labels_if_missing: $labels) { id name state url board { id } group { id title } column_values { id type text value } } }","variables":{"board":"9007199254740993","item":"9007199254740995","labels":false,"values":"{\"numbers\":0}"}}`, `{"data":{"change_multiple_column_values":{"id":"9007199254740995","name":"Task","state":"active","url":"https://acme.monday.com/pulses/9007199254740995","board":{"id":"9007199254740993"},"group":{"id":"topics","title":"Group"},"column_values":[{"id":"numbers","type":"numbers","text":"0","value":"0"}]}}}`)
		o, e := c.change_columns(context.Background(), bind[ChangeColumnsIn](t, `{"board_id":"9007199254740993","item_id":"9007199254740995","column_values_json":"{\"numbers\":0}","options":{"create_labels":false}}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
	t.Run("add_update", func(t *testing.T) {
		c := newClient(Cred{AccessToken: "secret-test"})
		wire(t, c, `POST`, `/v2`, ``, `{"query":"mutation ($item: ID!, $body: String!) { create_update(item_id: $item, body: $body) { id body } }","variables":{"body":"Hello","item":"9007199254740995"}}`, `{"data":{"create_update":{"id":"9007199254740997","body":"Hello"}}}`)
		o, e := c.add_update(context.Background(), bind[AddUpdateIn](t, `{"item_id":"9007199254740995","body":"Hello"}`))
		if e != nil || o == nil {
			t.Fatalf("result=%#v err=%v", o, e)
		}
	})
}
