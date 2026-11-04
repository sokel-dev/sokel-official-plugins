package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/zendesk/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func validID(id string) error {
	if !numericID.MatchString(id) {
		return fmt.Errorf("resource ID must be a positive decimal integer")
	}
	return nil
}

type wireTicket struct {
	ID          json.Number `json:"id"`
	Subject     string      `json:"subject"`
	Description string      `json:"description"`
	Status      string      `json:"status"`
	Priority    string      `json:"priority"`
	RequesterID json.Number `json:"requester_id"`
	AssigneeID  json.Number `json:"assignee_id"`
	Tags        []string    `json:"tags"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
	ResultType  string      `json:"result_type"`
}

func (w wireTicket) output() (schema.Ticket, error) {
	if e := validID(string(w.ID)); e != nil {
		return schema.Ticket{}, fmt.Errorf("Zendesk returned a ticket without a valid ID")
	}
	if w.ResultType != "" && w.ResultType != "ticket" {
		return schema.Ticket{}, fmt.Errorf("Zendesk search returned a non-ticket resource")
	}
	return schema.Ticket{ID: string(w.ID), Subject: w.Subject, Description: w.Description, Status: w.Status, Priority: w.Priority, RequesterID: string(w.RequesterID), AssigneeID: string(w.AssigneeID), Tags: w.Tags, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}, nil
}
func (a *api) health_check(c context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		User *struct {
			ID   json.Number `json:"id"`
			Role string      `json:"role"`
		} `json:"user"`
	}
	_, e := a.request(c, "GET", "/api/v2/users/me.json", nil, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.User == nil || validID(string(r.User.ID)) != nil || (r.User.Role != "agent" && r.User.Role != "admin") {
		return &HealthCheckOut{Message: "Zendesk did not confirm an agent or administrator account"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Zendesk credential is valid for an agent; ticket access is checked per operation"}, nil
}
func (a *api) ticket(c context.Context, method, path string, body any) (schema.Ticket, error) {
	var r struct {
		Ticket *wireTicket `json:"ticket"`
	}
	status, e := a.request(c, method, path, nil, body, &r)
	if e != nil {
		return schema.Ticket{}, e
	}
	expected := 200
	if method == "POST" {
		expected = 201
	}
	if status != expected || r.Ticket == nil {
		return schema.Ticket{}, fmt.Errorf("Zendesk did not return the expected ticket receipt")
	}
	return r.Ticket.output()
}
func (a *api) get_ticket(c context.Context, in *GetTicketIn) (*GetTicketOut, error) {
	if e := validID(in.TicketID); e != nil {
		return nil, e
	}
	v, e := a.ticket(c, "GET", "/api/v2/tickets/"+in.TicketID+".json", nil)
	if e != nil {
		return nil, e
	}
	if v.ID != in.TicketID {
		return nil, fmt.Errorf("Zendesk returned an unexpected ticket ID")
	}
	return &GetTicketOut{Ticket: v}, nil
}
func pageQuery(limit int, cursor string) (url.Values, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(cursor) > 8192 {
		return nil, fmt.Errorf("page size must be 1 to 100 and cursor must not exceed 8192 characters")
	}
	q := url.Values{"page[size]": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("page[after]", cursor)
	}
	return q, nil
}

type pageMeta struct {
	HasMore     *bool  `json:"has_more"`
	AfterCursor string `json:"after_cursor"`
}

func nextCursor(meta *pageMeta, current string) (string, bool, error) {
	if meta == nil || meta.HasMore == nil {
		return "", false, fmt.Errorf("Zendesk response is missing cursor metadata")
	}
	if !*meta.HasMore {
		return "", false, nil
	}
	if meta.AfterCursor == "" || meta.AfterCursor == current {
		return "", false, fmt.Errorf("Zendesk pagination cursor did not advance")
	}
	return meta.AfterCursor, true, nil
}
func (a *api) list_tickets(c context.Context, in *ListTicketsIn) (*ListTicketsOut, error) {
	q, e := pageQuery(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	var r struct {
		Tickets []wireTicket `json:"tickets"`
		Meta    *pageMeta    `json:"meta"`
	}
	_, e = a.request(c, "GET", "/api/v2/tickets.json", q, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Tickets == nil {
		return nil, fmt.Errorf("Zendesk response is missing tickets")
	}
	next, more, e := nextCursor(r.Meta, in.Cursor)
	if e != nil {
		return nil, e
	}
	v := make([]schema.Ticket, 0, len(r.Tickets))
	for _, w := range r.Tickets {
		t, e := w.output()
		if e != nil {
			return nil, e
		}
		v = append(v, t)
	}
	return &ListTicketsOut{Tickets: v, NextCursor: next, HasMore: more}, nil
}
func (a *api) search_tickets(c context.Context, in *SearchTicketsIn) (*SearchTicketsOut, error) {
	if strings.TrimSpace(in.Query) == "" || len(in.Query) > 8192 {
		return nil, fmt.Errorf("ticket search query is required and must not exceed 8192 characters")
	}
	page := in.Page
	if page == 0 {
		page = 1
	}
	if page < 1 || page > 10 {
		return nil, fmt.Errorf("search page must be 1 to 10; narrow the query for more than 1000 matches")
	}
	q := url.Values{"query": {"type:ticket " + in.Query}, "page": {strconv.Itoa(page)}, "per_page": {"100"}}
	var r struct {
		Results  []wireTicket `json:"results"`
		Count    *int         `json:"count"`
		NextPage *string      `json:"next_page"`
	}
	_, e := a.request(c, "GET", "/api/v2/search.json", q, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Results == nil || r.Count == nil || *r.Count < 0 {
		return nil, fmt.Errorf("Zendesk response is missing search results or count")
	}
	v := make([]schema.Ticket, 0, len(r.Results))
	for _, w := range r.Results {
		if w.ResultType != "ticket" {
			return nil, fmt.Errorf("search query must return tickets only")
		}
		t, e := w.output()
		if e != nil {
			return nil, e
		}
		v = append(v, t)
	}
	next := 0
	if r.NextPage != nil && *r.NextPage != "" {
		origin, e := a.origin()
		if e != nil {
			return nil, e
		}
		u, e := url.Parse(*r.NextPage)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || "https://"+u.Host != origin || u.Path != "/api/v2/search.json" || u.RawPath != "" {
			return nil, fmt.Errorf("Zendesk returned an invalid search continuation URL")
		}
		n, e := strconv.Atoi(u.Query().Get("page"))
		if e != nil || n != page+1 {
			return nil, fmt.Errorf("Zendesk search pagination did not advance")
		}
		if page < 10 {
			next = n
		}
	}
	return &SearchTicketsOut{Tickets: v, NextPage: next, HasMore: next != 0, TotalCount: *r.Count, Truncated: *r.Count > 1000}, nil
}
func changes(in schema.TicketChanges) (map[string]any, error) {
	b := map[string]any{}
	if in.Subject != nil {
		if strings.TrimSpace(*in.Subject) == "" {
			return nil, fmt.Errorf("subject must not be empty")
		}
		b["subject"] = *in.Subject
	}
	if in.Status != nil {
		switch *in.Status {
		case "new", "open", "pending", "hold", "solved":
			b["status"] = *in.Status
		default:
			return nil, fmt.Errorf("status must be new, open, pending, hold or solved")
		}
	}
	if in.Priority != nil {
		switch *in.Priority {
		case "":
			b["priority"] = nil
		case "low", "normal", "high", "urgent":
			b["priority"] = *in.Priority
		default:
			return nil, fmt.Errorf("priority must be low, normal, high, urgent or empty to clear")
		}
	}
	if in.AssigneeID != nil {
		if *in.AssigneeID == "" {
			b["assignee_id"] = nil
		} else {
			if e := validID(*in.AssigneeID); e != nil {
				return nil, e
			}
			b["assignee_id"] = json.Number(*in.AssigneeID)
		}
	}
	if in.Tags != nil {
		tags := *in.Tags
		if tags == nil {
			tags = []string{}
		}
		for _, tag := range tags {
			if strings.TrimSpace(tag) == "" {
				return nil, fmt.Errorf("tags must not contain empty entries")
			}
		}
		b["tags"] = tags
	}
	return b, nil
}
func concurrency(b map[string]any, stamp string) error {
	if stamp == "" {
		return nil
	}
	if _, e := time.Parse(time.RFC3339, stamp); e != nil {
		return fmt.Errorf("updated_stamp must be an RFC3339 timestamp from the current ticket")
	}
	b["safe_update"] = true
	b["updated_stamp"] = stamp
	return nil
}
func (a *api) create_ticket(c context.Context, in *CreateTicketIn) (*CreateTicketOut, error) {
	if strings.TrimSpace(in.Subject) == "" || strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("subject and initial comment are required")
	}
	if e := validID(in.RequesterID); e != nil {
		return nil, e
	}
	if in.Details.Subject != nil {
		return nil, fmt.Errorf("supply subject only as the top-level field")
	}
	b, e := changes(in.Details)
	if e != nil {
		return nil, e
	}
	b["subject"] = in.Subject
	b["requester_id"] = json.Number(in.RequesterID)
	b["comment"] = map[string]any{"body": in.Body, "public": in.Public}
	v, e := a.ticket(c, "POST", "/api/v2/tickets.json", map[string]any{"ticket": b})
	if e != nil {
		return nil, e
	}
	return &CreateTicketOut{Ticket: v}, nil
}
func (a *api) update_ticket(c context.Context, in *UpdateTicketIn) (*UpdateTicketOut, error) {
	if e := validID(in.TicketID); e != nil {
		return nil, e
	}
	b, e := changes(in.Changes)
	if e != nil {
		return nil, e
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("at least one changed field is required")
	}
	if e = concurrency(b, in.UpdatedStamp); e != nil {
		return nil, e
	}
	v, e := a.ticket(c, "PUT", "/api/v2/tickets/"+in.TicketID+".json", map[string]any{"ticket": b})
	if e != nil {
		return nil, e
	}
	if v.ID != in.TicketID {
		return nil, fmt.Errorf("Zendesk returned an unexpected ticket ID")
	}
	return &UpdateTicketOut{Ticket: v}, nil
}
func (a *api) add_comment(c context.Context, in *AddCommentIn) (*AddCommentOut, error) {
	if e := validID(in.TicketID); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("comment body is required")
	}
	b := map[string]any{"comment": map[string]any{"body": in.Body, "public": in.Public}}
	if e := concurrency(b, in.UpdatedStamp); e != nil {
		return nil, e
	}
	v, e := a.ticket(c, "PUT", "/api/v2/tickets/"+in.TicketID+".json", map[string]any{"ticket": b})
	if e != nil {
		return nil, e
	}
	if v.ID != in.TicketID {
		return nil, fmt.Errorf("Zendesk returned an unexpected ticket ID")
	}
	return &AddCommentOut{Ticket: v}, nil
}
func (a *api) list_comments(c context.Context, in *ListCommentsIn) (*ListCommentsOut, error) {
	if e := validID(in.TicketID); e != nil {
		return nil, e
	}
	q, e := pageQuery(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	var r struct {
		Comments []struct {
			ID        json.Number `json:"id"`
			AuthorID  json.Number `json:"author_id"`
			Body      string      `json:"body"`
			Public    *bool       `json:"public"`
			CreatedAt string      `json:"created_at"`
		} `json:"comments"`
		Meta *pageMeta `json:"meta"`
	}
	_, e = a.request(c, "GET", "/api/v2/tickets/"+in.TicketID+"/comments.json", q, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Comments == nil {
		return nil, fmt.Errorf("Zendesk response is missing comments")
	}
	next, more, e := nextCursor(r.Meta, in.Cursor)
	if e != nil {
		return nil, e
	}
	v := make([]schema.Comment, 0, len(r.Comments))
	for _, w := range r.Comments {
		if validID(string(w.ID)) != nil || w.Public == nil {
			return nil, fmt.Errorf("Zendesk returned an incomplete comment")
		}
		v = append(v, schema.Comment{ID: string(w.ID), AuthorID: string(w.AuthorID), Body: w.Body, Public: *w.Public, CreatedAt: w.CreatedAt})
	}
	return &ListCommentsOut{Comments: v, NextCursor: next, HasMore: more}, nil
}
