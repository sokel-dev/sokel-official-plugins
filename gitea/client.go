package main

import (
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/gitea/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type repositoryWire struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}
type issueWire struct {
	ID          int64           `json:"id"`
	Number      int64           `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	HTMLURL     string          `json:"html_url"`
	UpdatedAt   string          `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}
type pullWire struct {
	ID      int64  `json:"id"`
	Number  int64  `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Merged  bool   `json:"merged"`
	Draft   bool   `json:"draft"`
}
type commentWire struct {
	ID      int64  `json:"id"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

func (w repositoryWire) output() (schema.Repository, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Repository{}, e
	}
	if w.Name == "" || w.FullName == "" {
		return schema.Repository{}, errors.New("Gitea returned an incomplete repository.")
	}
	return schema.Repository{ID: id, Name: w.Name, FullName: w.FullName, Private: w.Private, DefaultBranch: w.DefaultBranch, HTMLURL: w.HTMLURL}, nil
}
func (w issueWire) output() (schema.Issue, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.Issue{}, e
	}
	num, e := idString(w.Number)
	if e != nil {
		return schema.Issue{}, e
	}
	if w.Title == "" || (w.State != "open" && w.State != "closed") {
		return schema.Issue{}, errors.New("Gitea returned an incomplete issue.")
	}
	return schema.Issue{ID: id, Number: num, Title: w.Title, Body: w.Body, State: w.State, HTMLURL: w.HTMLURL, UpdatedAt: w.UpdatedAt, IsPullRequest: len(w.PullRequest) > 0 && string(w.PullRequest) != "null"}, nil
}
func (w pullWire) output() (schema.PullRequest, error) {
	id, e := idString(w.ID)
	if e != nil {
		return schema.PullRequest{}, e
	}
	num, e := idString(w.Number)
	if e != nil {
		return schema.PullRequest{}, e
	}
	if w.Title == "" || (w.State != "open" && w.State != "closed") {
		return schema.PullRequest{}, errors.New("Gitea returned an incomplete pull request.")
	}
	return schema.PullRequest{ID: id, Number: num, Title: w.Title, Body: w.Body, State: w.State, HTMLURL: w.HTMLURL, Merged: w.Merged, Draft: w.Draft}, nil
}
func repoPath(owner, repo string) (string, error) {
	for _, s := range []string{owner, repo} {
		if s == "" || s == "." || s == ".." || !safeText(s, 255) {
			return "", errors.New("Provide a valid repository owner and name.")
		}
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_.-", r) {
				return "", errors.New("Repository names cannot contain path separators, spaces or URL syntax.")
			}
		}
	}
	return "/api/v1/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo), nil
}
func pageQuery(p schema.PageOptions) (url.Values, int, int, error) {
	page, limit := 1, 50
	if p.Page != nil {
		page = *p.Page
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	if page < 1 || page > 100000 || limit < 1 || limit > 50 {
		return nil, 0, 0, errors.New("Page must be 1–100000 and page size must be 1–50.")
	}
	return url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(limit)}}, page, limit, nil
}
func stateQuery(state string) (string, error) {
	if state == "" {
		return "open", nil
	}
	if state != "open" && state != "closed" && state != "all" {
		return "", errors.New("State must be open, closed or all.")
	}
	return state, nil
}

var linkPattern = regexp.MustCompile(`<([^<>]+)>\s*;\s*rel="([^"]+)"`)

