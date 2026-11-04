package main

import (
	"context"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/trello/schema"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestTrelloValidationAndProjection(t *testing.T) {
	for _, id := range []string{"", "../x", "short", "https://trello.com/c/abc", strings.Repeat("x", 24)} {
		if checkID(id) == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	for _, f := range []string{"all", "open", "closed"} {
		if _, e := filter(f); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := filter("bad"); e == nil {
		t.Fatal("bad filter")
	}
	for _, s := range []string{"", "  ", "\x00", strings.Repeat("x", 16385), string([]byte{255})} {
		if validText(s, 16384, false) == nil {
			t.Fatal("bad text accepted")
		}
	}
	if _, e := dueDate("bad"); e == nil {
		t.Fatal("bad date")
	}
	if s, e := dueDate("2026-11-05T09:00:00+08:00"); e != nil || s != "2026-11-05T01:00:00Z" {
		t.Fatal(s, e)
	}
	for _, b := range []string{`{}`, `null`, `[]`, `{"id":"x","name":"n","closed":false,"desc":"","url":"x"}`, strings.Replace(boardJSON, `"closed":false,`, "", 1)} {
		if _, e := board([]byte(b)); e == nil {
			t.Fatal("bad board accepted")
		}
	}
	for _, b := range []string{`{}`, strings.Replace(cardJSON, `"dueComplete":false,`, "", 1), strings.Replace(cardJSON, cardID, "x", 1)} {
		if _, e := card([]byte(b)); e == nil {
			t.Fatal("bad card accepted")
		}
	}
	c := fresh()
	c.cred.APIKey = "bad\nkey"
	if _, e := c.authorization(); e == nil {
		t.Fatal("bad key")
	}
	c = fresh()
	c.cred.APIToken = `quote"+token`
	a, e := c.authorization()
	if e != nil || !strings.Contains(a, "quote%22%2Btoken") {
		t.Fatal("unsafe OAuth encoding", a, e)
	}
}
func TestTrelloPagingAndWrites(t *testing.T) {
	ctx := context.Background()
	c := fresh()
	older := "5b6893f01cb3228998cf6290"
	newer := "5b6893f01cb3228998cf62ff"
	fixture(t, c, "GET", "/1/boards/"+boardID+"/cards", url.Values{"filter": {"all"}, "fields": {cardFields}, "limit": {"2"}, "sort": {"-id"}, "before": {newer}}, "", 200, "["+cardJSON+","+strings.ReplaceAll(cardJSON, cardID, older)+"]")
	o, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID, Filter: "all", Page: schema.CardPage{Limit: ptr(2), Before: ptr(newer)}})
	if e != nil || o.OldestCardID != older || !o.MayHaveMore {
		t.Fatal(o, e)
	}
	for _, reply := range []string{"null", "{}", "[" + cardJSON + "," + cardJSON + "]", "[" + strings.Replace(cardJSON, boardID, newer, 1) + "]"} {
		c = fresh()
		withReply(c, 200, reply)
		if _, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID}); e == nil {
			t.Fatal("invalid card collection accepted")
		}
	}
	c = fresh()
	withReply(c, 200, "["+cardJSON+"]")
	if _, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID, Page: schema.CardPage{Before: ptr(cardID)}}); e == nil {
		t.Fatal("repeated before accepted")
	}
	c = fresh()
	withReply(c, 200, "["+cardJSON+"]")
	if _, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID, Page: schema.CardPage{Before: ptr(older)}}); e == nil {
		t.Fatal("card newer than before boundary accepted")
	}
	c = fresh()
	fixture(t, c, "PUT", "/1/cards/"+cardID, nil, `{"closed":true,"desc":"Details","due":"2026-11-05T01:00:00Z","dueComplete":true,"idList":"5b6893f01cb3228998cf629f","name":"Review"}`, 200, cardJSON)
	_, e = c.update_card(ctx, &UpdateCardIn{CardID: cardID, Changes: schema.CardChanges{Name: ptr("Review"), Description: ptr("Details"), Due: ptr("2026-11-05T09:00:00+08:00"), DueComplete: ptr(true), Closed: ptr(true), ListID: ptr(listID)}})
	if e != nil {
		t.Fatal(e)
	}
	c = fresh()
	fixture(t, c, "POST", "/1/cards", nil, `{"desc":"Plan","due":"2026-11-05T01:00:00Z","idList":"5b6893f01cb3228998cf629f","name":"Review"}`, 200, cardJSON)
	if _, e = c.create_card(ctx, &CreateCardIn{ListID: listID, Name: "Review", Description: "Plan", Due: "2026-11-05T09:00:00+08:00"}); e != nil {
		t.Fatal(e)
	}
}
func TestTrelloBadInputsAndReceipts(t *testing.T) {
	ctx := context.Background()
	cases := []func(*client) error{
		func(c *client) error { _, e := c.get_board(ctx, &GetBoardIn{}); return e },
		func(c *client) error { _, e := c.list_lists(ctx, &ListListsIn{}); return e },
		func(c *client) error {
			_, e := c.list_lists(ctx, &ListListsIn{BoardID: boardID, Filter: "bad"})
			return e
		},
		func(c *client) error { _, e := c.list_cards(ctx, &ListCardsIn{}); return e },
		func(c *client) error {
			_, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID, Filter: "bad"})
			return e
		},
		func(c *client) error {
			_, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID, Page: schema.CardPage{Limit: ptr(0)}})
			return e
		},
		func(c *client) error {
			_, e := c.list_cards(ctx, &ListCardsIn{BoardID: boardID, Page: schema.CardPage{Before: ptr("bad")}})
			return e
		},
		func(c *client) error { _, e := c.get_card(ctx, &GetCardIn{}); return e },
		func(c *client) error { _, e := c.create_card(ctx, &CreateCardIn{}); return e },
		func(c *client) error { _, e := c.create_card(ctx, &CreateCardIn{ListID: listID}); return e },
		func(c *client) error {
			_, e := c.create_card(ctx, &CreateCardIn{ListID: listID, Name: "x", Description: string([]byte{255})})
			return e
		},
		func(c *client) error {
			_, e := c.create_card(ctx, &CreateCardIn{ListID: listID, Name: "x", Due: "bad"})
			return e
		},
		func(c *client) error { _, e := c.update_card(ctx, &UpdateCardIn{}); return e },
		func(c *client) error { _, e := c.update_card(ctx, &UpdateCardIn{CardID: cardID}); return e },
		func(c *client) error {
			_, e := c.update_card(ctx, &UpdateCardIn{CardID: cardID, Changes: schema.CardChanges{Name: ptr("")}})
			return e
		},
		func(c *client) error {
			_, e := c.update_card(ctx, &UpdateCardIn{CardID: cardID, Changes: schema.CardChanges{Description: ptr("\x00")}})
			return e
		},
		func(c *client) error {
			_, e := c.update_card(ctx, &UpdateCardIn{CardID: cardID, Changes: schema.CardChanges{ListID: ptr("bad")}})
			return e
		},
		func(c *client) error {
			_, e := c.update_card(ctx, &UpdateCardIn{CardID: cardID, Changes: schema.CardChanges{Due: ptr("bad")}})
			return e
		},
		func(c *client) error { _, e := c.add_comment(ctx, &AddCommentIn{}); return e },
		func(c *client) error { _, e := c.add_comment(ctx, &AddCommentIn{CardID: cardID}); return e },
	}
	for i, f := range cases {
		c := fresh()
		c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected network") })}
		e := f(c)
		if e == nil || strings.Contains(e.Error(), "transport") {
			t.Fatalf("case %d: %v", i, e)
		}
	}
	for _, body := range []string{`{}`, `{"error":"failed"}`, `null`, strings.Replace(cardJSON, cardID, "5b6893f01cb3228998cf62ff", 1)} {
		c := fresh()
		withReply(c, 200, body)
		if _, e := c.update_card(ctx, &UpdateCardIn{CardID: cardID, Changes: schema.CardChanges{Name: ptr("x")}}); e == nil {
			t.Fatal("false update success")
		}
	}
	for _, call := range []func(*client) error{func(c *client) error { _, e := c.create_card(ctx, &CreateCardIn{ListID: listID, Name: "x"}); return e }, func(c *client) error { _, e := c.add_comment(ctx, &AddCommentIn{CardID: cardID, Text: "x"}); return e }, func(c *client) error { _, e := c.list_boards(ctx, &ListBoardsIn{}); return e }, func(c *client) error { _, e := c.list_lists(ctx, &ListListsIn{BoardID: boardID}); return e }} {
		c := fresh()
		withReply(c, 200, `{}`)
		if call(c) == nil {
			t.Fatal("false success")
		}
	}
}
