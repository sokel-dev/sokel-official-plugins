package main

// The platform receives webhooks directly here (the zero-latency event path; pick either this
// or the polling source in events.go): point the GitHub repo's Settings -> Webhooks at the
// platform's /hooks/{token}, with Content type set to application/json.
//
// Signature verification: GitHub signs the whole request body with **HMAC-SHA256** and puts it
// in the X-Hub-Signature-256 header as sha256=<hex>. This is not the same scheme as GitLab's
// "compare a plaintext token" approach — copying GitLab's code here will never verify
// successfully. The comparison must use hmac.Equal (constant-time); a plain == leaks timing
// information.
//
// event_id is simply **X-GitHub-Delivery**: GitHub assigns each delivery a UUID, and
// **redeliveries reuse the same one** — which is exactly the dedup semantics the platform
// wants, so there's no need to build a fragile key out of object id + timestamp ourselves.
//
// This is **intentionally a separate source** from the polling path (which uses the poll:
// prefix): with both enabled, the same action fires twice. The docs say to pick one; we don't
// try to hard-dedupe here since the two sides' ids live in different domains and forcing them
// together would produce a fragile mapping.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func handleWebhook(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) sokel.WebhookResponse {
	if resp, ok := verifySignature(ctx, req); !ok {
		return resp
	}
	event := req.Header("X-GitHub-Event")
	delivery := req.Header("X-GitHub-Delivery")
	if delivery == "" {
		delivery = "nodelivery"
	}
	var body map[string]any
	if json.Unmarshal(req.Body, &body) != nil {
		return sokel.Text(400, "bad payload")
	}
	// GitHub sends a ping immediately when you save the webhook config — it only shows green
	// once we reply 200.
	if event == "ping" {
		return sokel.Text(200, "pong")
	}
	repo := nestedStr(body, "repository", "full_name")
	action := str(body, "action")

	switch event {
	case "push":
		emitPush(ctx, delivery, repo, body)
	case "pull_request":
		emitPullRequest(ctx, delivery, repo, action, body)
	case "pull_request_review":
		if action == "submitted" {
			pr := obj(body, "pull_request")
			rv := obj(body, "review")
			_ = TriggerPrReviewSubmitted(ctx, "wh:"+delivery, &PrReviewSubmittedEvent{
				Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
				Reviewer: nested(rv, "user", "login"),
				// GitHub sends these uppercase (APPROVED/CHANGES_REQUESTED) but the contract
				// uses lowercase — normalize it
				State: strings.ToLower(str(rv, "state")),
				Body:  clip(str(rv, "body"), 4000), HeadSHA: nested(pr, "head", "sha"),
				URL: str(rv, "html_url"), Source: "webhook",
			})
		}
	case "issues":
		emitIssues(ctx, delivery, repo, action, body)
	case "issue_comment":
		emitComment(ctx, delivery, repo, action, body)
	case "release":
		if action == "published" {
			rel := obj(body, "release")
			_ = TriggerReleasePublished(ctx, "wh:"+delivery, &ReleasePublishedEvent{
				Repo: repo, Tag: str(rel, "tag_name"), Name: str(rel, "name"),
				Body: clip(str(rel, "body"), 8000), Prerelease: boolean(rel, "prerelease"),
				Author: nested(rel, "author", "login"), URL: str(rel, "html_url"),
				Source: "webhook",
			})
		}
	case "workflow_run":
		run := obj(body, "workflow_run")
		if action == "completed" && str(run, "conclusion") == "failure" {
			_ = TriggerWorkflowFailed(ctx, "wh:"+delivery, &WorkflowFailedEvent{
				Repo: repo, Workflow: str(run, "name"), RunID: num(run, "id"),
				Branch: str(run, "head_branch"), CommitSHA: str(run, "head_sha"),
				Event: str(run, "event"), Actor: nested(run, "actor", "login"),
				Attempt: num(run, "run_attempt"), URL: str(run, "html_url"),
				Source: "webhook",
			})
		}
	}
	// Reply 200 for event types we don't recognize too: depending on the repo config GitHub
	// sends many types, a non-2xx makes it retry repeatedly, and after enough consecutive
	// failures GitHub disables the whole webhook.
	return sokel.OK()
}

// verifySignature checks the HMAC-SHA256 signature.
//
// If the credential has no secret configured, verification is skipped (acceptable for
// internal/private repos; the docs call out the risk). If a secret is configured but the
// request carries no signature, reject it: that means the GitHub side wasn't given the secret,
// the two sides disagree, and letting it through would be pretending we verified it.
func verifySignature(ctx sokel.WebhookCtx, req *sokel.WebhookRequest) (sokel.WebhookResponse, bool) {
	secret := strings.TrimSpace(ctx.Credential()["webhook_secret"])
	if secret == "" {
		return sokel.OK(), true
	}
	got := strings.TrimSpace(req.Header("X-Hub-Signature-256"))
	if got == "" {
		return sokel.Text(401, "missing signature (webhook secret configured on this credential)"), false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(req.Body) // must be the **raw bytes** — a signature computed over re-encoded JSON won't match
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(got), []byte(want)) {
		return sokel.Text(401, "bad signature"), false
	}
	return sokel.OK(), true
}

