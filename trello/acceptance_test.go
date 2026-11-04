package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func ptr[T any](v T) *T                                                   { return &v }

// Fixtures below are synthetic examples based on the official reference shapes.
// They are not captured from a live account. The transport checks the actual request.
func fixture(t *testing.T, c *client, method, path string, query url.Values, body string, status int, reply string) {
	t.Helper()
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Helper()
		if r.Method != method || r.URL.Scheme != "https" || r.URL.Host != "api.trello.com" || r.URL.Path != path {
			t.Fatalf("request %s %s", r.Method, r.URL)
		}
		if r.URL.Query().Encode() != query.Encode() {
			t.Fatalf("query %s want %s", r.URL.RawQuery, query.Encode())
		}
		if r.Header.Get("Authorization") != "OAuth oauth_consumer_key=\"key-test\", oauth_token=\"token-test\"" {
			t.Fatal("missing header authorization")
		}
		if r.URL.Query().Get("token") != "" || r.URL.Query().Get("key") != "" || r.URL.Query().Get("access_token") != "" {
			t.Fatal("URL credential leak")
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != body {
			t.Fatalf("body %s want %s", b, body)
		}
		if body != "" && r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON Content-Type")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(reply)), Request: r}, nil
	})}
}
func bind(t *testing.T, body string, out any) {
	t.Helper()
	if e := contract.BindInput(json.RawMessage(body), out); e != nil {
		t.Fatal(e)
	}
}

const boardID = "5b6893f01cb3228998cf629e"
const listID = "5b6893f01cb3228998cf629f"
const cardID = "5b6893f01cb3228998cf62a0"
const boardJSON = `{"id":"5b6893f01cb3228998cf629e","name":"Roadmap","desc":"Plan","closed":false,"url":"https://trello.com/b/test"}`
const cardJSON = `{"id":"5b6893f01cb3228998cf62a0","idBoard":"5b6893f01cb3228998cf629e","idList":"5b6893f01cb3228998cf629f","name":"Review","desc":"","closed":false,"due":null,"dueComplete":false,"url":"https://trello.com/c/test"}`

func fresh() *client { return newClient(Cred{APIKey: "key-test", APIToken: "token-test"}) }
func TestDocumentedWireRequests(t *testing.T) {
	ctx := context.Background()
	t.Run("health", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/1/members/me", url.Values{"fields": {"id,username"}}, "", 200, `{"id":"5b6893f01cb3228998cf629e","username":"demo"}`)
		o, e := c.health_check(ctx, &HealthCheckIn{})
		if e != nil || o == nil || !o.OK {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("boards", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/1/members/me/boards", url.Values{"filter": {"open"}, "fields": {"name,desc,closed,url"}}, "", 200, "["+boardJSON+"]")
		o, e := c.list_boards(ctx, &ListBoardsIn{})
		if e != nil || o == nil || len(o.Boards) != 1 || o.Boards[0].ID != boardID {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("board", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/1/boards/"+boardID, url.Values{"fields": {"name,desc,closed,url"}}, "", 200, boardJSON)
		o, e := c.get_board(ctx, &GetBoardIn{BoardID: boardID})
		if e != nil || o == nil || o.Board.Name != "Roadmap" {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("lists", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/1/boards/"+boardID+"/lists", url.Values{"filter": {"open"}, "fields": {"name,idBoard,closed,pos"}}, "", 200, `[{"id":"5b6893f01cb3228998cf629f","idBoard":"5b6893f01cb3228998cf629e","name":"Doing","closed":false,"pos":16384}]`)
		o, e := c.list_lists(ctx, &ListListsIn{BoardID: boardID})
		if e != nil || o == nil || len(o.Lists) != 1 || o.Lists[0].Position != 16384 {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("cards", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/1/boards/"+boardID+"/cards", url.Values{"filter": {"open"}, "fields": {"name,desc,idBoard,idList,closed,due,dueComplete,url"}, "limit": {"100"}, "sort": {"-id"}}, "", 200, "["+cardJSON+"]")
		o, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID})
		if e != nil || o == nil || len(o.Cards) != 1 || o.OldestCardID != cardID || o.MayHaveMore {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("card", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "GET", "/1/cards/"+cardID, url.Values{"fields": {"name,desc,idBoard,idList,closed,due,dueComplete,url"}}, "", 200, cardJSON)
		o, e := c.get_card(ctx, &GetCardIn{CardID: cardID})
		if e != nil || o == nil || o.Card.ID != cardID {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("create", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "POST", "/1/cards", nil, `{"desc":"","idList":"5b6893f01cb3228998cf629f","name":"Review"}`, 200, cardJSON)
		o, e := c.create_card(ctx, &CreateCardIn{ListID: listID, Name: "Review"})
		if e != nil || o == nil || o.Card.ID != cardID {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("update false and clear", func(t *testing.T) {
		c := fresh()
		var in UpdateCardIn
		bind(t, `{"card_id":"5b6893f01cb3228998cf62a0","changes":{"description":"","closed":false,"due":"","due_complete":false}}`, &in)
		fixture(t, c, "PUT", "/1/cards/"+cardID, nil, `{"closed":false,"desc":"","due":null,"dueComplete":false}`, 200, cardJSON)
		o, e := c.update_card(ctx, &in)
		if e != nil || o == nil || o.Card.ID != cardID {
			t.Fatalf("%+v %v", o, e)
		}
	})
	t.Run("comment", func(t *testing.T) {
		c := fresh()
		fixture(t, c, "POST", "/1/cards/"+cardID+"/actions/comments", nil, `{"text":"Ready for review"}`, 200, `{"id":"5b6893f01cb3228998cf62a1","idMemberCreator":"5b6893f01cb3228998cf629e","type":"commentCard","date":"2026-11-04T01:00:00Z","data":{"text":"Ready for review","card":{"id":"5b6893f01cb3228998cf62a0"}}}`)
		o, e := c.add_comment(ctx, &AddCommentIn{CardID: cardID, Text: "Ready for review"})
		if e != nil || o == nil || o.Comment.Text != "Ready for review" {
			t.Fatalf("%+v %v", o, e)
		}
	})
}
