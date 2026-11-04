package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/typeform/schema"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func checkID(s string) error {
	if !idPattern.MatchString(s) {
		return errors.New("Provide a Typeform resource ID without a URL, path or query string.")
	}
	return nil
}
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.AccessToken) {
		return "", errors.New("Provide a valid Typeform personal access token.")
	}
	return "Bearer " + c.cred.AccessToken, nil
}
func (c *client) health_check(ctx context.Context, in *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		Email string `json:"email"`
	}
	if e := c.json(ctx, "GET", "/me", nil, nil, &r); e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	m, e := mail.ParseAddress(r.Email)
	if e != nil || m.Address != r.Email {
		return &HealthCheckOut{Message: "Typeform did not return a valid account identity."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Typeform account identity verified. Forms, responses and workspaces require their own read scopes."}, nil
}
func pageValues(p schema.PageOptions) (url.Values, int, int, error) {
	page, size := 1, 100
	if p.Page != nil {
		page = *p.Page
	}
	if p.PageSize != nil {
		size = *p.PageSize
	}
	if page < 1 || page > 1000000 || size < 1 || size > 200 {
		return nil, 0, 0, errors.New("Page must be 1 to 1000000 and page size must be 1 to 200.")
	}
	return url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(size)}}, page, size, nil
}
func addSearch(q url.Values, s string) error {
	if len(s) > 1024 || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return errors.New("Search text must be valid UTF-8 and at most 1024 bytes.")
	}
	if s != "" {
		q.Set("search", s)
	}
	return nil
}

type collection struct {
	Items []json.RawMessage `json:"items"`
	Total *int              `json:"total_items"`
	Pages *int              `json:"page_count"`
}

