package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/monday/schema"
	"regexp"
	"strings"
	"unicode/utf8"
)

const boardFields = "id name state url"
const itemFields = "id name state url board { id } group { id title } column_values { id type text value }"

var numberID = regexp.MustCompile(`^[1-9][0-9]{0,39}$`)
var namedID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func checkID(s string) error {
	if !numberID.MatchString(s) {
		return errors.New("Resource ID must be a positive decimal string, not a URL.")
	}
	return nil
}
func textOK(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func cursorOK(s string) bool {
	return len(s) <= 8192 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func (c *client) gql(ctx context.Context, query string, vars map[string]any) (map[string]json.RawMessage, error) {
	var env struct {
		Data         map[string]json.RawMessage `json:"data"`
		Errors       []json.RawMessage          `json:"errors"`
		ErrorCode    json.RawMessage            `json:"error_code"`
		ErrorMessage string                     `json:"error_message"`
	}
	e := c.json(ctx, "POST", "/v2", nil, map[string]any{"query": query, "variables": vars}, &env)
	if e != nil {
		return nil, e
	}
	if len(env.Errors) > 0 || env.ErrorMessage != "" || (len(env.ErrorCode) > 0 && string(env.ErrorCode) != "null" && string(env.ErrorCode) != "0") {
		return nil, errors.New("Monday rejected the GraphQL operation. Check permissions, column formats and API limits; inspect writes before retrying.")
	}
	if env.Data == nil {
		return nil, errors.New("Monday returned no GraphQL data.")
	}
	return env.Data, nil
}
func decodeField(data map[string]json.RawMessage, key string, out any) error {
	b := data[key]
	if len(b) == 0 || string(b) == "null" {
		return errors.New("Monday returned no requested resource.")
	}
	return decodeJSON(b, out)
}
func boardValid(v schema.Board) bool {
	return checkID(v.ID) == nil && v.Name != "" && v.State != "" && v.URL != ""
}
func itemValid(v schema.Item) bool {
	if checkID(v.ID) != nil || checkID(v.Board.ID) != nil || v.Name == "" || v.URL == "" || v.State == "" || v.Columns == nil {
		return false
	}
	for _, col := range v.Columns {
		if col.ID == "" || col.Type == "" {
			return false
		}
	}
	return v.Group == nil || (v.Group.ID != "" && v.Group.Title != "")
}
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	data, e := c.gql(ctx, "query { me { id name } }", map[string]any{})
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	var me struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	e = decodeField(data, "me", &me)
	if e != nil || checkID(me.ID) != nil || me.Name == "" {
		return &HealthCheckOut{Message: "Monday did not return a valid current user."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Monday credentials are usable for the current user."}, nil
}
func (c *client) list_boards(ctx context.Context, in *ListBoardsIn) (*ListBoardsOut, error) {
	page, limit := 1, 100
	if in.Page.Number != nil {
		page = *in.Page.Number
	}
	if in.Page.Limit != nil {
		limit = *in.Page.Limit
	}
	if page < 1 || page > 10000 || limit < 1 || limit > 100 {
		return nil, errors.New("Board page must be 1..10000 and page size 1..100.")
	}
	data, e := c.gql(ctx, "query ($page: Int!, $limit: Int!) { boards(page: $page, limit: $limit) { "+boardFields+" } }", map[string]any{"page": page, "limit": limit})
	if e != nil {
		return nil, e
	}
	var rows []schema.Board
	if e = decodeField(data, "boards", &rows); e != nil {
		return nil, e
	}
	if rows == nil || len(rows) > limit {
		return nil, errors.New("Monday returned an invalid board page.")
	}
	seen := map[string]bool{}
	for _, v := range rows {
		if !boardValid(v) || seen[v.ID] {
			return nil, errors.New("Monday returned invalid or duplicate boards.")
		}
		seen[v.ID] = true
	}
	out := &ListBoardsOut{Boards: rows}
	if len(rows) == limit {
		if page == 10000 {
			return nil, errors.New("Board pagination exceeds the plugin page limit.")
		}
		out.NextPage = page + 1
	}
	return out, nil
}
func (c *client) get_board(ctx context.Context, in *GetBoardIn) (*GetBoardOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	data, e := c.gql(ctx, "query ($ids: [ID!]!) { boards(ids: $ids) { "+boardFields+" groups { id title } columns { id title type } } }", map[string]any{"ids": []string{in.BoardID}})
	if e != nil {
		return nil, e
	}
	var rows []struct {
		schema.Board
		Groups  []schema.Group  `json:"groups"`
		Columns []schema.Column `json:"columns"`
	}
	if e = decodeField(data, "boards", &rows); e != nil {
		return nil, e
	}
	if len(rows) != 1 || rows[0].ID != in.BoardID || !boardValid(rows[0].Board) || rows[0].Groups == nil || rows[0].Columns == nil {
		return nil, errors.New("Monday returned no complete matching board structure.")
	}
	v := rows[0]
	for _, g := range v.Groups {
		if g.ID == "" || g.Title == "" {
			return nil, errors.New("Monday returned an invalid group.")
		}
	}
	for _, col := range v.Columns {
		if col.ID == "" || col.Title == "" || col.Type == "" {
			return nil, errors.New("Monday returned an invalid column.")
		}
	}
	return &GetBoardOut{Board: v.Board, Groups: v.Groups, Columns: v.Columns}, nil
}
func (c *client) list_items(ctx context.Context, in *ListItemsIn) (*ListItemsOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	limit := 100
	if in.Page.Limit != nil {
		limit = *in.Page.Limit
	}
	if limit < 1 || limit > 500 {
		return nil, errors.New("Item page size must be 1..500.")
	}
	var cursor any
	prior := ""
	if in.Page.Cursor != nil {
		prior = *in.Page.Cursor
		if prior == "" || !cursorOK(prior) {
			return nil, errors.New("Invalid item cursor.")
		}
		cursor = prior
	}
	data, e := c.gql(ctx, "query ($ids: [ID!]!, $limit: Int!, $cursor: String) { boards(ids: $ids) { id items_page(limit: $limit, cursor: $cursor) { cursor items { "+itemFields+" } } } }", map[string]any{"ids": []string{in.BoardID}, "limit": limit, "cursor": cursor})
	if e != nil {
		return nil, e
	}
	var rows []struct {
		ID   string `json:"id"`
		Page *struct {
			Cursor json.RawMessage `json:"cursor"`
			Items  []schema.Item   `json:"items"`
		} `json:"items_page"`
	}
	if e = decodeField(data, "boards", &rows); e != nil {
		return nil, e
	}
	if len(rows) != 1 || rows[0].ID != in.BoardID || rows[0].Page == nil || rows[0].Page.Items == nil || len(rows[0].Page.Cursor) == 0 || len(rows[0].Page.Items) > limit {
		return nil, errors.New("Monday returned an incomplete item page.")
	}
	p := rows[0].Page
	next := ""
	if string(p.Cursor) != "null" {
		if e = decodeJSON(p.Cursor, &next); e != nil || next == "" || next == prior || !cursorOK(next) {
			return nil, errors.New("Monday returned an invalid or repeated item cursor.")
		}
	}
	if len(p.Items) == 0 && next != "" {
		return nil, errors.New("Monday returned an empty page with a continuation cursor.")
	}
	seen := map[string]bool{}
	for _, v := range p.Items {
		if !itemValid(v) || v.Board.ID != in.BoardID || seen[v.ID] {
			return nil, errors.New("Monday returned invalid items or a different board.")
		}
		seen[v.ID] = true
	}
	return &ListItemsOut{Items: p.Items, NextCursor: next, HasMore: next != ""}, nil
}
func (c *client) get_item(ctx context.Context, in *GetItemIn) (*GetItemOut, error) {
	if e := checkID(in.ItemID); e != nil {
		return nil, e
	}
	data, e := c.gql(ctx, "query ($ids: [ID!]!) { items(ids: $ids) { "+itemFields+" } }", map[string]any{"ids": []string{in.ItemID}})
	if e != nil {
		return nil, e
	}
	var rows []schema.Item
	if e = decodeField(data, "items", &rows); e != nil {
		return nil, e
	}
	if len(rows) != 1 || rows[0].ID != in.ItemID || !itemValid(rows[0]) {
		return nil, errors.New("Monday returned no complete matching item.")
	}
	return &GetItemOut{Item: rows[0]}, nil
}
func columnJSON(s string) error {
	if len(s) > 256<<10 {
		return errors.New("Column values exceed 256 KiB.")
	}
	var v map[string]json.RawMessage
	if decodeJSON([]byte(s), &v) != nil || len(v) == 0 {
		return errors.New("Column values must be a nonempty JSON object string keyed by column ID.")
	}
	return nil
}
func mutationItem(data map[string]json.RawMessage, key, board, id string) (schema.Item, error) {
	var v schema.Item
	if e := decodeField(data, key, &v); e != nil {
		return v, e
	}
	if !itemValid(v) || v.Board.ID != board || (id != "" && v.ID != id) {
		return v, errors.New("Monday returned an invalid write receipt. Inspect the item before retrying.")
	}
	return v, nil
}
func (c *client) create_item(ctx context.Context, in *CreateItemIn) (*CreateItemOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	if !textOK(in.Name, 1024) {
		return nil, errors.New("Item name is required and must be at most 1024 bytes.")
	}
	var group, values any
	if in.GroupID != "" {
		if !namedID.MatchString(in.GroupID) {
			return nil, errors.New("Invalid group ID.")
		}
		group = in.GroupID
	}
	if in.ColumnValuesJSON != "" {
		if e := columnJSON(in.ColumnValuesJSON); e != nil {
			return nil, e
		}
		values = in.ColumnValuesJSON
	}
	data, e := c.gql(ctx, "mutation ($board: ID!, $name: String!, $group: String, $values: JSON, $labels: Boolean) { create_item(board_id: $board, item_name: $name, group_id: $group, column_values: $values, create_labels_if_missing: $labels) { "+itemFields+" } }", map[string]any{"board": in.BoardID, "name": in.Name, "group": group, "values": values, "labels": in.Options.CreateLabels})
	if e != nil {
		return nil, e
	}
	v, e := mutationItem(data, "create_item", in.BoardID, "")
	if e != nil {
		return nil, e
	}
	if in.GroupID != "" && (v.Group == nil || v.Group.ID != in.GroupID) {
		return nil, errors.New("Monday returned a different destination group. Inspect the item before retrying.")
	}
	return &CreateItemOut{Item: v}, nil
}
func (c *client) change_columns(ctx context.Context, in *ChangeColumnsIn) (*ChangeColumnsOut, error) {
	if e := checkID(in.BoardID); e != nil {
		return nil, e
	}
	if e := checkID(in.ItemID); e != nil {
		return nil, e
	}
	if e := columnJSON(in.ColumnValuesJSON); e != nil {
		return nil, e
	}
	data, e := c.gql(ctx, "mutation ($board: ID!, $item: ID!, $values: JSON!, $labels: Boolean) { change_multiple_column_values(board_id: $board, item_id: $item, column_values: $values, create_labels_if_missing: $labels) { "+itemFields+" } }", map[string]any{"board": in.BoardID, "item": in.ItemID, "values": in.ColumnValuesJSON, "labels": in.Options.CreateLabels})
	if e != nil {
		return nil, e
	}
	v, e := mutationItem(data, "change_multiple_column_values", in.BoardID, in.ItemID)
	if e != nil {
		return nil, e
	}
	return &ChangeColumnsOut{Item: v}, nil
}
func (c *client) add_update(ctx context.Context, in *AddUpdateIn) (*AddUpdateOut, error) {
	if e := checkID(in.ItemID); e != nil {
		return nil, e
	}
	if !textOK(in.Body, 64<<10) {
		return nil, errors.New("Update body is required and must be at most 64 KiB.")
	}
	data, e := c.gql(ctx, "mutation ($item: ID!, $body: String!) { create_update(item_id: $item, body: $body) { id body } }", map[string]any{"item": in.ItemID, "body": in.Body})
	if e != nil {
		return nil, e
	}
	var v struct {
		ID   string `json:"id"`
		Body string `json:"body"`
	}
	if e = decodeField(data, "create_update", &v); e != nil {
		return nil, e
	}
	if checkID(v.ID) != nil || v.Body == "" {
		return nil, errors.New("Monday returned an invalid update receipt. Inspect the item before retrying.")
	}
	return &AddUpdateOut{UpdateID: v.ID, Body: v.Body}, nil
}
