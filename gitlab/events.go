package main

// Polling event source: Events API + failed-pipeline polling -> five kinds of platform events.
//
// GitLab has no long-polling/streaming, so in outbound mode polling is the only option
// (latency = poll interval, 60s). For zero latency, use the platform's Webhook trigger
// (pointed at from the GitLab project's Settings -> Webhooks) -- the two approaches complement
// each other.
//
// Coverage = the projects named in the credential's watch_projects -- the polling volume of
// watching the whole instance is unbounded. If it's empty, the source doesn't start (normal
// operations are unaffected).
//
// The two cursors are tracked independently:
//   - Events API dedupes by after (day granularity) + the set of seen event ids -- its time
//     filter is only day-granular, so replays within the same day are blocked by the id set;
//   - failed pipelines dedupe by updated_after (RFC3339) + the set of seen pipeline ids.
//
// The first round only records position, it doesn't backfill (same judgment call as feed/x:
// connecting to an old project shouldn't flood ten years of history in). Re-delivery of events
// is backstopped by the platform's own dedup on (pluginId, event, eventID) -- after a process
// restart the set is empty, so there will be brief re-reports, which the platform's dedup
// blocks.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const pollInterval = 60 * time.Second

// glGet is the GET used on the event-source side (SourceCtx and the operation-side Ctx are two
// different types; this reuses the HTTP layer). It only accepts context.Context: that's the
// only thing it actually uses. Accepting the concrete sokel.SourceCtx struct (pre SDK v0.7.0)
// would tie "can this be called from a test" to a transport-layer type.
func glGet(ctx context.Context, cred Cred, path string, params url.Values) ([]byte, error) {
	token := strings.TrimSpace(cred.Token)
	if token == "" {
		return nil, fmt.Errorf("凭证缺访问令牌")
	}
	base := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if base == "" {
		base = "https://gitlab.com"
	}
	full := base + "/api/v4" + path
	if enc := params.Encode(); enc != "" {
		full += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitLab 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitLab 返回 HTTP %d（%s）", resp.StatusCode, path)
	}
	b := make([]byte, 0, 1<<16)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		b = append(b, buf[:n]...)
		if rerr != nil || len(b) > 16<<20 {
			break
		}
	}
	return b, nil
}

// glEvent is a single Events API event.
type glEvent struct {
	ID         int64  `json:"id"`
	ActionName string `json:"action_name"`
	TargetType string `json:"target_type"`
	TargetIID  int64  `json:"target_iid"`
	TargetTit  string `json:"target_title"`
	CreatedAt  string `json:"created_at"`
	Author     struct {
		Username string `json:"username"`
	} `json:"author"`
	Note *struct {
		Body string `json:"body"`
		// Fields confirmed by testing against real GitLab /events output:
		//   noteable_iid  is the actual Issue number -- the event-level target_iid is the
		//                 comment's own id
		//   noteable_type distinguishes Issue / MergeRequest (both go through the same Note event)
		//   system        actions like relabeling or reassigning also generate a note
		NoteableIID  int    `json:"noteable_iid"`
		NoteableType string `json:"noteable_type"`
		System       bool   `json:"system"`
	} `json:"note"`
	PushData *struct {
		CommitCount int    `json:"commit_count"`
		Ref         string `json:"ref"`
		CommitTo    string `json:"commit_to"`
		CommitTitle string `json:"commit_title"`
	} `json:"push_data"`
}