func emitPush(ctx sokel.WebhookCtx, delivery, repo string, body map[string]any) {
	after := str(body, "after")
	// Deleting a branch is also a push event, with `after` all zeros — without this check,
	// downstream would try to process a commit that doesn't exist.
	deleted := boolean(body, "deleted")
	commits := arr(body, "commits")
	msg := ""
	if n := len(commits); n > 0 {
		if m, ok := commits[n-1].(map[string]any); ok {
			msg = clip(str(m, "message"), 2000)
		}
	}
	_ = TriggerCommitPushed(ctx, "wh:"+delivery, &CommitPushedEvent{
		Repo: repo, Ref: strings.TrimPrefix(str(body, "ref"), "refs/heads/"),
		CommitSHA: after, CommitMessage: msg, CommitCount: len(commits),
		Author:  firstNonEmpty(nestedStr(body, "pusher", "name"), nestedStr(body, "sender", "login")),
		Forced:  boolean(body, "forced"),
		Created: boolean(body, "created"), Deleted: deleted,
		Source: "webhook",
	})
}

func emitPullRequest(ctx sokel.WebhookCtx, delivery, repo, action string, body map[string]any) {
	pr := obj(body, "pull_request")
	switch action {
	case "opened", "reopened", "ready_for_review":
		_ = TriggerPrOpened(ctx, "wh:"+delivery, &PrOpenedEvent{
			Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
			Body: clip(str(pr, "body"), 4000), Author: nested(pr, "user", "login"),
			Base: nested(pr, "base", "ref"), Head: nested(pr, "head", "ref"),
			HeadSHA: nested(pr, "head", "sha"), Draft: boolean(pr, "draft"),
			Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
			Source: "webhook",
		})
	case "closed":
		// **closed does not mean merged**: closing without merging is also "closed". The
		// `merged` field is the actual criterion.
		if boolean(pr, "merged") {
			_ = TriggerPrMerged(ctx, "wh:"+delivery, &PrMergedEvent{
				Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
				Author: nested(pr, "user", "login"), MergedBy: nested(pr, "merged_by", "login"),
				Base: nested(pr, "base", "ref"), MergeSHA: str(pr, "merge_commit_sha"),
				Labels: namesOf(pr["labels"], "name"), URL: str(pr, "html_url"),
				Source: "webhook",
			})
		}
	case "labeled":
		// Labeling a PR fires the pull_request event, not issues — we need to emit from both
		// places, or the very common "trigger a workflow by labeling a PR" use case silently
		// breaks.
		_ = TriggerIssueLabeled(ctx, "wh:"+delivery, &IssueLabeledEvent{
			Repo: repo, Number: num(pr, "number"), Title: str(pr, "title"),
			Body: clip(str(pr, "body"), 4000), AddedLabel: nestedStr(body, "label", "name"),
			Labels: namesOf(pr["labels"], "name"), IsPr: true,
			Actor: nestedStr(body, "sender", "login"), URL: str(pr, "html_url"),
			Source: "webhook",
		})
	}
}

func emitIssues(ctx sokel.WebhookCtx, delivery, repo, action string, body map[string]any) {
	iss := obj(body, "issue")
	switch action {
	case "opened", "reopened":
		_ = TriggerIssueOpened(ctx, "wh:"+delivery, &IssueOpenedEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Body: clip(str(iss, "body"), 4000), Author: nested(iss, "user", "login"),
			Labels: namesOf(iss["labels"], "name"), URL: str(iss, "html_url"),
			Source: "webhook",
		})
	case "labeled":
		// GitHub fires one event per label added, and tells us directly which one was just
		// added — much cleaner than GitLab's approach of diffing two arrays ourselves.
		_ = TriggerIssueLabeled(ctx, "wh:"+delivery, &IssueLabeledEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Body: clip(str(iss, "body"), 4000), AddedLabel: nestedStr(body, "label", "name"),
			Labels: namesOf(iss["labels"], "name"), IsPr: isPullRequest(iss),
			Actor: nestedStr(body, "sender", "login"), URL: str(iss, "html_url"),
			Source: "webhook",
		})
	}
}

// emitComment handles comments. GitHub fires **the same event** for both Issue and PR
// comments — we tell them apart using the issue.pull_request key; without that check, "leaving
// a comment on a PR" would also fire the Issue trigger.
func emitComment(ctx sokel.WebhookCtx, delivery, repo, action string, body map[string]any) {
	if action != "created" {
		return // editing/deleting a comment doesn't trigger, or a bot would keep waking itself up on its own edited comments
	}
	iss := obj(body, "issue")
	cm := obj(body, "comment")
	author := nested(cm, "user", "login")
	bot := isBot(obj(cm, "user"))
	if isPullRequest(iss) {
		_ = TriggerPrCommented(ctx, "wh:"+delivery, &PrCommentedEvent{
			Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
			Comment: clip(str(cm, "body"), 8000), CommentID: num(cm, "id"),
			Author: author, AuthorAssociation: str(cm, "author_association"),
			IsBot: bot, URL: str(cm, "html_url"), Source: "webhook",
		})
		return
	}
	_ = TriggerIssueCommented(ctx, "wh:"+delivery, &IssueCommentedEvent{
		Repo: repo, Number: num(iss, "number"), Title: str(iss, "title"),
		Comment: clip(str(cm, "body"), 8000), CommentID: num(cm, "id"),
		Author: author, AuthorAssociation: str(cm, "author_association"),
		IsBot: bot, URL: str(cm, "html_url"), Source: "webhook",
	})
}

// isBot reports whether this was posted by a bot.
//
// **ChatOps must filter this out**: a bot replying to its own comments loops forever — this is
// the first trap people hit when wiring up GitHub bots, and the loop spins fast (one API call
// per round, enough to burn through the rate limit in minutes). Both checks are needed: the
// `type` field is authoritative, and the name suffix covers older payloads that lack `type`.
func isBot(user map[string]any) bool {
	if strings.EqualFold(str(user, "type"), "Bot") {
		return true
	}
	return strings.HasSuffix(str(user, "login"), "[bot]")
}

func nestedStr(m map[string]any, k1, k2 string) string { return nested(m, k1, k2) }
