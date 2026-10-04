package main

// The polling event source (the path that doesn't need a webhook configured; pick one or the
// other with webhook.go).
//
// Goes through /repos/{o}/{r}/events: GitHub puts a repo's activity feed here, and a single call
// gets you five kinds of activity — pushes/Issues/PRs/comments/releases — which is much cheaper
// than polling each endpoint separately (the rate limit is 5000/hour; polling one repo once a
// minute is 60 calls/hour, so watching a dozen or so repos is comfortably within budget).
//
// Two details that must be handled:
//
//   - **The first round doesn't fire events**. The first fetch returns the most recent 30
//     historical activities; dispatching them as-is would flood the workflow with 30 stale events
//     the moment the plugin starts. So the first round only records the cursor (primed), and
//     events are only dispatched from the second round on.
//   - **The activity feed is cached**. GitHub documents about 60 seconds of caching on this
//     endpoint, so polling more often than every 60 seconds is pointless and just burns quota.
//
// Workflow failures aren't in the activity feed (GitHub doesn't count them as repo activity), so
// they're queried separately via the Actions runs endpoint.

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const pollInterval = 60 * time.Second

func runEvents(ctx plugin.SourceCtx) error {
	cred := credOf(ctx)
	repos := splitRepos(cred.WatchRepos)
	if len(repos) == 0 {
		// Report "waiting for config" rather than silently exiting — that lights up the row on
		// the credential list; otherwise the user would just see a source that does nothing,
		// with no way to tell if it's broken or just unconfigured.
		ctx.ReportStatus("idle", "凭证里没填「事件盯哪些仓库」，轮询源不启动")
		return nil
	}
	if strings.TrimSpace(cred.Token) == "" {
		ctx.ReportStatus("auth_required", "凭证里没有访问令牌")
		return nil
	}
	log.Printf("[github] 轮询源启动，盯 %d 个仓库：%s", len(repos), strings.Join(repos, ", "))

	seen := map[string]string{}  // repo → the newest event id last seen
	primed := map[string]bool{}  // repo → whether the first round has passed
	runSeen := map[string]bool{} // workflow-failure dedup (run_id+attempt)

	for {
		for _, repo := range repos {
			if ctx.Err() != nil {
				return nil
			}
			if err := pollRepo(ctx, repo, seen, primed); err != nil {
				log.Printf("[github] 轮询 %s 失败：%v", repo, err)
			}
			if err := pollFailedRuns(ctx, repo, runSeen, primed[repo]); err != nil {
				log.Printf("[github] 查 %s 的失败工作流出错：%v", repo, err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pollInterval):
		}
	}
}

func splitRepos(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pollRepo fetches one repo's activity feed and dispatches by type.
func pollRepo(ctx plugin.SourceCtx, repo string, seen map[string]string, primed map[string]bool) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/events", map[string]any{"per_page": 30})
	if err != nil {
		return err
	}
	list := digList(raw)
	if len(list) == 0 {
		return nil
	}
	last := seen[repo]
	newest := str(list[0], "id")

	if !primed[repo] {
		// First round: record the cursor only, don't dispatch.
		seen[repo], primed[repo] = newest, true
		log.Printf("[github] %s 首轮记录游标 %s（历史事件不补发）", repo, newest)
		return nil
	}
	// The activity feed is newest→oldest; iterate backwards to dispatch in chronological order.
	for i := len(list) - 1; i >= 0; i-- {
		e := list[i]
		id := str(e, "id")
		if id == "" || id == last || !newerThan(id, last) {
			continue
		}
		dispatchActivity(ctx, repo, id, e)
	}
	seen[repo] = newest
	return nil
}

// newerThan: the activity feed's id is a **monotonically increasing decimal digit string** (but
// can exceed int64's comfort zone, so compare by length, then lexically).
func newerThan(id, last string) bool {
	if last == "" {
		return true
	}
	if len(id) != len(last) {
		return len(id) > len(last)
	}
	return id > last
}

// dispatchActivity translates one activity-feed event into an event in the plugin contract.
//
// The activity feed's payload and the webhook's payload **look similar but aren't the same**
// (e.g. there's no top-level repository object), so the webhook parsing code can't be reused
// directly — that's why each path has its own implementation.
func dispatchActivity(ctx plugin.SourceCtx, repo, id string, e map[string]any) {
	pl := obj(e, "payload")
	actor := nested(e, "actor", "login")
	eid := "poll:" + id

	switch str(e, "type") {
	case "PushEvent":
		commits := arr(pl, "commits")
		msg := ""
		if n := len(commits); n > 0 {
			if m, ok := commits[n-1].(map[string]any); ok {
				msg = clip(str(m, "message"), 2000)
			}
		}
		_ = TriggerCommitPushed(ctx, eid, &CommitPushedEvent{
			Repo: repo, Ref: strings.TrimPrefix(str(pl, "ref"), "refs/heads/"),
			CommitSHA: str(pl, "head"), CommitMessage: msg, CommitCount: len(commits),
			Author: actor, Source: "poll",
		})
	case "IssuesEvent":
		iss := obj(pl, "issue")
		switch str(pl, "action") {
		case "opened", "reopened":
			_ = TriggerIssueOpened(ctx, eid, &IssueOpenedEvent{
				Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
				Body: clip(str(iss, "body"), 4000), Author: nested(iss, "user", "login"),
				Labels: namesOf(iss["labels"], "name"), URL: str(iss, "html_url"),
				Source: "poll",
			})
		case "labeled":
			_ = TriggerIssueLabeled(ctx, eid, &IssueLabeledEvent{
				Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
				Body: clip(str(iss, "body"), 4000), AddedLabel: nested(pl, "label", "name"),
				Labels: namesOf(iss["labels"], "name"), IsPr: isPullRequest(iss),
				Actor: actor, URL: str(iss, "html_url"), Source: "poll",
			})
		}
	case "PullRequestEvent":
		pr := obj(pl, "pull_request")
		switch str(pl, "action") {
		case "opened", "reopened":
			_ = TriggerPrOpened(ctx, eid, &PrOpenedEvent{
				Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
				Body: clip(str(pr, "body"), 4000), Author: nested(pr, "user", "login"),
				Base: nested(pr, "base", "ref"), Head: nested(pr, "head", "ref"),
				HeadSHA: nested(pr, "head", "sha"), Draft: boolean(pr, "draft"),
				Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
				Source: "poll",
			})
		case "closed":
			if boolean(pr, "merged") {
				_ = TriggerPrMerged(ctx, eid, &PrMergedEvent{
					Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
					Author: nested(pr, "user", "login"), MergedBy: nested(pr, "merged_by", "login"),
					Base: nested(pr, "base", "ref"), MergeSHA: str(pr, "merge_commit_sha"),
					Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
					Source: "poll",
				})
			}
		}
	case "PullRequestReviewEvent":
		pr := obj(pl, "pull_request")
		rv := obj(pl, "review")
		_ = TriggerPrReviewSubmitted(ctx, eid, &PrReviewSubmittedEvent{
			Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
			Reviewer: nested(rv, "user", "login"), State: strings.ToLower(str(rv, "state")),
			Body: clip(str(rv, "body"), 4000), HeadSHA: nested(pr, "head", "sha"),
			URL: str(rv, "html_url"), Source: "poll",
		})
	case "IssueCommentEvent":
		if str(pl, "action") != "created" {
			return
		}
		iss := obj(pl, "issue")
		cm := obj(pl, "comment")
		ev := IssueCommentedEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Comment: clip(str(cm, "body"), 8000), CommentID: num(cm, "id"),
			Author: nested(cm, "user", "login"), AuthorAssociation: str(cm, "author_association"),
			IsBot: isBot(obj(cm, "user")), URL: str(cm, "html_url"), Source: "poll",
		}
		if isPullRequest(iss) {
			_ = TriggerPrCommented(ctx, eid, &PrCommentedEvent{
				Repo: ev.Repo, Number: ev.Number, Title: ev.Title, Comment: ev.Comment,
				CommentID: ev.CommentID, Author: ev.Author,
				AuthorAssociation: ev.AuthorAssociation, IsBot: ev.IsBot,
				URL: ev.URL, Source: "poll",
			})
			return
		}
		_ = TriggerIssueCommented(ctx, eid, &ev)
	case "ReleaseEvent":
		if str(pl, "action") != "published" {
			return
		}
		rel := obj(pl, "release")
		_ = TriggerReleasePublished(ctx, eid, &ReleasePublishedEvent{
			Repo: repo, Tag: str(rel, "tag_name"), Name: str(rel, "name"),
			Body: clip(str(rel, "body"), 8000), Prerelease: boolean(rel, "prerelease"),
			Author: nested(rel, "author", "login"), URL: str(rel, "html_url"),
			Source: "poll",
		})
	}
}