func (c *client) collection(ctx context.Context, path string, q url.Values, limit int) (collection, error) {
	var p collection
	if e := c.json(ctx, "GET", path, q, nil, &p); e != nil {
		return p, e
	}
	if p.Items == nil || p.Total == nil || p.Pages == nil || *p.Total < 0 || *p.Pages < 0 || len(p.Items) > limit || *p.Total < len(p.Items) {
		return p, errors.New("Typeform returned missing or inconsistent pagination metadata.")
	}
	return p, nil
}
func nextPage(page int, p collection) int {
	if page < *p.Pages {
		return page + 1
	}
	return 0
}
func workspace(b []byte) (schema.Workspace, error) {
	var r schema.Workspace
	if e := decodeJSON(b, &r); e != nil {
		return r, e
	}
	if checkID(r.ID) != nil || r.Name == "" {
		return r, errors.New("Typeform returned incomplete workspace metadata.")
	}
	return r, nil
}
func (c *client) list_workspaces(ctx context.Context, in *ListWorkspacesIn) (*ListWorkspacesOut, error) {
	q, page, size, e := pageValues(in.Page)
	if e != nil {
		return nil, e
	}
	if e = addSearch(q, in.Search); e != nil {
		return nil, e
	}
	p, e := c.collection(ctx, "/workspaces", q, size)
	if e != nil {
		return nil, e
	}
	out := &ListWorkspacesOut{Workspaces: []schema.Workspace{}, TotalItems: *p.Total, PageCount: *p.Pages, NextPage: nextPage(page, p)}
	for _, b := range p.Items {
		r, e := workspace(b)
		if e != nil {
			return nil, e
		}
		out.Workspaces = append(out.Workspaces, r)
	}
	return out, nil
}
func (c *client) get_workspace(ctx context.Context, in *GetWorkspaceIn) (*GetWorkspaceOut, error) {
	if e := checkID(in.WorkspaceID); e != nil {
		return nil, e
	}
	b, e := c.request(ctx, "GET", "/workspaces/"+in.WorkspaceID, nil, nil)
	if e != nil {
		return nil, e
	}
	r, e := workspace(b)
	if e != nil {
		return nil, e
	}
	if r.ID != in.WorkspaceID {
		return nil, errors.New("Typeform returned a different workspace.")
	}
	return &GetWorkspaceOut{Workspace: r}, nil
}
func (c *client) list_forms(ctx context.Context, in *ListFormsIn) (*ListFormsOut, error) {
	q, page, size, e := pageValues(in.Page)
	if e != nil {
		return nil, e
	}
	if e = addSearch(q, in.Search); e != nil {
		return nil, e
	}
	if in.WorkspaceID != "" {
		if e = checkID(in.WorkspaceID); e != nil {
			return nil, e
		}
		q.Set("workspace_id", in.WorkspaceID)
	}
	p, e := c.collection(ctx, "/forms", q, size)
	if e != nil {
		return nil, e
	}
	out := &ListFormsOut{Forms: []schema.FormSummary{}, TotalItems: *p.Total, PageCount: *p.Pages, NextPage: nextPage(page, p)}
	for _, b := range p.Items {
		var r struct {
			schema.FormSummary
			Links struct {
				Display *string `json:"display"`
			} `json:"_links"`
		}
		if e := decodeJSON(b, &r); e != nil {
			return nil, e
		}
		if checkID(r.ID) != nil || r.Title == "" {
			return nil, errors.New("Typeform returned incomplete form metadata.")
		}
		r.DisplayURL = r.Links.Display
		out.Forms = append(out.Forms, r.FormSummary)
	}
	return out, nil
}
func (c *client) get_form(ctx context.Context, in *GetFormIn) (*GetFormOut, error) {
	if e := checkID(in.FormID); e != nil {
		return nil, e
	}
	var r schema.Form
	if e := c.json(ctx, "GET", "/forms/"+in.FormID, nil, nil, &r); e != nil {
		return nil, e
	}
	if r.ID != in.FormID || r.Title == "" {
		return nil, errors.New("Typeform returned incomplete or mismatched form metadata.")
	}
	if r.Questions == nil {
		r.Questions = []schema.Question{}
	}
	for _, f := range r.Questions {
		if checkID(f.ID) != nil || f.Type == "" {
			return nil, errors.New("Typeform returned a question without its ID or type.")
		}
	}
	return &GetFormOut{Form: r}, nil
}
func cursor(s string) bool {
	return len(s) > 0 && len(s) <= 1024 && safeToken(s) && !strings.ContainsAny(s, "/\\?#%")
}
func submittedTime(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339, s)
	if e != nil || t.Nanosecond() != 0 {
		return time.Time{}, errors.New("Submission times must be RFC3339 timestamps with whole-second precision.")
	}
	return t.UTC(), nil
}
func response(b []byte) (schema.Response, error) {
	var r struct {
		Token       string            `json:"token"`
		ResponseID  *string           `json:"response_id"`
		LandedAt    *string           `json:"landed_at"`
		SubmittedAt *string           `json:"submitted_at"`
		Answers     []json.RawMessage `json:"answers"`
		Hidden      map[string]string `json:"hidden"`
		Calculated  map[string]any    `json:"calculated"`
	}
	if e := decodeJSON(b, &r); e != nil {
		return schema.Response{}, e
	}
	if !cursor(r.Token) {
		return schema.Response{}, errors.New("Typeform returned a response without a valid cursor token.")
	}
	out := schema.Response{Token: r.Token, ResponseID: r.ResponseID, LandedAt: r.LandedAt, SubmittedAt: r.SubmittedAt, Answers: []schema.Answer{}, Hidden: r.Hidden, Calculated: r.Calculated}
	for _, b := range r.Answers {
		var a struct {
			Type  string `json:"type"`
			Field struct {
				ID   string  `json:"id"`
				Ref  *string `json:"ref"`
				Type string  `json:"type"`
			} `json:"field"`
		}
		if e := decodeJSON(b, &a); e != nil {
			return out, e
		}
		if a.Type == "" || checkID(a.Field.ID) != nil || a.Field.Type == "" {
			return out, errors.New("Typeform returned an answer without question or answer type metadata.")
		}
		var data map[string]any
		if e := decodeJSON(b, &data); e != nil {
			return out, e
		}
		v, ok := data[a.Type]
		if !ok {
			return out, errors.New("Typeform returned an answer without its typed value.")
		}
		out.Answers = append(out.Answers, schema.Answer{Type: a.Type, FieldID: a.Field.ID, FieldRef: a.Field.Ref, FieldType: a.Field.Type, Value: v})
	}
	return out, nil
}
func (c *client) list_responses(ctx context.Context, in *ListResponsesIn) (*ListResponsesOut, error) {
	if e := checkID(in.FormID); e != nil {
		return nil, e
	}
	o := in.Options
	size := 100
	if o.PageSize != nil {
		size = *o.PageSize
	}
	if size < 1 || size > 1000 {
		return nil, errors.New("Response page size must be between 1 and 1000.")
	}
	q := url.Values{"page_size": {strconv.Itoa(size)}, "response_type": {"completed"}}
	before := ""
	if o.Before != nil {
		before = *o.Before
		if !cursor(before) {
			return nil, errors.New("Provide a valid response cursor token.")
		}
		q.Set("before", before)
	}
	var since, until time.Time
	if o.Since != nil {
		v, e := submittedTime(*o.Since)
		if e != nil {
			return nil, e
		}
		since = v
		q.Set("since", v.Format("2006-01-02T15:04:05"))
	}
	if o.Until != nil {
		v, e := submittedTime(*o.Until)
		if e != nil {
			return nil, e
		}
		until = v
		q.Set("until", v.Format("2006-01-02T15:04:05"))
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return nil, errors.New("Submitted until must not be earlier than submitted since.")
	}
	if o.Query != nil {
		if len(*o.Query) > 1024 || !utf8.ValidString(*o.Query) || strings.ContainsRune(*o.Query, 0) {
			return nil, errors.New("Answer search phrase must be valid UTF-8 and at most 1024 bytes.")
		}
		q.Set("query", *o.Query)
	}
	p, e := c.collection(ctx, "/forms/"+in.FormID+"/responses", q, size)
	if e != nil {
		return nil, e
	}
	out := &ListResponsesOut{Responses: []schema.Response{}, TotalItems: *p.Total, PageCount: *p.Pages, MayHaveMore: len(p.Items) == size || *p.Pages > 1}
	seen := map[string]bool{}
	for _, b := range p.Items {
		r, e := response(b)
		if e != nil {
			return nil, e
		}
		if r.Token == before || seen[r.Token] {
			return nil, errors.New("Typeform returned a repeated response cursor. Stop paging and inspect the query.")
		}
		seen[r.Token] = true
		out.Responses = append(out.Responses, r)
	}
	if len(out.Responses) > 0 {
		out.NextBefore = out.Responses[len(out.Responses)-1].Token
	} else {
		if out.MayHaveMore {
			return nil, errors.New("Typeform indicated further responses without a usable cursor.")
		}
	}
	if !out.MayHaveMore {
		out.NextBefore = ""
	}
	return out, nil
}
