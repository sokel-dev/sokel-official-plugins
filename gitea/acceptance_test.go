package main

import (
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/gitea/schema"
	"net/url"
	"os"
	"testing"
	"time"
)

// Explicitly gated: only a caller-provisioned loopback fixture is accepted.
func TestLocalServiceAcceptance(t *testing.T) {
	base := os.Getenv("SOKEL_TEST_GITEA_URL")
	if base == "" {
		t.Skip("Set SOKEL_TEST_GITEA_URL and the synthetic fixture credentials to run local acceptance.")
	}
	u, e := url.Parse(base)
	if e != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("Local acceptance requires a loopback service.")
	}
	token, owner, repo := os.Getenv("SOKEL_TEST_GITEA_TOKEN"), os.Getenv("SOKEL_TEST_GITEA_OWNER"), os.Getenv("SOKEL_TEST_GITEA_REPO")
	if token == "" || owner == "" || repo == "" {
		t.Fatal("Missing synthetic fixture configuration.")
	}
	c := newClient(base, Cred{Token: token})
	ctx := ctxFor("")
	health, e := c.healthCheck(ctx, &HealthCheckIn{})
	if e != nil || !health.OK {
		t.Fatalf("Health failed: %v %+v", e, health)
	}
	repos, e := c.listRepositories(ctx, &ListRepositoriesIn{})
	if e != nil || len(repos.Repositories) < 1 {
		t.Fatalf("Repository list: %v", e)
	}
	detail, e := c.getRepository(ctx, &GetRepositoryIn{Owner: owner, Repository: repo})
	if e != nil || detail.Repository.Name != repo {
		t.Fatalf("Repository get: %v", e)
	}
	if _, e = c.listIssues(ctx, &ListIssuesIn{Owner: owner, Repository: repo, State: "all"}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.listPullRequests(ctx, &ListPullRequestsIn{Owner: owner, Repository: repo, State: "all"}); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("SOKEL_TEST_GITEA_WRITE") != "1" {
		t.Log("Read-only local acceptance passed; write gate disabled.")
		return
	}
	stamp := fmt.Sprintf("Sokel synthetic acceptance %d", time.Now().UnixNano())
	f := false
	created, e := c.createIssue(ctx, &CreateIssueIn{Owner: owner, Repository: repo, Title: stamp, Options: schema.IssueOptions{Closed: &f}})
	if e != nil {
		t.Fatal(e)
	}
	got, e := c.getIssue(ctx, &GetIssueIn{Owner: owner, Repository: repo, Number: created.Issue.Number})
	if e != nil || got.Issue.Title != stamp {
		t.Fatalf("Issue read-back: %v", e)
	}
	comment, e := c.addComment(ctx, &AddCommentIn{Owner: owner, Repository: repo, Number: created.Issue.Number, Body: "Synthetic local acceptance comment; safe to delete with the fixture."})
	if e != nil || comment.Comment.ID == "" {
		t.Fatalf("Comment creation: %v", e)
	}
	if _, e = c.createIssue(ctx, &CreateIssueIn{Owner: owner, Repository: repo, Title: stamp + " pagination"}); e != nil {
		t.Fatal(e)
	}
	one := 1
	listed, e := c.listIssues(ctx, &ListIssuesIn{Owner: owner, Repository: repo, State: "all", Pagination: schema.PageOptions{Limit: &one}})
	if e != nil || listed.NextPage != 2 || len(listed.Issues) != 1 {
		t.Fatalf("First issue page: %v %+v", e, listed)
	}
	second := 2
	listed2, e := c.listIssues(ctx, &ListIssuesIn{Owner: owner, Repository: repo, State: "all", Pagination: schema.PageOptions{Page: &second, Limit: &one}})
	if e != nil || len(listed2.Issues) != 1 || listed2.Issues[0].ID == listed.Issues[0].ID {
		t.Fatalf("Second issue page: %v", e)
	}
	head, branch := os.Getenv("SOKEL_TEST_GITEA_PR_HEAD"), os.Getenv("SOKEL_TEST_GITEA_PR_BASE")
	if head != "" && branch != "" {
		pr, e := c.createPullRequest(ctx, &CreatePullRequestIn{Owner: owner, Repository: repo, Title: stamp, Head: head, Base: branch, Body: "Synthetic local acceptance pull request."})
		if e != nil || pr.PullRequest.Number == "" {
			t.Fatalf("Pull request creation: %v", e)
		}
		prs, e := c.listPullRequests(ctx, &ListPullRequestsIn{Owner: owner, Repository: repo, State: "all"})
		if e != nil || len(prs.PullRequests) < 1 {
			t.Fatalf("Pull request list: %v", e)
		}
	}
	t.Log("Synthetic isolated Gitea acceptance passed; fixture writes remain for caller cleanup.")
}