func runEvents(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	projects := splitProjects(cred.WatchProjects)
	if len(projects) == 0 {
		return fmt.Errorf("凭证没填「事件盯哪些项目」，事件源不启动（普通操作不受影响）")
	}

	seenEvents := map[int64]bool{}  // Events API event ids
	seenPipelines := map[int]bool{} // failed pipelines already reported
	pipeSince := time.Now().UTC()   // time cursor for failed pipelines
	eventDay := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	first := true

	// Issue label changes: the Events API simply can't report these (it only has a handful of
	// actions like opened/closed), so we have to poll the issues list separately and diff
	// against the previous round's labels in memory.
	// This path is the fallback for deployments without a webhook configured -- and "create the
	// Issue, then label it" happens to be the most common usage pattern.
	issueLabels := map[string]map[int][]string{} // proj -> iid -> labels from the previous round
	for _, p := range projects {
		issueLabels[p] = map[int][]string{}
	}

	ctx.ReportStatus("ok", fmt.Sprintf("轮询 %d 个项目（%s 间隔）", len(projects), pollInterval))
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		for _, proj := range projects {
			pollProject(ctx, cred, proj, seenEvents, first)
			pollFailedPipelines(ctx, cred, proj, seenPipelines, pipeSince, first)
			pollIssueLabels(ctx, cred, proj, issueLabels[proj], first)
		}
		if first {
			first = false // the first round only records position: old projects' history isn't backfilled
		}
		// Guard against the set growing unbounded: old ids outside the polling window won't
		// reappear, so once it's big enough, just replace it wholesale.
		if len(seenEvents) > 5000 {
			seenEvents = map[int64]bool{}
			eventDay = time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		}
		_ = eventDay
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func splitProjects(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pollProject fetches a project's recent events and triggers them.
func pollProject(ctx plugin.SourceCtx, cred Cred, proj string, seen map[int64]bool, first bool) {
	q := url.Values{"per_page": {"50"}}
	// after is only day-granular: start from yesterday, replays within the same day are blocked by the seen set.
	q.Set("after", time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"))
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/events", q)
	if err != nil {
		log.Printf("[gitlab] 拉 %s 事件失败: %v", proj, err)
		return
	}
	var evs []glEvent
	_ = json.Unmarshal(raw, &evs)
	for _, ev := range evs {
		if seen[ev.ID] {
			continue
		}
		seen[ev.ID] = true
		if first {
			continue // first round only records position
		}
		triggerEvent(ctx, cred, proj, ev)
	}
}

// triggerEvent maps one Events API event to the corresponding platform event (unrecognized types are skipped).
func triggerEvent(ctx plugin.SourceCtx, cred Cred, proj string, ev glEvent) {
	eid := fmt.Sprintf("%d", ev.ID)
	var raw any
	b, _ := json.Marshal(ev)
	_ = json.Unmarshal(b, &raw)
	action := strings.ToLower(ev.ActionName)
	switch {
	case ev.PushData != nil && strings.HasPrefix(action, "pushed"):
		_ = TriggerCommitPushed(ctx, eid, &CommitPushedEvent{
			Project: proj, Ref: ev.PushData.Ref, CommitID: ev.PushData.CommitTo,
			CommitTitle: ev.PushData.CommitTitle, CommitCount: ev.PushData.CommitCount,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	case ev.TargetType == "MergeRequest" && action == "opened":
		_ = TriggerMrOpened(ctx, eid, &MrOpenedEvent{
			Project: proj, Iid: int(ev.TargetIID), Title: ev.TargetTit,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	case ev.TargetType == "MergeRequest" && action == "accepted":
		// GitLab's "merge" shows up as accepted in the Events API -- don't go looking for merged by intuition.
		_ = TriggerMrMerged(ctx, eid, &MrMergedEvent{
			Project: proj, Iid: int(ev.TargetIID), Title: ev.TargetTit,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	// Comments: the Events API records these as target_type=Note, action_name="commented on",
	// with the body in note.body. We only recognize ones attached to an Issue (the Events API
	// doesn't give us the noteable_type layer directly; we catch Note via target_type and then
	// associate it to the Issue by target_iid -- inline-diff comments like DiffNote have a
	// target_type other than Note, so they're naturally excluded).
	case ev.TargetType == "Note" && strings.HasPrefix(action, "commented"):
		n := ev.Note
		// Three filters, each pinned down by testing against real GitLab /events output:
		//   system: relabeling/reassigning also generates a note -- without this guard,
		//           "touching a label" would directly dispatch work and burn money
		//   noteable_type: Issue and MR comments go through the same event; without
		//                  distinguishing them, we'd dispatch to the wrong place
		//   noteable_iid: the event-level target_iid is the comment's own id, not the Issue
		//                 number (confirmed by testing: target_iid=1970 while the Issue is #4)
		//                 -- using it wrong means landing on someone else's Issue
		if n == nil || n.System || n.NoteableIID <= 0 || strings.TrimSpace(n.Body) == "" {
			return
		}
		switch n.NoteableType {
		case "Issue":
			_ = TriggerIssueCommented(ctx, eid, &IssueCommentedEvent{
				Project: proj, Iid: n.NoteableIID, Title: ev.TargetTit,
				Comment: n.Body, Author: ev.Author.Username,
			})
		case "MergeRequest":
			_ = TriggerMrCommented(ctx, eid, &MrCommentedEvent{
				Project: proj, Iid: n.NoteableIID, Title: ev.TargetTit,
				Comment: n.Body, Author: ev.Author.Username,
			})
		}
	case ev.TargetType == "Issue" && action == "opened":
		// The Events API only gives us the title; getting the body and labels needs one more
		// detail fetch. The extra call buys downstream consumers actual Issue content -- worth
		// it: new Issues are a low-frequency event, and an event with no body is basically
		// useless to downstream flows that act on the Issue.
		desc, labels, issueURL := issueDetail(ctx, cred, proj, int(ev.TargetIID))
		_ = TriggerIssueOpened(ctx, eid, &IssueOpenedEvent{
			Project: proj, Iid: int(ev.TargetIID), Title: ev.TargetTit,
			Description: desc, Labels: labels, URL: issueURL,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	}
}

// pollFailedPipelines polls separately for failed pipelines, which aren't in the Events API (a gap in GitLab's own coverage).
func pollFailedPipelines(ctx plugin.SourceCtx, cred Cred, proj string, seen map[int]bool, since time.Time, first bool) {
	q := url.Values{"status": {"failed"}, "per_page": {"20"},
		"updated_after": {since.Format(time.RFC3339)}}
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/pipelines", q)
	if err != nil {
		log.Printf("[gitlab] 拉 %s 失败流水线失败: %v", proj, err)
		return
	}
	var ps []struct {
		ID     int    `json:"id"`
		Ref    string `json:"ref"`
		SHA    string `json:"sha"`
		WebURL string `json:"web_url"`
	}
	_ = json.Unmarshal(raw, &ps)
	for _, p := range ps {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		if first {
			continue
		}
		_ = TriggerPipelineFailed(ctx, fmt.Sprintf("pipe-%d", p.ID), &PipelineFailedEvent{
			Project: proj, PipelineID: p.ID, Ref: p.Ref, SHA: p.SHA, WebURL: p.WebURL,
		})
	}
}

// issueDetail fetches an Issue's body/labels/link. Returns empty on failure -- the event
// itself must still go out: a failed detail fetch is a secondary failure, while dropping the
// whole "there's a new Issue" event would be the real loss.
func issueDetail(ctx context.Context, cred Cred, proj string, iid int) (desc string, labels []string, webURL string) {
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/issues/"+strconv.Itoa(iid), nil)
	if err != nil {
		log.Printf("[gitlab] 取 %s#%d 详情失败（事件照发，正文为空）: %v", proj, iid, err)
		return "", nil, ""
	}
	var d struct {
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
		WebURL      string   `json:"web_url"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return "", nil, ""
	}
	return d.Description, d.Labels, d.WebURL
}

// pollIssueLabels polls for Issue label changes.
//
// Why this needs its own path: adding a label is an update on GitLab's side, not an open, so
// issue_opened won't fire for it; and the Events API's action_name has no category for label
// changes either. Supporting "create the Issue, then label it to dispatch work" on deployments
// without a webhook means polling and diffing it ourselves.
//
// The first round only records position and doesn't trigger (same convention as the other
// sources): connecting to a project that already has hundreds of labeled Issues shouldn't treat
// all of them as "just labeled" and flood hundreds of runs.
func pollIssueLabels(ctx plugin.SourceCtx, cred Cred, proj string, seen map[int][]string, first bool) {
	q := url.Values{"per_page": {"50"}, "order_by": {"updated_at"}, "state": {"opened"}}
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/issues", q)
	if err != nil {
		log.Printf("[gitlab] 轮询 %s 的 Issue 标签失败: %v", proj, err)
		return
	}
	var issues []struct {
		IID         int      `json:"iid"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
		WebURL      string   `json:"web_url"`
		UpdatedAt   string   `json:"updated_at"`
	}
	if json.Unmarshal(raw, &issues) != nil {
		return
	}
	for _, is := range issues {
		prev, known := seen[is.IID]
		seen[is.IID] = is.Labels
		if first || !known {
			continue // first round / first time seeing this Issue: only record position
		}
		had := map[string]bool{}
		for _, l := range prev {
			had[l] = true
		}
		var added []string
		for _, l := range is.Labels {
			if !had[l] {
				added = append(added, l)
			}
		}
		if len(added) == 0 {
			continue
		}
		sort.Strings(added)
		// event_id includes updated_at: removing then re-adding the same label is two real events.
		_ = TriggerIssueLabeled(ctx, fmt.Sprintf("poll:issue-label:%s:%d:%s", proj, is.IID, is.UpdatedAt),
			&IssueLabeledEvent{
				Project: proj, Iid: is.IID, Title: is.Title, Description: is.Description,
				AddedLabels: added, Labels: is.Labels, URL: is.WebURL,
			})
	}
}