// pollFailedRuns looks up recently failed workflows. They're not in the activity feed, so this
// has to be queried separately.
func pollFailedRuns(ctx plugin.SourceCtx, repo string, seen map[string]bool, primed bool) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/actions/runs", map[string]any{
		"status": "failure", "per_page": 10,
	})
	if err != nil {
		// A repo with Actions disabled returns 404 here — that shouldn't fail the whole polling
		// loop.
		if strings.Contains(err.Error(), "404") {
			return nil
		}
		return err
	}
	for _, it := range arr(digObj(raw), "workflow_runs") {
		r, _ := it.(map[string]any)
		if r == nil {
			continue
		}
		// run_id + attempt: a retry that also fails is a new, genuine event — deduping by run_id
		// alone would swallow it.
		key := repo + ":" + str(r, "id") + ":" + str(r, "run_attempt")
		if seen[key] {
			continue
		}
		seen[key] = true
		if !primed {
			continue // first round only records, doesn't dispatch — same rule as the activity feed
		}
		_ = TriggerWorkflowFailed(ctx, "poll:run:"+key, &WorkflowFailedEvent{
			Repo: repo, Workflow: str(r, "name"), RunID: num(r, "id"),
			Branch: str(r, "head_branch"), CommitSHA: str(r, "head_sha"),
			Event: str(r, "event"), Actor: nested(r, "actor", "login"),
			Attempt: num(r, "run_attempt"), URL: str(r, "html_url"), Source: "poll",
		})
	}
	return nil
}

var _ = context.Background