func (c *client) pagination(h http.Header, route string, q url.Values, page, limit, size int) (int, error) {
	if size > limit {
		return 0, errors.New("Gitea returned more rows than the requested limit.")
	}
	link := h.Get("Link")
	next := 0
	if link != "" {
		matches := linkPattern.FindAllStringSubmatch(link, -1)
		if len(matches) == 0 {
			return 0, errors.New("Gitea returned a malformed pagination header.")
		}
		for _, m := range matches {
			for _, rel := range strings.Fields(m[2]) {
				if rel == "next" {
					if next != 0 {
						return 0, errors.New("Gitea returned duplicate next-page links.")
					}
					var e error
					next, e = c.nextPage(m[1], route, q, page)
					if e != nil {
						return 0, e
					}
				}
			}
		}
	}
	total := h.Get("X-Total-Count")
	if total != "" {
		n, e := strconv.ParseInt(total, 10, 64)
		if e != nil || n < 0 || n < int64(size) {
			return 0, errors.New("Gitea returned an invalid total count.")
		}
		if next == 0 && int64((page-1)*limit+size) < n {
			return 0, errors.New("Gitea omitted a required next-page link; check reverse proxy pagination headers.")
		}
	} else if link == "" && size > 0 {
		return 0, errors.New("Gitea omitted pagination metadata; check reverse proxy Link and X-Total-Count headers.")
	}
	return next, nil
}
func (c *client) healthCheck(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	_, e := c.json(ctx, "GET", "/api/v1/user", nil, nil, 200, &user)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if user.ID < 1 || user.Login == "" {
		return &HealthCheckOut{Message: "Gitea did not return an authenticated user."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Gitea accepted the token and returned the current user. Repository permissions are checked by each operation."}, nil
}
func (c *client) listRepositories(ctx plugin.Ctx, in *ListRepositoriesIn) (*ListRepositoriesOut, error) {
	q, page, limit, e := pageQuery(in.Pagination)
	if e != nil {
		return nil, e
	}
	route := "/api/v1/user/repos"
	var rows []repositoryWire
	h, e := c.json(ctx, "GET", route, q, nil, 200, &rows)
	if e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("Gitea returned a null repository list.")
	}
	next, e := c.pagination(h, route, q, page, limit, len(rows))
	if e != nil {
		return nil, e
	}
	out := &ListRepositoriesOut{Repositories: make([]schema.Repository, 0, len(rows)), NextPage: next}
	for _, w := range rows {
		v, e := w.output()
		if e != nil {
			return nil, e
		}
		out.Repositories = append(out.Repositories, v)
	}
	return out, nil
}
func (c *client) getRepository(ctx plugin.Ctx, in *GetRepositoryIn) (*GetRepositoryOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	var row repositoryWire
	if _, e = c.json(ctx, "GET", route, nil, nil, 200, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	return &GetRepositoryOut{Repository: v}, nil
}
func (c *client) listIssues(ctx plugin.Ctx, in *ListIssuesIn) (*ListIssuesOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	route += "/issues"
	q, page, limit, e := pageQuery(in.Pagination)
	if e != nil {
		return nil, e
	}
	state, e := stateQuery(in.State)
	if e != nil {
		return nil, e
	}
	q.Set("state", state)
	q.Set("type", "issues")
	var rows []issueWire
	h, e := c.json(ctx, "GET", route, q, nil, 200, &rows)
	if e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("Gitea returned a null issue list.")
	}
	next, e := c.pagination(h, route, q, page, limit, len(rows))
	if e != nil {
		return nil, e
	}
	out := &ListIssuesOut{Issues: make([]schema.Issue, 0, len(rows)), NextPage: next}
	for _, w := range rows {
		v, e := w.output()
		if e != nil {
			return nil, e
		}
		if v.IsPullRequest {
			return nil, errors.New("Gitea returned a pull request in the issues-only list.")
		}
		out.Issues = append(out.Issues, v)
	}
	return out, nil
}
func (c *client) getIssue(ctx plugin.Ctx, in *GetIssueIn) (*GetIssueOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	if _, e = positiveID(in.Number); e != nil {
		return nil, e
	}
	var row issueWire
	if _, e = c.json(ctx, "GET", route+"/issues/"+in.Number, nil, nil, 200, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	if v.Number != in.Number {
		return nil, errors.New("Gitea returned a different issue number.")
	}
	return &GetIssueOut{Issue: v}, nil
}
func (c *client) createIssue(ctx plugin.Ctx, in *CreateIssueIn) (*CreateIssueOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Title) == "" || !safeText(in.Title, 255) {
		return nil, errors.New("Issue title must contain 1–255 characters without control characters.")
	}
	body := map[string]any{"title": in.Title}
	o := in.Options
	if o.Body != nil {
		if !validBody(*o.Body, 1<<20) {
			return nil, errors.New("Issue description exceeds 1 MiB or contains invalid text.")
		}
		body["body"] = *o.Body
	}
	if o.Closed != nil {
		body["closed"] = *o.Closed
	}
	if len(o.Assignees) > 100 || len(o.LabelIDs) > 100 {
		return nil, errors.New("At most 100 assignees and label IDs are supported.")
	}
	if o.Assignees != nil {
		for _, v := range o.Assignees {
			if _, e := repoPath(v, "placeholder"); e != nil {
				return nil, e
			}
		}
		body["assignees"] = o.Assignees
	}
	if o.LabelIDs != nil {
		ids := make([]int64, 0, len(o.LabelIDs))
		for _, v := range o.LabelIDs {
			id, e := positiveID(v)
			if e != nil {
				return nil, e
			}
			ids = append(ids, id)
		}
		body["labels"] = ids
	}
	var row issueWire
	if _, e = c.json(ctx, "POST", route+"/issues", nil, body, 201, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	return &CreateIssueOut{Issue: v}, nil
}
func (c *client) addComment(ctx plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	if _, e = positiveID(in.Number); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Body) == "" || !validBody(in.Body, 1<<20) {
		return nil, errors.New("Comment text must be nonempty and at most 1 MiB.")
	}
	var row commentWire
	if _, e = c.json(ctx, "POST", route+"/issues/"+in.Number+"/comments", nil, map[string]string{"body": in.Body}, 201, &row); e != nil {
		return nil, e
	}
	id, e := idString(row.ID)
	if e != nil {
		return nil, e
	}
	return &AddCommentOut{Comment: schema.Comment{ID: id, Body: row.Body, HTMLURL: row.HTMLURL}}, nil
}
func (c *client) listPullRequests(ctx plugin.Ctx, in *ListPullRequestsIn) (*ListPullRequestsOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	route += "/pulls"
	q, page, limit, e := pageQuery(in.Pagination)
	if e != nil {
		return nil, e
	}
	state, e := stateQuery(in.State)
	if e != nil {
		return nil, e
	}
	q.Set("state", state)
	var rows []pullWire
	h, e := c.json(ctx, "GET", route, q, nil, 200, &rows)
	if e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, errors.New("Gitea returned a null pull request list.")
	}
	next, e := c.pagination(h, route, q, page, limit, len(rows))
	if e != nil {
		return nil, e
	}
	out := &ListPullRequestsOut{PullRequests: make([]schema.PullRequest, 0, len(rows)), NextPage: next}
	for _, w := range rows {
		v, e := w.output()
		if e != nil {
			return nil, e
		}
		out.PullRequests = append(out.PullRequests, v)
	}
	return out, nil
}
func (c *client) createPullRequest(ctx plugin.Ctx, in *CreatePullRequestIn) (*CreatePullRequestOut, error) {
	route, e := repoPath(in.Owner, in.Repository)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Title) == "" || !safeText(in.Title, 255) {
		return nil, errors.New("Pull request title must contain 1–255 characters without control characters.")
	}
	if in.Head == "" || in.Base == "" || !safeText(in.Head, 512) || !safeText(in.Base, 512) || !validBody(in.Body, 1<<20) {
		return nil, errors.New("Provide valid head and base branches and a description of at most 1 MiB.")
	}
	body := map[string]string{"title": in.Title, "head": in.Head, "base": in.Base}
	if in.Body != "" {
		body["body"] = in.Body
	}
	var row pullWire
	if _, e = c.json(ctx, "POST", route+"/pulls", nil, body, 201, &row); e != nil {
		return nil, e
	}
	v, e := row.output()
	if e != nil {
		return nil, e
	}
	return &CreatePullRequestOut{PullRequest: v}, nil
}
