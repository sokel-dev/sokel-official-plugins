package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/intercom/schema"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var resourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var decimalID = regexp.MustCompile(`^[1-9][0-9]{0,39}$`)

func checkID(s string) error {
	if !resourceID.MatchString(s) {
		return errors.New("Resource ID must contain only letters, digits, hyphens or underscores, up to 128 bytes.")
	}
	return nil
}
func textOK(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func cursorOK(s string) bool {
	return len(s) <= 8192 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func emailOK(s string) bool {
	a, e := mail.ParseAddress(s)
	return len(s) <= 320 && e == nil && a.Address == s
}
func (c *client) call(ctx context.Context, method, path string, q url.Values, input any) ([]byte, error) {
	b, e := c.request(ctx, method, path, q, input)
	if e != nil {
		return nil, e
	}
	var env struct {
		Type   string            `json:"type"`
		Errors []json.RawMessage `json:"errors"`
	}
	if e = decodeJSON(b, &env); e != nil {
		return nil, e
	}
	if env.Type == "error.list" || len(env.Errors) > 0 {
		return nil, errors.New("Intercom rejected the request. Check permissions, required fields and workspace limits; inspect writes before retrying.")
	}
	return b, nil
}
func contact(b []byte) (schema.Contact, error) {
	var v schema.Contact
	if e := requireKeys(b, "id", "role", "unsubscribed_from_emails"); e != nil {
		return v, e
	}
	if e := decodeJSON(b, &v); e != nil {
		return v, e
	}
	var env struct {
		Type string `json:"type"`
	}
	_ = decodeJSON(b, &env)
	if env.Type != "contact" || checkID(v.ID) != nil || (v.Role != "user" && v.Role != "lead") {
		return v, errors.New("Intercom returned invalid contact data.")
	}
	return v, nil
}
func conversation(b []byte) (schema.Conversation, error) {
	var v schema.Conversation
	if e := requireKeys(b, "id", "state", "open", "read", "created_at", "updated_at"); e != nil {
		return v, e
	}
	if e := decodeJSON(b, &v); e != nil {
		return v, e
	}
	var env struct {
		Type string `json:"type"`
	}
	_ = decodeJSON(b, &env)
	if env.Type != "conversation" || !decimalID.MatchString(v.ID) || (v.State != "open" && v.State != "closed" && v.State != "snoozed") || v.CreatedAt < 0 || v.UpdatedAt < 0 {
		return v, errors.New("Intercom returned invalid conversation data.")
	}
	return v, nil
}
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	b, e := c.call(ctx, "GET", "/me", nil, nil)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	var me struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if decodeJSON(b, &me) != nil || me.Type != "admin" || !decimalID.MatchString(me.ID) || me.Name == "" || !emailOK(me.Email) {
		return &HealthCheckOut{Message: "Intercom did not return a valid current admin."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Intercom credentials are usable for the current admin."}, nil
}
func pageOptions(p schema.Page) (int, string, error) {
	limit := 50
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 1 || limit > 150 {
		return 0, "", errors.New("Page size must be 1..150.")
	}
	cursor := ""
	if p.Cursor != nil {
		cursor = *p.Cursor
		if cursor == "" || !cursorOK(cursor) {
			return 0, "", errors.New("Invalid pagination cursor.")
		}
	}
	return limit, cursor, nil
}
func parsePage(b []byte, key, prior string, limit int) ([]json.RawMessage, string, error) {
	var env struct {
		Type          string            `json:"type"`
		Data          []json.RawMessage `json:"data"`
		Conversations []json.RawMessage `json:"conversations"`
		Total         *int              `json:"total_count"`
		Pages         *struct {
			Page       *int            `json:"page"`
			TotalPages *int            `json:"total_pages"`
			Next       json.RawMessage `json:"next"`
		} `json:"pages"`
	}
	if e := decodeJSON(b, &env); e != nil {
		return nil, "", e
	}
	rows := env.Data
	want := "list"
	if key == "conversations" {
		rows = env.Conversations
		want = "conversation.list"
	}
	if env.Type != want || rows == nil || len(rows) > limit || env.Total == nil || *env.Total < len(rows) {
		return nil, "", errors.New("Intercom returned an invalid collection.")
	}
	if env.Pages == nil {
		if len(rows) == 0 && *env.Total == 0 {
			return rows, "", nil
		}
		return nil, "", errors.New("Intercom returned no pagination metadata.")
	}
	p := env.Pages
	if p.Page == nil || p.TotalPages == nil || *p.Page < 1 || *p.TotalPages < 0 {
		return nil, "", errors.New("Intercom returned invalid page counts.")
	}
	next := ""
	if len(p.Next) > 0 && string(p.Next) != "null" {
		var n struct {
			Cursor string `json:"starting_after"`
		}
		if decodeJSON(p.Next, &n) != nil || n.Cursor == "" || n.Cursor == prior || !cursorOK(n.Cursor) {
			return nil, "", errors.New("Intercom returned an invalid or repeated cursor.")
		}
		next = n.Cursor
	}
	if next == "" && *p.Page < *p.TotalPages {
		return nil, "", errors.New("Intercom omitted the continuation cursor before the final page.")
	}
	if next != "" && len(rows) == 0 {
		return nil, "", errors.New("Intercom returned an empty page with more results.")
	}
	return rows, next, nil
}
func (c *client) contacts(ctx context.Context, p schema.Page, email string) ([]schema.Contact, string, error) {
	limit, cursor, e := pageOptions(p)
	if e != nil {
		return nil, "", e
	}
	var b []byte
	if email == "" {
		q := url.Values{"per_page": {strconv.Itoa(limit)}}
		if cursor != "" {
			q.Set("starting_after", cursor)
		}
		b, e = c.call(ctx, "GET", "/contacts", q, nil)
	} else {
		page := map[string]any{"per_page": limit}
		if cursor != "" {
			page["starting_after"] = cursor
		}
		b, e = c.call(ctx, "POST", "/contacts/search", nil, map[string]any{"query": map[string]any{"field": "email", "operator": "=", "value": email}, "pagination": page})
	}
	if e != nil {
		return nil, "", e
	}
	rows, next, e := parsePage(b, "data", cursor, limit)
	if e != nil {
		return nil, "", e
	}
	out := []schema.Contact{}
	seen := map[string]bool{}
	for _, raw := range rows {
		v, e := contact(raw)
		if e != nil {
			return nil, "", e
		}
		if seen[v.ID] {
			return nil, "", errors.New("Intercom returned duplicate contacts.")
		}
		seen[v.ID] = true
		out = append(out, v)
	}
	return out, next, nil
}
func contactFields(f schema.ContactFields) (map[string]any, error) {
	v := map[string]any{}
	for k, p := range map[string]*string{"name": f.Name, "phone": f.Phone} {
		if p != nil {
			if len(*p) > 1024 || !utf8.ValidString(*p) || strings.ContainsRune(*p, 0) {
				return nil, errors.New("Contact name and phone must be valid UTF-8 and at most 1024 bytes.")
			}
			v[k] = *p
		}
	}
	if f.Unsubscribed != nil {
		v["unsubscribed_from_emails"] = *f.Unsubscribed
	}
	return v, nil
}
func (c *client) list_contacts(ctx context.Context, in *ListContactsIn) (*ListContactsOut, error) {
	rows, next, e := c.contacts(ctx, in.Page, "")
	if e != nil {
		return nil, e
	}
	return &ListContactsOut{Contacts: rows, NextCursor: next, HasMore: next != ""}, nil
}
func (c *client) search_contacts(ctx context.Context, in *SearchContactsIn) (*SearchContactsOut, error) {
	if !emailOK(in.Email) {
		return nil, errors.New("Provide a plain email address without a display name.")
	}
	rows, next, e := c.contacts(ctx, in.Page, in.Email)
	if e != nil {
		return nil, e
	}
	return &SearchContactsOut{Contacts: rows, NextCursor: next, HasMore: next != ""}, nil
}
func (c *client) get_contact(ctx context.Context, in *GetContactIn) (*GetContactOut, error) {
	if e := checkID(in.ContactID); e != nil {
		return nil, e
	}
	b, e := c.call(ctx, "GET", "/contacts/"+in.ContactID, nil, nil)
	if e != nil {
		return nil, e
	}
	v, e := contact(b)
	if e != nil {
		return nil, e
	}
	if v.ID != in.ContactID {
		return nil, errors.New("Intercom returned a different contact.")
	}
	return &GetContactOut{Contact: v}, nil
}
func (c *client) create_contact(ctx context.Context, in *CreateContactIn) (*CreateContactOut, error) {
	if !emailOK(in.Email) {
		return nil, errors.New("Provide a plain email address without a display name.")
	}
	role := in.Role
	if role == "" {
		role = "user"
	}
	if role != "user" && role != "lead" {
		return nil, errors.New("Contact role must be user or lead.")
	}
	body, e := contactFields(in.Fields)
	if e != nil {
		return nil, e
	}
	body["email"] = in.Email
	body["role"] = role
	b, e := c.call(ctx, "POST", "/contacts", nil, body)
	if e != nil {
		return nil, e
	}
	v, e := contact(b)
	if e != nil {
		return nil, e
	}
	return &CreateContactOut{Contact: v}, nil
}
func (c *client) update_contact(ctx context.Context, in *UpdateContactIn) (*UpdateContactOut, error) {
	if e := checkID(in.ContactID); e != nil {
		return nil, e
	}
	body, e := contactFields(in.Fields)
	if e != nil {
		return nil, e
	}
	if len(body) == 0 {
		return nil, errors.New("Provide at least one contact field to update.")
	}
	b, e := c.call(ctx, "PUT", "/contacts/"+in.ContactID, nil, body)
	if e != nil {
		return nil, e
	}
	v, e := contact(b)
	if e != nil {
		return nil, e
	}
	if v.ID != in.ContactID {
		return nil, errors.New("Intercom returned a different contact. Inspect the write before retrying.")
	}
	return &UpdateContactOut{Contact: v}, nil
}
func (c *client) list_conversations(ctx context.Context, in *ListConversationsIn) (*ListConversationsOut, error) {
	limit, cursor, e := pageOptions(in.Page)
	if e != nil {
		return nil, e
	}
	q := url.Values{"per_page": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("starting_after", cursor)
	}
	b, e := c.call(ctx, "GET", "/conversations", q, nil)
	if e != nil {
		return nil, e
	}
	rows, next, e := parsePage(b, "conversations", cursor, limit)
	if e != nil {
		return nil, e
	}
	out := &ListConversationsOut{Conversations: []schema.Conversation{}, NextCursor: next, HasMore: next != ""}
	seen := map[string]bool{}
	for _, raw := range rows {
		v, e := conversation(raw)
		if e != nil {
			return nil, e
		}
		if seen[v.ID] {
			return nil, errors.New("Intercom returned duplicate conversations.")
		}
		seen[v.ID] = true
		out.Conversations = append(out.Conversations, v)
	}
	return out, nil
}
func (c *client) get_conversation(ctx context.Context, in *GetConversationIn) (*GetConversationOut, error) {
	if !decimalID.MatchString(in.ConversationID) {
		return nil, errors.New("Conversation ID must be a positive decimal string.")
	}
	b, e := c.call(ctx, "GET", "/conversations/"+in.ConversationID, url.Values{"display_as": {"plaintext"}}, nil)
	if e != nil {
		return nil, e
	}
	v, e := conversation(b)
	if e != nil {
		return nil, e
	}
	if v.ID != in.ConversationID {
		return nil, errors.New("Intercom returned a different conversation.")
	}
	var w struct {
		Source *schema.MessageSource `json:"source"`
		Parts  *struct {
			Rows  []schema.Part `json:"conversation_parts"`
			Total *int          `json:"total_count"`
		} `json:"conversation_parts"`
	}
	if decodeJSON(b, &w) != nil || w.Source == nil || w.Parts == nil || w.Parts.Rows == nil || w.Parts.Total == nil || *w.Parts.Total < len(w.Parts.Rows) || len(w.Parts.Rows) > 500 {
		return nil, errors.New("Intercom returned incomplete conversation parts.")
	}
	seen := map[string]bool{}
	for _, p := range w.Parts.Rows {
		if checkID(p.ID) != nil || p.Type == "" || p.CreatedAt < 0 || seen[p.ID] {
			return nil, errors.New("Intercom returned invalid conversation parts.")
		}
		seen[p.ID] = true
	}
	return &GetConversationOut{Conversation: v, Source: *w.Source, Parts: w.Parts.Rows, PartsTotal: *w.Parts.Total, PartsMayBeTruncated: len(w.Parts.Rows) >= 500 || len(w.Parts.Rows) < *w.Parts.Total}, nil
}
func (c *client) reply_conversation(ctx context.Context, in *ReplyConversationIn) (*ReplyConversationOut, error) {
	if !decimalID.MatchString(in.ConversationID) || !decimalID.MatchString(in.AdminID) {
		return nil, errors.New("Conversation and admin IDs must be positive decimal strings.")
	}
	if !textOK(in.Body, 64<<10) {
		return nil, errors.New("Reply body is required and must be at most 64 KiB.")
	}
	kind := in.MessageType
	if kind == "" {
		kind = "comment"
	}
	if kind != "comment" && kind != "note" {
		return nil, errors.New("Reply type must be comment or note.")
	}
	b, e := c.call(ctx, "POST", "/conversations/"+in.ConversationID+"/reply", nil, map[string]any{"admin_id": in.AdminID, "body": in.Body, "message_type": kind, "type": "admin"})
	if e != nil {
		return nil, e
	}
	v, e := conversation(b)
	if e != nil {
		return nil, e
	}
	if v.ID != in.ConversationID {
		return nil, errors.New("Intercom returned a different conversation. Inspect the reply before retrying.")
	}
	return &ReplyConversationOut{Conversation: v}, nil
}
