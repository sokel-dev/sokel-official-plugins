package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/trello/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const boardFields = "name,desc,closed,url"
const listFields = "name,idBoard,closed,pos"
const cardFields = "name,desc,idBoard,idList,closed,due,dueComplete,url"

var idPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

func validID(id string) bool { return idPattern.MatchString(id) }
func checkID(id string) error {
	if !validID(id) {
		return errors.New("Provide the full 24-character Trello resource ID, not a URL or short link.")
	}
	return nil
}
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.APIKey) || !safeToken(c.cred.APIToken) {
		return "", errors.New("Provide a valid Trello API key and user token.")
	}
	enc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return `OAuth oauth_consumer_key="` + enc(c.cred.APIKey) + `", oauth_token="` + enc(c.cred.APIToken) + `"`, nil
}
func filter(s string) (string, error) {
	if s == "" {
		return "open", nil
	}
	if s != "all" && s != "open" && s != "closed" {
		return "", errors.New("Filter must be open, closed or all.")
	}
	return s, nil
}
func validText(s string, max int, empty bool) error {
	if !utf8.ValidString(s) || len(s) > max || strings.ContainsRune(s, 0) || (!empty && strings.TrimSpace(s) == "") {
		return errors.New("Text is empty, invalid UTF-8 or exceeds the supported length.")
	}
	return nil
}
func dueDate(s string) (string, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return "", errors.New("Due date must be RFC3339 with a timezone offset.")
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	e := c.json(ctx, "GET", "/1/members/me", url.Values{"fields": {"id,username"}}, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if !validID(r.ID) || r.Username == "" {
		return &HealthCheckOut{Message: "Trello did not return a valid member identity."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Trello member identity verified. Board access and write scope are checked by each operation."}, nil
}
func (c *client) collection(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if e := c.json(ctx, "GET", path, q, nil, &rows); e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("Trello returned a missing collection.")
	}
	return rows, nil
}
func board(b []byte) (schema.Board, error) {
	var r schema.Board
	if e := requireKeys(b, "id", "name", "desc", "closed", "url"); e != nil {
		return r, e
	}
	if e := decodeJSON(b, &r); e != nil {
		return r, e
	}
	if !validID(r.ID) || r.Name == "" {
		return r, errors.New("Trello returned invalid board metadata.")
	}
	return r, nil
}
func card(b []byte) (schema.Card, error) {
	var r schema.Card
	if e := requireKeys(b, "id", "idBoard", "idList", "name", "desc", "closed", "dueComplete", "url"); e != nil {
		return r, e
	}
	if e := decodeJSON(b, &r); e != nil {
		return r, e
	}
	if !validID(r.ID) || !validID(r.BoardID) || !validID(r.ListID) || r.Name == "" {
		return r, errors.New("Trello returned invalid card metadata.")
	}
	return r, nil
}
func (c *client) list_boards(ctx context.Context, in *ListBoardsIn) (*ListBoardsOut, error) {
	f, e := filter(in.Filter)
	if e != nil {
		return nil, e
	}
	rows, e := c.collection(ctx, "/1/members/me/boards", url.Values{"filter": {f}, "fields": {boardFields}})
	if e != nil {
		return nil, e
	}
	out := &ListBoardsOut{Boards: []schema.Board{}}
	for _, b := range rows {
		r, e := board(b)
		if e != nil {
			return nil, e
		}
		out.Boards = append(out.Boards, r)
	}
	return out, nil
}
func (c *client) get_board(ctx context.Context, in *GetBoardIn) (*GetBoardOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", "/1/boards/"+in.BoardID, url.Values{"fields": {boardFields}}, nil)
	if e != nil {
		return nil, e
	}
	r, e := board(b)
	if e != nil {
		return nil, e
	}
	if r.ID != in.BoardID {
		return nil, errors.New("Trello returned a different board.")
	}
	return &GetBoardOut{Board: r}, nil
}
func (c *client) list_lists(ctx context.Context, in *ListListsIn) (*ListListsOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	f, e := filter(in.Filter)
	if e != nil {
		return nil, e
	}
	rows, e := c.collection(ctx, "/1/boards/"+in.BoardID+"/lists", url.Values{"filter": {f}, "fields": {listFields}})
	if e != nil {
		return nil, e
	}
	out := &ListListsOut{Lists: []schema.BoardList{}}
	for _, b := range rows {
		if e := requireKeys(b, "id", "idBoard", "name", "closed", "pos"); e != nil {
			return nil, e
		}
		var r schema.BoardList
		if e := decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if !validID(r.ID) || r.BoardID != in.BoardID || r.Name == "" || r.Position < 0 {
			return nil, errors.New("Trello returned invalid list metadata.")
		}
		out.Lists = append(out.Lists, r)
	}
	return out, nil
}
func (c *client) list_cards(ctx context.Context, in *ListCardsIn) (*ListCardsOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	f, e := filter(in.Filter)
	if e != nil {
		return nil, e
	}
	limit := 100
	if in.Page.Limit != nil {
		limit = *in.Page.Limit
	}
	if limit < 1 || limit > 1000 {
		return nil, errors.New("Card page size must be between 1 and 1000.")
	}
	q := url.Values{"filter": {f}, "fields": {cardFields}, "limit": {strconv.Itoa(limit)}, "sort": {"-id"}}
	before := ""
	if in.Page.Before != nil {
		before = *in.Page.Before
		if e := checkID(before); e != nil {
			return nil, e
		}
		q.Set("before", before)
	}
	rows, e := c.collection(ctx, "/1/boards/"+in.BoardID+"/cards", q)
	if e != nil {
		return nil, e
	}
	if len(rows) > limit {
		return nil, errors.New("Trello returned more cards than requested.")
	}
	out := &ListCardsOut{Cards: []schema.Card{}, MayHaveMore: len(rows) == limit}
	seen := map[string]bool{}
	for _, b := range rows {
		r, e := card(b)
		if e != nil {
			return nil, e
		}
		if r.BoardID != in.BoardID || seen[r.ID] || (before != "" && r.ID >= before) {
			return nil, errors.New("Trello returned cards outside the requested board or pagination boundary.")
		}
		seen[r.ID] = true
		out.Cards = append(out.Cards, r)
		if out.OldestCardID == "" || r.ID < out.OldestCardID {
			out.OldestCardID = r.ID
		}
	}
	return out, nil
}
func (c *client) get_card(ctx context.Context, in *GetCardIn) (*GetCardOut, error) {
	if e := checkID(in.CardID); e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", "/1/cards/"+in.CardID, url.Values{"fields": {cardFields}}, nil)
	if e != nil {
		return nil, e
	}
	r, e := card(b)
	if e != nil {
		return nil, e
	}
	if r.ID != in.CardID {
		return nil, errors.New("Trello returned a different card.")
	}
	return &GetCardOut{Card: r}, nil
}
func (c *client) create_card(ctx context.Context, in *CreateCardIn) (*CreateCardOut, error) {
	if e := checkID(in.ListID); e != nil {
		return nil, e
	}
	if e := validText(in.Name, 16384, false); e != nil {
		return nil, e
	}
	if e := validText(in.Description, 16384, true); e != nil {
		return nil, e
	}
	body := map[string]any{"idList": in.ListID, "name": in.Name, "desc": in.Description}
	if in.Due != "" {
		s, e := dueDate(in.Due)
		if e != nil {
			return nil, e
		}
		body["due"] = s
	}
	b, e := c.request(ctx, "POST", "/1/cards", nil, body)
	if e != nil {
		return nil, e
	}
	r, e := card(b)
	if e != nil || r.ListID != in.ListID {
		return nil, errors.New("Trello returned no valid card creation receipt. Inspect the list before retrying.")
	}
	return &CreateCardOut{Card: r}, nil
}
func (c *client) update_card(ctx context.Context, in *UpdateCardIn) (*UpdateCardOut, error) {
	if e := checkID(in.CardID); e != nil {
		return nil, e
	}
	v := in.Changes
	body := map[string]any{}
	if v.Name != nil {
		if e := validText(*v.Name, 16384, false); e != nil {
			return nil, e
		}
		body["name"] = *v.Name
	}
	if v.Description != nil {
		if e := validText(*v.Description, 16384, true); e != nil {
			return nil, e
		}
		body["desc"] = *v.Description
	}
	if v.ListID != nil {
		if e := checkID(*v.ListID); e != nil {
			return nil, e
		}
		body["idList"] = *v.ListID
	}
	if v.Due != nil {
		if *v.Due == "" {
			body["due"] = nil
		} else {
			s, e := dueDate(*v.Due)
			if e != nil {
				return nil, e
			}
			body["due"] = s
		}
	}
	if v.DueComplete != nil {
		body["dueComplete"] = *v.DueComplete
	}
	if v.Closed != nil {
		body["closed"] = *v.Closed
	}
	if len(body) == 0 {
		return nil, errors.New("Provide at least one card change.")
	}
	b, e := c.request(ctx, "PUT", "/1/cards/"+in.CardID, nil, body)
	if e != nil {
		return nil, e
	}
	r, e := card(b)
	if e != nil || r.ID != in.CardID || (v.ListID != nil && r.ListID != *v.ListID) {
		return nil, errors.New("Trello returned no valid card update receipt. Inspect the card before retrying.")
	}
	return &UpdateCardOut{Card: r}, nil
}
func (c *client) add_comment(ctx context.Context, in *AddCommentIn) (*AddCommentOut, error) {
	if e := checkID(in.CardID); e != nil {
		return nil, e
	}
	if e := validText(in.Text, 16384, false); e != nil {
		return nil, e
	}
	var r struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Date     string `json:"date"`
		MemberID string `json:"idMemberCreator"`
		Data     struct {
			Text string `json:"text"`
			Card struct {
				ID string `json:"id"`
			} `json:"card"`
		} `json:"data"`
	}
	if e := c.json(ctx, "POST", "/1/cards/"+in.CardID+"/actions/comments", nil, map[string]any{"text": in.Text}, &r); e != nil {
		return nil, e
	}
	if !validID(r.ID) || !validID(r.MemberID) || r.Type != "commentCard" || r.Date == "" || r.Data.Card.ID != in.CardID || r.Data.Text != in.Text {
		return nil, errors.New("Trello returned no valid comment receipt. Inspect the card before retrying.")
	}
	return &AddCommentOut{Comment: schema.Comment{ID: r.ID, CardID: r.Data.Card.ID, Text: r.Data.Text, Date: r.Date, MemberID: r.MemberID}}, nil
}
