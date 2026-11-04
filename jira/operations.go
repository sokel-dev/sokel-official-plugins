package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/jira/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const issueFields = "summary,description,status,assignee,labels,updated,duedate,priority"

var issuePattern = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_]*-[1-9][0-9]*|[1-9][0-9]*)$`)
var customPattern = regexp.MustCompile(`^customfield_[0-9]+$`)

func issuePath(id string) (string, error) {
	if !issuePattern.MatchString(id) {
		return "", fmt.Errorf("issue_key must be a Jira issue key or numeric ID")
	}
	return "/rest/api/3/issue/" + id, nil
}

type rawIssue struct {
	ID, Key string
	Fields  struct {
		Summary     string
		Description json.RawMessage
		Status      struct{ ID, Name string }
		Assignee    *struct {
			AccountID string `json:"accountId"`
		}
		Labels   []string
		Updated  string
		DueDate  string `json:"duedate"`
		Priority *struct{ ID string }
	}
}

func (r rawIssue) typed() schema.Issue {
	o := schema.Issue{ID: r.ID, Key: r.Key, Summary: r.Fields.Summary, DescriptionADF: rawText(r.Fields.Description), Status: r.Fields.Status.Name, StatusID: r.Fields.Status.ID, Labels: r.Fields.Labels, Updated: r.Fields.Updated, DueDate: r.Fields.DueDate}
	if r.Fields.Assignee != nil {
		o.AssigneeAccountID = r.Fields.Assignee.AccountID
	}
	if r.Fields.Priority != nil {
		o.PriorityID = r.Fields.Priority.ID
	}
	return o
}
func rawText(r json.RawMessage) string {
	if len(r) == 0 || string(r) == "null" {
		return ""
	}
	return string(r)
}
func (a *api) health(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		AccountID string `json:"accountId"`
	}
	_, e := a.request(ctx, "GET", "/rest/api/3/myself", nil, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.AccountID == "" {
		return &HealthCheckOut{Message: "Jira returned no authenticated account"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Jira identity access verified; project permissions are checked by each operation"}, nil
}
func (a *api) search(ctx context.Context, in *SearchIssuesIn) (*SearchIssuesOut, error) {
	if strings.TrimSpace(in.Jql) == "" {
		return nil, fmt.Errorf("jql is required; use a bounded project query")
	}
	n, e := pageSize(in.Limit)
	if e != nil {
		return nil, e
	}
	payload := map[string]any{"jql": in.Jql, "maxResults": n, "fields": strings.Split(issueFields, ",")}
	if in.NextPageToken != "" {
		payload["nextPageToken"] = in.NextPageToken
	}
	var r struct {
		Issues []rawIssue `json:"issues"`
		Next   string     `json:"nextPageToken"`
		IsLast *bool      `json:"isLast"`
	}
	_, e = a.request(ctx, "POST", "/rest/api/3/search/jql", nil, payload, &r)
	if e != nil {
		return nil, e
	}
	if r.Issues == nil {
		return nil, fmt.Errorf("Jira returned an incomplete issue list")
	}
	last := r.Next == ""
	if r.IsLast != nil {
		last = *r.IsLast
	}
	if !last && r.Next == "" {
		return nil, fmt.Errorf("Jira returned an incomplete pagination token")
	}
	out := &SearchIssuesOut{Issues: []schema.Issue{}, NextPageToken: r.Next, IsLast: last}
	for _, item := range r.Issues {
		out.Issues = append(out.Issues, item.typed())
	}
	return out, nil
}
func (a *api) getIssue(ctx context.Context, in *GetIssueIn) (*GetIssueOut, error) {
	path, e := issuePath(in.IssueKey)
	if e != nil {
		return nil, e
	}
	var r rawIssue
	_, e = a.request(ctx, "GET", path, url.Values{"fields": {issueFields}}, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Jira returned an incomplete issue")
	}
	return &GetIssueOut{Issue: r.typed()}, nil
}
func adf(text string) map[string]any {
	content := []any{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		parts := []any{}
		if line != "" {
			parts = append(parts, map[string]any{"type": "text", "text": line})
		}
		content = append(content, map[string]any{"type": "paragraph", "content": parts})
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}
func issueChanges(c schema.IssueChanges, custom map[string]any) (map[string]any, error) {
	m := map[string]any{}
	for k, v := range custom {
		if !customPattern.MatchString(k) {
			return nil, fmt.Errorf("custom_fields accepts only customfield_<number> keys")
		}
		m[k] = v
	}
	if c.Summary != nil {
		if e := required(*c.Summary, "summary"); e != nil {
			return nil, e
		}
		m["summary"] = *c.Summary
	}
	if c.Description != nil {
		if *c.Description == "" {
			m["description"] = nil
		} else {
			m["description"] = adf(*c.Description)
		}
	}
	if c.AssigneeAccountID != nil {
		var id any = *c.AssigneeAccountID
		if *c.AssigneeAccountID == "" {
			id = nil
		}
		m["assignee"] = map[string]any{"accountId": id}
	}
	if c.Labels != nil {
		for _, l := range c.Labels {
			if strings.TrimSpace(l) == "" || strings.ContainsAny(l, " \t\n\r") {
				return nil, fmt.Errorf("labels must be nonempty and contain no whitespace")
			}
		}
		m["labels"] = c.Labels
	}
	if c.DueDate != nil {
		if e := validDate(*c.DueDate); e != nil {
			return nil, e
		}
		if *c.DueDate == "" {
			m["duedate"] = nil
		} else {
			m["duedate"] = *c.DueDate
		}
	}
	if c.PriorityID != nil {
		if _, e := numericID(*c.PriorityID, "priority_id"); e != nil {
			return nil, e
		}
		m["priority"] = map[string]string{"id": *c.PriorityID}
	}
	return m, nil
}
func (a *api) createIssue(ctx context.Context, in *CreateIssueIn) (*CreateIssueOut, error) {
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`).MatchString(in.ProjectKey) {
		return nil, fmt.Errorf("project_key is invalid")
	}
	if _, e := numericID(in.IssueTypeID, "issue_type_id"); e != nil {
		return nil, e
	}
	if e := required(in.Summary, "summary"); e != nil {
		return nil, e
	}
	if in.Details.Summary != nil {
		return nil, fmt.Errorf("set summary at the top level, not inside details")
	}
	fields, e := issueChanges(in.Details, in.CustomFields)
	if e != nil {
		return nil, e
	}
	fields["project"] = map[string]string{"key": in.ProjectKey}
	fields["issuetype"] = map[string]string{"id": in.IssueTypeID}
	fields["summary"] = in.Summary
	out := &CreateIssueOut{}
	_, e = a.request(ctx, "POST", "/rest/api/3/issue", nil, map[string]any{"fields": fields}, out)
	if e != nil {
		return nil, e
	}
	if out.ID == "" || out.Key == "" {
		return nil, fmt.Errorf("Jira did not confirm issue creation; inspect Jira before retrying")
	}
	return out, nil
}
func (a *api) updateIssue(ctx context.Context, in *UpdateIssueIn) (*UpdateIssueOut, error) {
	path, e := issuePath(in.IssueKey)
	if e != nil {
		return nil, e
	}
	fields, e := issueChanges(in.Changes, in.CustomFields)
	if e != nil {
		return nil, e
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("changes or custom_fields must contain at least one field")
	}
	_, e = a.request(ctx, "PUT", path, nil, map[string]any{"fields": fields}, nil)
	if e != nil {
		return nil, e
	}
	return &UpdateIssueOut{OK: true}, nil
}
func (a *api) transitions(ctx context.Context, in *ListTransitionsIn) (*ListTransitionsOut, error) {
	path, e := issuePath(in.IssueKey)
	if e != nil {
		return nil, e
	}
	var r struct {
		Transitions []struct {
			ID, Name string
			To       struct{ ID, Name string }
			Fields   json.RawMessage
		}
	}
	_, e = a.request(ctx, "GET", path+"/transitions", url.Values{"expand": {"transitions.fields"}}, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Transitions == nil {
		return nil, fmt.Errorf("Jira returned an incomplete transition list")
	}
	out := &ListTransitionsOut{Transitions: []schema.Transition{}}
	for _, v := range r.Transitions {
		out.Transitions = append(out.Transitions, schema.Transition{ID: v.ID, Name: v.Name, ToID: v.To.ID, ToName: v.To.Name, FieldsJSON: rawText(v.Fields)})
	}
	return out, nil
}
func (a *api) transition(ctx context.Context, in *TransitionIssueIn) (*TransitionIssueOut, error) {
	path, e := issuePath(in.IssueKey)
	if e != nil {
		return nil, e
	}
	if _, e := numericID(in.TransitionID, "transition_id"); e != nil {
		return nil, e
	}
	payload := map[string]any{"transition": map[string]string{"id": in.TransitionID}}
	if len(in.Fields) > 0 {
		payload["fields"] = in.Fields
	}
	_, e = a.request(ctx, "POST", path+"/transitions", nil, payload, nil)
	if e != nil {
		return nil, e
	}
	return &TransitionIssueOut{OK: true}, nil
}

type rawComment struct {
	ID     string
	Body   json.RawMessage
	Author struct {
		AccountID string `json:"accountId"`
	}
	Created, Updated string
}

func (r rawComment) typed() schema.Comment {
	return schema.Comment{ID: r.ID, BodyADF: rawText(r.Body), AuthorAccountID: r.Author.AccountID, Created: r.Created, Updated: r.Updated}
}
func (a *api) comments(ctx context.Context, in *ListCommentsIn) (*ListCommentsOut, error) {
	path, e := issuePath(in.IssueKey)
	if e != nil {
		return nil, e
	}
	n, e := pageSize(in.Limit)
	if e != nil {
		return nil, e
	}
	if in.StartAt < 0 {
		return nil, fmt.Errorf("start_at cannot be negative")
	}
	var r struct {
		StartAt  int `json:"startAt"`
		Total    int
		Comments []rawComment
	}
	_, e = a.request(ctx, "GET", path+"/comment", url.Values{"startAt": {strconv.Itoa(in.StartAt)}, "maxResults": {strconv.Itoa(n)}}, nil, &r)
	if e != nil {
		return nil, e
	}
	if r.Comments == nil {
		return nil, fmt.Errorf("Jira returned an incomplete comment list")
	}
	next := r.StartAt + len(r.Comments)
	more := next < r.Total
	if more && next <= in.StartAt {
		return nil, fmt.Errorf("Jira comment pagination made no progress")
	}
	out := &ListCommentsOut{Comments: []schema.Comment{}, Total: r.Total, NextStartAt: next, HasMore: more}
	for _, v := range r.Comments {
		out.Comments = append(out.Comments, v.typed())
	}
	return out, nil
}
func (a *api) addComment(ctx context.Context, in *AddCommentIn) (*AddCommentOut, error) {
	path, e := issuePath(in.IssueKey)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Text) == "" {
		return nil, fmt.Errorf("comment text is required")
	}
	var r rawComment
	_, e = a.request(ctx, "POST", path+"/comment", nil, map[string]any{"body": adf(in.Text)}, &r)
	if e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Jira did not confirm comment creation; inspect Jira before retrying")
	}
	return &AddCommentOut{Comment: r.typed()}, nil
}
