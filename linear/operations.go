package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/linear/schema"
	"strings"
)

const issueFields = `id identifier title description url priority dueDate updatedAt team { id key name } state { id name } assignee { id name } project { id name }`
const pageFields = `pageInfo { hasNextPage endCursor }`

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func (p pageInfo) cursor() (string, error) {
	if p.HasNextPage && p.EndCursor == "" {
		return "", fmt.Errorf("Linear returned an incomplete pagination cursor")
	}
	if !p.HasNextPage {
		return "", nil
	}
	return p.EndCursor, nil
}
func (a *api) graph(ctx context.Context, query string, variables map[string]any, out any) error {
	var response struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if _, e := a.request(ctx, "POST", "/graphql", nil, map[string]any{"query": query, "variables": variables}, &response); e != nil {
		return e
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("Linear returned GraphQL errors; check permissions, IDs, input values and rate limits")
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return fmt.Errorf("Linear returned no GraphQL data")
	}
	if e := json.Unmarshal(response.Data, out); e != nil {
		return fmt.Errorf("Linear returned an invalid GraphQL data shape")
	}
	return nil
}
func (a *api) health(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var r struct {
		Viewer struct{ ID, Name string } `json:"viewer"`
	}
	e := a.graph(ctx, `query { viewer { id name } }`, nil, &r)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if r.Viewer.ID == "" {
		return &HealthCheckOut{Message: "Linear returned no authenticated viewer"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Linear identity access verified; resource permissions are checked by each operation"}, nil
}
func pagination(n int, cursor string) (map[string]any, error) {
	limit, e := pageSize(n)
	if e != nil {
		return nil, e
	}
	v := map[string]any{"first": limit}
	if cursor != "" {
		v["after"] = cursor
	}
	return v, nil
}
func (a *api) teams(ctx context.Context, in *ListTeamsIn) (*ListTeamsOut, error) {
	v, e := pagination(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	var r struct {
		Teams *struct {
			Nodes    []schema.Team `json:"nodes"`
			PageInfo *pageInfo     `json:"pageInfo"`
		} `json:"teams"`
	}
	e = a.graph(ctx, `query($first:Int!,$after:String){teams(first:$first,after:$after){nodes{id key name} `+pageFields+`}}`, v, &r)
	if e != nil {
		return nil, e
	}
	if r.Teams == nil || r.Teams.PageInfo == nil || r.Teams.Nodes == nil {
		return nil, fmt.Errorf("Linear returned an incomplete connection")
	}
	c, e := r.Teams.PageInfo.cursor()
	return &ListTeamsOut{Teams: r.Teams.Nodes, NextCursor: c, HasMore: r.Teams.PageInfo.HasNextPage}, e
}
func (a *api) issues(ctx context.Context, in *ListIssuesIn) (*ListIssuesOut, error) {
	v, e := pagination(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	filter := map[string]any{}
	for k, id := range map[string]string{"team": in.TeamID, "project": in.ProjectID, "assignee": in.AssigneeID, "state": in.StateID} {
		if id != "" {
			filter[k] = map[string]any{"id": map[string]string{"eq": id}}
		}
	}
	if in.TitleContains != "" {
		filter["title"] = map[string]string{"containsIgnoreCase": in.TitleContains}
	}
	v["filter"] = filter
	var r struct {
		Issues *struct {
			Nodes    []schema.Issue `json:"nodes"`
			PageInfo *pageInfo      `json:"pageInfo"`
		} `json:"issues"`
	}
	e = a.graph(ctx, `query($first:Int!,$after:String,$filter:IssueFilter){issues(first:$first,after:$after,filter:$filter){nodes{`+issueFields+`} `+pageFields+`}}`, v, &r)
	if e != nil {
		return nil, e
	}
	if r.Issues == nil || r.Issues.PageInfo == nil || r.Issues.Nodes == nil {
		return nil, fmt.Errorf("Linear returned an incomplete connection")
	}
	c, e := r.Issues.PageInfo.cursor()
	return &ListIssuesOut{Issues: r.Issues.Nodes, NextCursor: c, HasMore: r.Issues.PageInfo.HasNextPage}, e
}
func (a *api) projects(ctx context.Context, in *ListProjectsIn) (*ListProjectsOut, error) {
	v, e := pagination(in.Limit, in.Cursor)
	if e != nil {
		return nil, e
	}
	filter := map[string]any{}
	if in.NameContains != "" {
		filter["name"] = map[string]string{"containsIgnoreCase": in.NameContains}
	}
	v["filter"] = filter
	var r struct {
		Projects *struct {
			Nodes    []schema.Project `json:"nodes"`
			PageInfo *pageInfo        `json:"pageInfo"`
		} `json:"projects"`
	}
	e = a.graph(ctx, `query($first:Int!,$after:String,$filter:ProjectFilter){projects(first:$first,after:$after,filter:$filter){nodes{id name description url updatedAt targetDate} `+pageFields+`}}`, v, &r)
	if e != nil {
		return nil, e
	}
	if r.Projects == nil || r.Projects.PageInfo == nil || r.Projects.Nodes == nil {
		return nil, fmt.Errorf("Linear returned an incomplete connection")
	}
	c, e := r.Projects.PageInfo.cursor()
	return &ListProjectsOut{Projects: r.Projects.Nodes, NextCursor: c, HasMore: r.Projects.PageInfo.HasNextPage}, e
}
func (a *api) getIssue(ctx context.Context, in *GetIssueIn) (*GetIssueOut, error) {
	if e := required(in.IssueID, "issue_id"); e != nil {
		return nil, e
	}
	var r struct {
		Issue *schema.Issue `json:"issue"`
	}
	if e := a.graph(ctx, `query($id:String!){issue(id:$id){`+issueFields+`}}`, map[string]any{"id": in.IssueID}, &r); e != nil {
		return nil, e
	}
	if r.Issue == nil || r.Issue.ID == "" {
		return nil, fmt.Errorf("Linear issue was not found or is not accessible")
	}
	return &GetIssueOut{Issue: *r.Issue}, nil
}
func issueChanges(c schema.IssueChanges) (map[string]any, error) {
	m := map[string]any{}
	for key, p := range map[string]*string{"title": c.Title, "description": c.Description, "stateId": c.StateID, "assigneeId": c.AssigneeID, "projectId": c.ProjectID, "dueDate": c.DueDate} {
		if p == nil {
			continue
		}
		if (key == "title" || key == "stateId") && strings.TrimSpace(*p) == "" {
			return nil, fmt.Errorf("title and state_id cannot be cleared")
		}
		if key == "dueDate" {
			if e := validDate(*p); e != nil {
				return nil, e
			}
		}
		if (key == "assigneeId" || key == "projectId" || key == "dueDate") && *p == "" {
			m[key] = nil
		} else {
			m[key] = *p
		}
	}
	if c.Priority != nil {
		if *c.Priority < 0 || *c.Priority > 4 {
			return nil, fmt.Errorf("priority must be 0 (none) to 4 (low)")
		}
		m["priority"] = *c.Priority
	}
	return m, nil
}

type mutationResult struct {
	Success bool          `json:"success"`
	Issue   *schema.Issue `json:"issue"`
}

func (a *api) createIssue(ctx context.Context, in *CreateIssueIn) (*CreateIssueOut, error) {
	if e := required(in.TeamID, "team_id"); e != nil {
		return nil, e
	}
	if e := required(in.Title, "title"); e != nil {
		return nil, e
	}
	if in.Details.Title != nil {
		return nil, fmt.Errorf("set title at the top level, not inside details")
	}
	m, e := issueChanges(in.Details)
	if e != nil {
		return nil, e
	}
	m["title"] = in.Title
	m["teamId"] = in.TeamID
	var r struct {
		Result mutationResult `json:"issueCreate"`
	}
	e = a.graph(ctx, `mutation($input:IssueCreateInput!){issueCreate(input:$input){success issue{`+issueFields+`}}}`, map[string]any{"input": m}, &r)
	if e != nil {
		return nil, e
	}
	if !r.Result.Success || r.Result.Issue == nil || r.Result.Issue.ID == "" {
		return nil, fmt.Errorf("Linear did not confirm issue creation; inspect Linear before retrying")
	}
	return &CreateIssueOut{Issue: *r.Result.Issue}, nil
}
func (a *api) updateIssue(ctx context.Context, in *UpdateIssueIn) (*UpdateIssueOut, error) {
	if e := required(in.IssueID, "issue_id"); e != nil {
		return nil, e
	}
	m, e := issueChanges(in.Changes)
	if e != nil {
		return nil, e
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("changes must contain at least one supported field")
	}
	var r struct {
		Result mutationResult `json:"issueUpdate"`
	}
	e = a.graph(ctx, `mutation($id:String!,$input:IssueUpdateInput!){issueUpdate(id:$id,input:$input){success issue{`+issueFields+`}}}`, map[string]any{"id": in.IssueID, "input": m}, &r)
	if e != nil {
		return nil, e
	}
	if !r.Result.Success || r.Result.Issue == nil || r.Result.Issue.ID == "" {
		return nil, fmt.Errorf("Linear did not confirm issue update; inspect Linear before retrying")
	}
	return &UpdateIssueOut{Issue: *r.Result.Issue}, nil
}
